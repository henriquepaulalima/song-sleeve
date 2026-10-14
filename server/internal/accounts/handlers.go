// Package accounts covers registration, login, sessions, password recovery and
// change, reauthentication, terms and the account document (R-AUTH, §7.12).
package accounts

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/crypt"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/ratelimit"
)

type Handlers struct{ d *app.Deps }

func New(d *app.Deps) *Handlers { return &Handlers{d: d} }

// Me is the account document returned by GET /me and by login/registration.
type Me struct {
	ID               uuid.UUID `json:"id"`
	Email            string    `json:"email"`
	Locale           string    `json:"locale"`
	CloudSyncEnabled bool      `json:"cloud_sync_enabled"`
	DeviceID         uuid.UUID `json:"device_id"`
	SessionID        uuid.UUID `json:"session_id"`
	TermsVersion     string    `json:"terms_version"`
	CurrentTerms     string    `json:"current_terms_version"`
	TermsOutdated    bool      `json:"terms_outdated"`
	Revision         int64     `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	CSRFToken        string    `json:"csrf_token,omitempty"`
}

type authResponse struct {
	Account Me     `json:"account"`
	Token   string `json:"token,omitempty"` // bearer transport only
}

func (h *Handlers) loadMe(ctx context.Context, q db.Querier, userID, sessionID, deviceID uuid.UUID, transport string) (Me, error) {
	var m Me
	err := q.QueryRow(ctx, `
		SELECT id, email, locale, cloud_sync_enabled, terms_version, revision, created_at
		FROM users WHERE id = $1`, userID).Scan(&m.ID, &m.Email, &m.Locale, &m.CloudSyncEnabled, &m.TermsVersion, &m.Revision, &m.CreatedAt)
	if err != nil {
		return m, err
	}
	m.SessionID, m.DeviceID = sessionID, deviceID
	m.CurrentTerms = h.d.Cfg.TermsVersion
	m.TermsOutdated = m.TermsVersion != h.d.Cfg.TermsVersion
	if transport == "cookie" {
		m.CSRFToken = CSRFToken(h.d.Cfg.SessionSecret, sessionID)
	}
	return m, nil
}

func (h *Handlers) respondAuth(c *gin.Context, status int, userID, sessionID, deviceID uuid.UUID, token string) {
	platform := httpx.Platform(c)
	transport := "bearer"
	if platform == "web" {
		transport = "cookie"
	}
	me, err := h.loadMe(c.Request.Context(), h.d.Pool, userID, sessionID, deviceID, transport)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := authResponse{Account: me}
	if transport == "cookie" {
		setSessionCookie(c, h.d, token)
	} else {
		resp.Token = token
	}
	c.JSON(status, resp)
}

type registerRequest struct {
	Email         string     `json:"email"`
	Password      string     `json:"password"`
	Locale        string     `json:"locale"`
	AcceptedTerms bool       `json:"accepted_terms"`
	TermsVersion  string     `json:"terms_version"`
	Device        deviceInfo `json:"device"`
}

func (h *Handlers) Register(c *gin.Context) {
	var req registerRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if err := h.d.Limiter.Allow(ctx, ratelimit.Register, c.ClientIP()); err != nil {
		httpx.Fail(c, err)
		return
	}
	email := fields.NormalizeEmail(req.Email)
	if !fields.ValidEmail(email) {
		httpx.Fail(c, apperr.Field("email", "invalid"))
		return
	}
	if !req.AcceptedTerms || req.TermsVersion != h.d.Cfg.TermsVersion {
		httpx.Fail(c, apperr.WithDetails(apperr.TermsRequired, "Agree to the Terms of Use and Privacy Policy to continue.",
			map[string]any{"current_terms_version": h.d.Cfg.TermsVersion}))
		return
	}
	if err := CheckPolicy(req.Password); err != nil {
		httpx.Fail(c, err)
		return
	}
	locale := req.Locale
	if locale != "pt-BR" {
		locale = "en"
	}
	allowlisted := slices.Contains(h.d.Cfg.CloudSyncAllowlist, email)
	platform := httpx.Platform(c)
	if platform == "web" && !allowlisted {
		// R-WEB-ACCESS: no account and no web session from the web for others.
		httpx.Fail(c, apperr.New(apperr.WebAppUnavailable, "Song Sleeve on the web is coming soon. Download the app to get started."))
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	userID, _ := uuid.NewV7()
	var sessionID, deviceID uuid.UUID
	var token string
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, terms_accepted_at, terms_version, locale, cloud_sync_enabled, storage_quota_bytes)
			VALUES ($1, $2, $3, now(), $4, $5, $6, $7)`,
			userID, email, hash, h.d.Cfg.TermsVersion, locale, allowlisted, h.d.Cfg.DefaultQuotaBytes)
		if err != nil {
			if db.IsUniqueViolation(err, "users_email_key") {
				return apperr.New(apperr.EmailUnavailable, "An account with this email already exists. Log in or reset your password.")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO library_view_preferences (user_id) VALUES ($1)`, userID); err != nil {
			return err
		}
		sessionID, deviceID, token, err = createSession(ctx, tx, h.d, userID, platform, req.Device, c.GetHeader("User-Agent"))
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.respondAuth(c, http.StatusCreated, userID, sessionID, deviceID, token)
}

type loginRequest struct {
	Email    string     `json:"email"`
	Password string     `json:"password"`
	Device   deviceInfo `json:"device"`
}

func (h *Handlers) Login(c *gin.Context) {
	var req loginRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	email := fields.NormalizeEmail(req.Email)
	if err := h.d.Limiter.Allow(ctx, ratelimit.Login, c.ClientIP()+"|"+email); err != nil {
		httpx.Fail(c, err)
		return
	}
	var userID uuid.UUID
	var hash string
	var cloud bool
	var deletion, suspended *time.Time
	err := h.d.Pool.QueryRow(ctx, `
		SELECT id, password_hash, cloud_sync_enabled, deletion_requested_at, suspended_at FROM users WHERE email = $1`, email).
		Scan(&userID, &hash, &cloud, &deletion, &suspended)
	if db.IsNoRows(err) {
		VerifyPassword(req.Password, dummyHash)
		httpx.Fail(c, apperr.New(apperr.InvalidCredentials, "Wrong email or password."))
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ok, rehash := VerifyPassword(req.Password, hash)
	if !ok || deletion != nil {
		httpx.Fail(c, apperr.New(apperr.InvalidCredentials, "Wrong email or password."))
		return
	}
	if suspended != nil {
		httpx.Fail(c, apperr.New(apperr.AccountSuspended, "This account is suspended."))
		return
	}
	platform := httpx.Platform(c)
	if platform == "web" && !cloud {
		httpx.Fail(c, apperr.New(apperr.WebAppUnavailable, "Song Sleeve on the web is coming soon. Download the app to get started."))
		return
	}
	var sessionID, deviceID uuid.UUID
	var token string
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		if rehash {
			if newHash, err := HashPassword(req.Password); err == nil {
				if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, newHash); err != nil {
					return err
				}
			}
		}
		var err error
		sessionID, deviceID, token, err = createSession(ctx, tx, h.d, userID, platform, req.Device, c.GetHeader("User-Agent"))
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.respondAuth(c, http.StatusOK, userID, sessionID, deviceID, token)
}

func (h *Handlers) Logout(c *gin.Context) {
	s := httpx.CurrentSession(c)
	if _, err := h.d.Pool.Exec(c.Request.Context(), `UPDATE user_sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, s.ID); err != nil {
		httpx.Fail(c, err)
		return
	}
	if s.Transport == "cookie" {
		clearSessionCookie(c, h.d)
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) GetMe(c *gin.Context) {
	s := httpx.CurrentSession(c)
	me, err := h.loadMe(c.Request.Context(), h.d.Pool, s.UserID, s.ID, s.DeviceID, s.Transport)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, me)
}

