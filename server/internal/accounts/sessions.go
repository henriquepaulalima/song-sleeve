package accounts

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/crypt"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
)

const SessionCookie = "ss_session"

// CSRFToken binds a CSRF token to a cookie session. The web keeps it in memory
// only (R-WEB) and gets it again from GET /me after a reload.
func CSRFToken(secret string, sessionID uuid.UUID) string {
	return crypt.MAC(secret, "csrf:"+sessionID.String())
}

type deviceInfo struct {
	InstallationID string `json:"installation_id"`
	Label          string `json:"label"`
}

// session is what a login or registration opens.
type session struct {
	id, deviceID uuid.UUID
	token        string
	// cleanup is the installed app's restricted cleanup credential (R-AUTH-6):
	// it only reads this account's deletion status and acknowledges the device's
	// purge, and it outlives the sessions and the user row. Empty on the web.
	cleanup string
}

// createSession registers (or reuses) the device and opens a 90-day session bound
// to its transport (R-AUTH-3). The web gets a new device per login (F-25).
// Installed apps also get a fresh cleanup credential, which replaces the device's
// earlier one for this account.
func createSession(ctx context.Context, tx pgx.Tx, d *app.Deps, userID uuid.UUID, platform string, dev deviceInfo, userAgent string) (out session, err error) {
	transport := "bearer"
	if platform == "web" {
		transport = "cookie"
		dev = deviceInfo{InstallationID: "web-" + uuid.NewString(), Label: webLabel(userAgent)}
	}
	if dev.InstallationID == "" || len(dev.InstallationID) > 100 {
		return out, apperr.Field("device.installation_id", "required")
	}
	label := strings.TrimSpace(dev.Label)
	if label == "" {
		label = platform
	}
	if len(label) > 100 {
		label = label[:100]
	}
	newDevice, _ := uuid.NewV7()
	err = tx.QueryRow(ctx, `
		INSERT INTO devices (id, user_id, installation_id, platform, label)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, installation_id) DO UPDATE SET label = EXCLUDED.label, last_seen_at = now()
		RETURNING id`, newDevice, userID, dev.InstallationID, platform, label).Scan(&out.deviceID)
	if err != nil {
		return
	}
	token, hash := crypt.NewToken()
	out.token = token
	out.id, _ = uuid.NewV7()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, device_id, token_hash, transport, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		out.id, userID, out.deviceID, hash, transport, time.Now().Add(d.Cfg.SessionLifetime))
	if err != nil || transport != "bearer" {
		return
	}
	cleanup, cleanupHash := crypt.NewToken()
	if _, err = tx.Exec(ctx, `DELETE FROM device_cleanup_receipts WHERE original_user_id = $1 AND device_id = $2 AND deletion_request_id IS NULL`, userID, out.deviceID); err != nil {
		return
	}
	receiptID, _ := uuid.NewV7()
	if _, err = tx.Exec(ctx, `
		INSERT INTO device_cleanup_receipts (id, original_user_id, device_id, credential_hash) VALUES ($1, $2, $3, $4)`,
		receiptID, userID, out.deviceID, cleanupHash); err != nil {
		return
	}
	out.cleanup = cleanup
	return
}

func setSessionCookie(c *gin.Context, d *app.Deps, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/",
		MaxAge:   int(d.Cfg.SessionLifetime.Seconds()),
		HttpOnly: true, Secure: d.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(c *gin.Context, d *app.Deps) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: d.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// webLabel names a web device from its browser and OS, e.g. "Firefox · Linux".
func webLabel(ua string) string {
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	os := ""
	switch {
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS"):
		os = "macOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if os == "" {
		return browser + " · Web"
	}
	return browser + " · " + os
}

// Gate options for the auth middleware.
type gate struct {
	allowOutdatedTerms bool
}

// Authenticate resolves the session from the transport it was issued for, then
// checks expiry, revocation, the deletion gate, suspension, the web access gate
// (R-WEB-ACCESS), CSRF on cookie writes, and the terms gate (R-AUTH-10).
func Authenticate(d *app.Deps) gin.HandlerFunc { return authenticate(d, gate{}) }

// AuthenticateAllowOutdatedTerms is used by the few routes reachable before new
// terms are accepted: GET /me, POST /me/terms and logout.
func AuthenticateAllowOutdatedTerms(d *app.Deps) gin.HandlerFunc {
	return authenticate(d, gate{allowOutdatedTerms: true})
}

func authenticate(d *app.Deps, g gate) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, transport := "", ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token, transport = strings.TrimPrefix(h, "Bearer "), "bearer"
		} else if ck, err := c.Cookie(SessionCookie); err == nil && ck != "" {
			token, transport = ck, "cookie"
		}
		if token == "" {
			httpx.Fail(c, apperr.New(apperr.Unauthenticated, "Sign in to continue."))
			return
		}
		ctx := c.Request.Context()
		var s httpx.Session
		var sessTransport string
		var expires time.Time
		var revoked, deletion, suspended *time.Time
		var lastActive time.Time
		err := d.Pool.QueryRow(ctx, `
			SELECT s.id, s.user_id, s.device_id, s.transport, s.expires_at, s.revoked_at, s.last_active_at,
			       u.email, u.locale, u.cloud_sync_enabled, u.terms_version, u.deletion_requested_at, u.suspended_at
			FROM user_sessions s JOIN users u ON u.id = s.user_id
			WHERE s.token_hash = $1`, crypt.HashToken(token)).Scan(
			&s.ID, &s.UserID, &s.DeviceID, &sessTransport, &expires, &revoked, &lastActive,
			&s.Email, &s.Locale, &s.CloudSyncEnabled, &s.TermsVersion, &deletion, &suspended)
		if err != nil {
			if !db.IsNoRows(err) {
				httpx.Fail(c, err)
				return
			}
			// A session that no longer resolves could belong to a deleted account;
			// clients learn deletion only through the restricted status (R-AUTH-6).
			httpx.Fail(c, apperr.New(apperr.Unauthenticated, "Sign in to continue."))
			return
		}
		switch {
		case sessTransport != transport:
			// A bearer token is never accepted as a cookie, or the reverse.
			httpx.Fail(c, apperr.New(apperr.Unauthenticated, "Sign in to continue."))
			return
		case revoked != nil || time.Now().After(expires):
			if transport == "cookie" {
				clearSessionCookie(c, d)
			}
			httpx.Fail(c, apperr.New(apperr.Unauthenticated, "Your session ended. Sign in again."))
			return
		case deletion != nil:
			httpx.Fail(c, apperr.New(apperr.AccountDeleted, "This account is being deleted."))
			return
		case suspended != nil:
			httpx.Fail(c, apperr.New(apperr.AccountSuspended, "This account is suspended."))
			return
		case transport == "cookie" && !s.CloudSyncEnabled:
			httpx.Fail(c, apperr.New(apperr.WebAppUnavailable, "Song Sleeve on the web is coming soon. Download the app to get started."))
			return
		}
		if transport == "cookie" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if !crypt.EqualMAC(c.GetHeader("X-CSRF-Token"), CSRFToken(d.Cfg.SessionSecret, s.ID)) {
				httpx.Fail(c, apperr.New(apperr.CSRFFailed, "Security check failed. Reload and try again."))
				return
			}
		}
		if !g.allowOutdatedTerms && s.TermsVersion != d.Cfg.TermsVersion {
			httpx.Fail(c, apperr.New(apperr.TermsOutdated, "Accept the updated terms to continue."))
			return
		}
		s.Transport = transport
		if time.Since(lastActive) > 5*time.Minute {
			_, _ = d.Pool.Exec(ctx, `UPDATE user_sessions SET last_active_at = now() WHERE id = $1`, s.ID)
			_, _ = d.Pool.Exec(ctx, `UPDATE devices SET last_seen_at = now() WHERE id = $1`, s.DeviceID)
		}
		httpx.SetSession(c, &s)
		c.Next()
	}
}

// RequireCloudSync rejects sync-related requests for accounts without cloud sync
// with cloud_sync_unavailable, on every transport (R-CLOUD-8).
func RequireCloudSync() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s := httpx.CurrentSession(c); s == nil || !s.CloudSyncEnabled {
			httpx.Fail(c, apperr.New(apperr.CloudSyncUnavailable, "Cloud sync is coming soon."))
			return
		}
		c.Next()
	}
}