type patchMeRequest struct {
	Locale           string `json:"locale"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// PatchMe changes the account language (D-14); it applies on every device.
func (h *Handlers) PatchMe(c *gin.Context) {
	var req patchMeRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.Locale != "en" && req.Locale != "pt-BR" {
		httpx.Fail(c, apperr.Field("locale", "unsupported"))
		return
	}
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	err := db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		var rev int64
		err := tx.QueryRow(ctx, `UPDATE users SET locale = $2, revision = revision + 1 WHERE id = $1 RETURNING revision`, s.UserID, req.Locale).Scan(&rev)
		if err != nil {
			return err
		}
		_, err = events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityAccount, EntityID: s.UserID, EventType: "account_changed", Revision: rev})
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.GetMe(c)
}

type termsRequest struct {
	TermsVersion string `json:"terms_version"`
}

// AcceptTerms records acceptance of the current terms version (R-AUTH-10).
func (h *Handlers) AcceptTerms(c *gin.Context) {
	var req termsRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.TermsVersion != h.d.Cfg.TermsVersion {
		httpx.Fail(c, apperr.WithDetails(apperr.TermsRequired, "These aren't the current terms.", map[string]any{"current_terms_version": h.d.Cfg.TermsVersion}))
		return
	}
	s := httpx.CurrentSession(c)
	if _, err := h.d.Pool.Exec(c.Request.Context(), `UPDATE users SET terms_version = $2, terms_accepted_at = now() WHERE id = $1`, s.UserID, req.TermsVersion); err != nil {
		httpx.Fail(c, err)
		return
	}
	s.TermsVersion = req.TermsVersion
	h.GetMe(c)
}

// Summary returns synced counts and storage usage (R-LIM-5).
func (h *Handlers) Summary(c *gin.Context) {
	s := httpx.CurrentSession(c)
	var out struct {
		SyncedSongs   int64 `json:"synced_songs"`
		UsedBytes     int64 `json:"storage_used_bytes"`
		ReservedBytes int64 `json:"storage_reserved_bytes"`
		QuotaBytes    int64 `json:"storage_quota_bytes"`
		CloudSync     bool  `json:"cloud_sync_enabled"`
	}
	err := h.d.Pool.QueryRow(c.Request.Context(), `
		SELECT (SELECT count(*) FROM tracks WHERE user_id = u.id AND state = 'ready'),
		       u.storage_used_bytes, u.storage_reserved_bytes,
		       CASE WHEN u.cloud_sync_enabled THEN u.storage_quota_bytes ELSE 0 END, u.cloud_sync_enabled
		FROM users u WHERE u.id = $1`, s.UserID).Scan(&out.SyncedSongs, &out.UsedBytes, &out.ReservedBytes, &out.QuotaBytes, &out.CloudSync)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type sessionDoc struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	Current      bool      `json:"current"`
	Device       struct {
		ID       uuid.UUID `json:"id"`
		Platform string    `json:"platform"`
		Label    string    `json:"label"`
	} `json:"device"`
}

func (h *Handlers) ListSessions(c *gin.Context) {
	s := httpx.CurrentSession(c)
	rows, err := h.d.Pool.Query(c.Request.Context(), `
		SELECT s.id, s.created_at, s.last_active_at, d.id, d.platform, d.label
		FROM user_sessions s JOIN devices d ON d.id = s.device_id
		WHERE s.user_id = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
		ORDER BY s.last_active_at DESC`, s.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := []sessionDoc{}
	for rows.Next() {
		var d sessionDoc
		if err := rows.Scan(&d.ID, &d.CreatedAt, &d.LastActiveAt, &d.Device.ID, &d.Device.Platform, &d.Device.Label); err != nil {
			httpx.Fail(c, err)
			return
		}
		d.Current = d.ID == s.ID
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

func (h *Handlers) RevokeSession(c *gin.Context) {
	s := httpx.CurrentSession(c)
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	tag, err := h.d.Pool.Exec(c.Request.Context(), `UPDATE user_sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, s.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(c, apperr.New(apperr.NotFound, "Session not found"))
		return
	}
	if id == s.ID && s.Transport == "cookie" {
		clearSessionCookie(c, h.d)
	}
	c.Status(http.StatusNoContent)
}

// RevokeAll logs out every device, including this one.
func (h *Handlers) RevokeAll(c *gin.Context) {
	s := httpx.CurrentSession(c)
	if _, err := h.d.Pool.Exec(c.Request.Context(), `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, s.UserID); err != nil {
		httpx.Fail(c, err)
		return
	}
	if s.Transport == "cookie" {
		clearSessionCookie(c, h.d)
	}
	c.Status(http.StatusNoContent)
}

type deviceRequest struct {
	Label string `json:"label"`
}

func (h *Handlers) PutDevice(c *gin.Context) {
	s := httpx.CurrentSession(c)
	id, err := httpx.ParamUUID(c, "deviceID")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req deviceRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	label, err := fields.Name("label", req.Label, 100)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	tag, err := h.d.Pool.Exec(c.Request.Context(), `UPDATE devices SET label = $3 WHERE id = $1 AND user_id = $2`, id, s.UserID, label)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(c, apperr.New(apperr.NotFound, "Device not found"))
		return
	}
	c.Status(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword requires the current password, keeps this session, revokes every
// other one and emails a notice (R-AUTH-8).
func (h *Handlers) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	if err := h.d.Limiter.Allow(ctx, ratelimit.PasswordChange, s.UserID.String()); err != nil {
		httpx.Fail(c, err)
		return
	}
	var hash string
	if err := h.d.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, s.UserID).Scan(&hash); err != nil {
		httpx.Fail(c, err)
		return
	}
	if ok, _ := VerifyPassword(req.CurrentPassword, hash); !ok {
		httpx.Fail(c, apperr.New(apperr.InvalidCredentials, "The current password is wrong."))
		return
	}
	if err := CheckPolicy(req.NewPassword); err != nil {
		httpx.Fail(c, err)
		return
	}
	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, s.UserID, newHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, s.UserID, s.ID); err != nil {
			return err
		}
		_, err := h.d.River.InsertTx(ctx, tx, jobs.SendEmail{UserID: s.UserID, Email: "password_changed", ExpiresAt: time.Now().Add(24 * time.Hour)}, nil)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "changed"})
}

type forgotRequest struct {
	Email string `json:"email"`
}

// Forgot always answers with the same generic response (R-AUTH-2, §7.12).
func (h *Handlers) Forgot(c *gin.Context) {
	var req forgotRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if err := h.d.Limiter.Allow(ctx, ratelimit.ForgotIP, c.ClientIP()); err != nil {
		httpx.Fail(c, err)
		return
	}
	email := fields.NormalizeEmail(req.Email)
	generic := gin.H{"status": "sent"}
	if h.d.Limiter.Allow(ctx, ratelimit.ForgotEmail, email) != nil {
		c.JSON(http.StatusAccepted, generic)
		return
	}
	var userID uuid.UUID
	err := h.d.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1 AND deletion_requested_at IS NULL`, email).Scan(&userID)
	if err != nil {
		if !db.IsNoRows(err) {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusAccepted, generic)
		return
	}
	token, hash := crypt.NewToken()
	expires := time.Now().Add(h.d.Cfg.ResetTokenTTL)
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		// A new request invalidates earlier tokens.
		if _, err := tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return err
		}
		id, _ := uuid.NewV7()
		if _, err := tx.Exec(ctx, `INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`, id, userID, hash, expires); err != nil {
			return err
		}
		// The token travels in the URL fragment, which browsers never send to a
		// server or in a Referer; the page strips it from the address bar.
		link := h.d.Cfg.WebBaseURL + "/reset#token=" + token
		_, err := h.d.River.InsertTx(ctx, tx, jobs.SendEmail{UserID: userID, Email: "password_reset", SealedLink: h.d.Sealer.Seal(link), ExpiresAt: expires},
			&river.InsertOpts{MaxAttempts: 10})
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, generic)
}

type tokenRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ValidateReset checks a reset token without consuming it.
func (h *Handlers) ValidateReset(c *gin.Context) {
	var req tokenRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	var email string
	err := h.d.Pool.QueryRow(c.Request.Context(), `
		SELECT u.email FROM password_reset_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.used_at IS NULL AND t.expires_at > now() AND u.deletion_requested_at IS NULL`,
		crypt.HashToken(req.Token)).Scan(&email)
	if err != nil {
		if db.IsNoRows(err) {
			httpx.Fail(c, apperr.New(apperr.ResetTokenInvalid, "This link has expired or was already used."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"email": email})
}

// Reset sets a new password with a single-use token, signs out every device, and
// never creates a web session (R-AUTH-4, D-27). Of two racing submits only one wins.
func (h *Handlers) Reset(c *gin.Context) {
	var req tokenRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if err := h.d.Limiter.Allow(ctx, ratelimit.ResetSubmit, c.ClientIP()); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := CheckPolicy(req.NewPassword); err != nil {
		httpx.Fail(c, err)
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var webLogin bool
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE password_reset_tokens SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			RETURNING user_id`, crypt.HashToken(req.Token)).Scan(&userID)
		if db.IsNoRows(err) {
			return apperr.New(apperr.ResetTokenInvalid, "This link has expired or was already used.")
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1 AND deletion_requested_at IS NULL RETURNING cloud_sync_enabled`, userID, hash).Scan(&webLogin)
		if db.IsNoRows(err) {
			return apperr.New(apperr.ResetTokenInvalid, "This link has expired or was already used.")
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "changed", "web_login_available": webLogin})
}

type reauthRequest struct {
	Password string `json:"password"`
	Purpose  string `json:"purpose"`
}

// Reauthenticate issues a single-use proof valid for 5 minutes, bound to this
// session and purpose (R-AUTH-5).
func (h *Handlers) Reauthenticate(c *gin.Context) {
	var req reauthRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.Purpose != "delete_account" {
		httpx.Fail(c, apperr.Field("purpose", "unsupported"))
		return
	}
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	if err := h.d.Limiter.Allow(ctx, ratelimit.Reauth, s.UserID.String()); err != nil {
		httpx.Fail(c, err)
		return
	}
	var hash string
	if err := h.d.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, s.UserID).Scan(&hash); err != nil {
		httpx.Fail(c, err)
		return
	}
	if ok, _ := VerifyPassword(req.Password, hash); !ok {
		httpx.Fail(c, apperr.New(apperr.InvalidCredentials, "The password is wrong."))
		return
	}
	token, tokenHash := crypt.NewToken()
	id, _ := uuid.NewV7()
	expires := time.Now().Add(h.d.Cfg.ReauthProofTTL)
	if _, err := h.d.Pool.Exec(ctx, `
		INSERT INTO reauthentication_proofs (id, user_id, session_id, token_hash, purpose, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, s.UserID, s.ID, tokenHash, req.Purpose, expires); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"proof": token, "expires_at": expires})
}

// ConsumeProof marks a reauthentication proof used, inside the caller's transaction.
func ConsumeProof(ctx context.Context, tx pgx.Tx, s *httpx.Session, proof, purpose string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE reauthentication_proofs SET used_at = now()
		WHERE token_hash = $1 AND user_id = $2 AND session_id = $3 AND purpose = $4
		  AND used_at IS NULL AND expires_at > now()`, crypt.HashToken(proof), s.UserID, s.ID, purpose)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.ReauthenticationNeeded, "Confirm your password again.")
	}
	return nil
}
