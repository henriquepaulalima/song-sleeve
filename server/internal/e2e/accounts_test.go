//go:build integration

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

func TestRegistrationTakenEmail(t *testing.T) {
	email := uniqueEmail("taken")
	newAppAccount(t, email)
	c := newClient(t, "desktop")
	r := c.do("POST", "/auth/register", map[string]any{"email": strings.ToUpper(email), "password": testPassword, "accepted_terms": true, "terms_version": "2026-10-draft", "device": map[string]any{"installation_id": "x"}}, "")
	if r.Status != http.StatusConflict || r.code() != "email_unavailable" {
		t.Fatalf("taken email (other case): %d %s", r.Status, r.Raw)
	}
}

func TestLoginFailuresAndRateLimit(t *testing.T) {
	email := uniqueEmail("ratelimit")
	newAppAccount(t, email)
	c := newClient(t, "desktop")
	login := func(pw string) resp {
		return c.do("POST", "/auth/login", map[string]any{"email": email, "password": pw, "device": map[string]any{"installation_id": "rl"}}, "")
	}
	if r := login("wrong-password!"); r.code() != "invalid_credentials" {
		t.Fatalf("wrong password: %s", r.Raw)
	}
	if r := c.do("POST", "/auth/login", map[string]any{"email": uniqueEmail("nobody"), "password": testPassword, "device": map[string]any{"installation_id": "rl"}}, ""); r.code() != "invalid_credentials" {
		t.Fatalf("unknown email must look like a wrong password: %s", r.Raw)
	}
	for range 9 {
		login("wrong-password!")
	}
	r := login(testPassword)
	if r.Status != http.StatusTooManyRequests || r.code() != "rate_limited" {
		t.Fatalf("11th attempt in the window: %d %s", r.Status, r.Raw)
	}
	resetRateLimits(t)
}

func TestClientUpdateRequired(t *testing.T) {
	req, _ := http.NewRequest("GET", srv.URL+"/api/media-policy", nil)
	req.Header.Set("X-Client-Platform", "android")
	req.Header.Set("X-Client-Version", "0.0.9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("old client: %d", res.StatusCode)
	}
	req.Header.Set("X-Client-Platform", "fridge")
	req.Header.Set("X-Client-Version", "9.9.9")
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown platform: %d", res.StatusCode)
	}
}

func TestPasswordChange(t *testing.T) {
	email := uniqueEmail("pwchange")
	a, _ := newAppAccount(t, email)
	b := loginApp(t, email, testPassword)
	if r := a.do("POST", "/me/password", map[string]any{"current_password": "not-the-password", "new_password": "a-brand-new-pass"}, ""); r.code() != "invalid_credentials" {
		t.Fatalf("wrong current password: %s", r.Raw)
	}
	if r := a.do("POST", "/me/password", map[string]any{"current_password": testPassword, "new_password": "1234567890"}, ""); r.code() != "password_too_common" {
		t.Fatalf("common new password: %s", r.Raw)
	}
	since := time.Now()
	expect(t, a.do("POST", "/me/password", map[string]any{"current_password": testPassword, "new_password": "a-brand-new-pass"}, ""), 200)
	// This session stays; every other one is revoked (R-AUTH-8).
	expect(t, a.do("GET", "/me", nil, ""), 200)
	if r := b.do("GET", "/me", nil, ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("other session still valid: %d", r.Status)
	}
	waitForMail(t, email, "password was changed", since)
	if r := newClient(t, "desktop").do("POST", "/auth/login", map[string]any{"email": email, "password": testPassword, "device": map[string]any{"installation_id": "x"}}, ""); r.code() != "invalid_credentials" {
		t.Fatalf("old password still works: %s", r.Raw)
	}
	loginApp(t, email, "a-brand-new-pass")
}

func TestPasswordReset(t *testing.T) {
	email := uniqueEmail("reset")
	a, _ := newAppAccount(t, email)
	anon := newClient(t, "web")
	// The response is the same whether or not the account exists (R-AUTH-2).
	unknown := expect(t, anon.do("POST", "/auth/password/forgot", map[string]any{"email": uniqueEmail("nobody")}, ""), http.StatusAccepted)
	since := time.Now()
	known := expect(t, anon.do("POST", "/auth/password/forgot", map[string]any{"email": email}, ""), http.StatusAccepted)
	if unknown["status"] != known["status"] {
		t.Fatal("forgot-password responses differ")
	}
	text := waitForMail(t, email, "Reset", since)
	m := tokenRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no reset link in: %s", text)
	}
	token := m[1]
	v := expect(t, anon.do("POST", "/auth/password/reset/validate", map[string]any{"token": token}, ""), 200)
	if v["email"] != email {
		t.Fatalf("validate: %v", v)
	}
	// Validation doesn't consume the token; the reset does, once.
	expect(t, anon.do("POST", "/auth/password/reset/validate", map[string]any{"token": token}, ""), 200)
	if r := anon.do("POST", "/auth/password/reset", map[string]any{"token": token, "new_password": "short"}, ""); r.code() != "password_invalid" {
		t.Fatalf("weak password at reset: %s", r.Raw)
	}
	body := expect(t, anon.do("POST", "/auth/password/reset", map[string]any{"token": token, "new_password": "reset-password-ok"}, ""), 200)
	if body["web_login_available"] != false {
		t.Fatalf("an account without cloud sync can't log in on the web: %v", body)
	}
	if r := anon.do("POST", "/auth/password/reset", map[string]any{"token": token, "new_password": "reset-password-2"}, ""); r.code() != "reset_token_invalid" {
		t.Fatalf("token reused: %s", r.Raw)
	}
	// A reset signs out every device and never creates a session (R-AUTH-4, D-27).
	if r := a.do("GET", "/me", nil, ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("session survived the reset: %d", r.Status)
	}
	if r := anon.do("GET", "/me", nil, ""); r.Status != http.StatusUnauthorized {
		t.Fatal("the reset created a session")
	}
	// A newer request invalidates earlier tokens.
	since = time.Now()
	expect(t, anon.do("POST", "/auth/password/forgot", map[string]any{"email": email}, ""), http.StatusAccepted)
	first := tokenRe.FindStringSubmatch(waitForMail(t, email, "Reset", since))[1]
	time.Sleep(1100 * time.Millisecond)
	since = time.Now()
	expect(t, anon.do("POST", "/auth/password/forgot", map[string]any{"email": email}, ""), http.StatusAccepted)
	waitForMail(t, email, "Reset", since)
	if r := anon.do("POST", "/auth/password/reset/validate", map[string]any{"token": first}, ""); r.code() != "reset_token_invalid" {
		t.Fatalf("an older token still works: %s", r.Raw)
	}
	resetRateLimits(t)
}

func TestSessions(t *testing.T) {
	email := uniqueEmail("sessions")
	a, _ := newAppAccount(t, email)
	b := loginApp(t, email, testPassword)
	c := loginApp(t, email, testPassword)
	list := expect(t, a.do("GET", "/me/sessions", nil, ""), 200)["sessions"].([]any)
	if len(list) != 3 {
		t.Fatalf("sessions: %d", len(list))
	}
	var otherID string
	current := 0
	for _, x := range list {
		s := x.(map[string]any)
		if s["current"] == true {
			current++
		} else if otherID == "" {
			otherID = s["id"].(string)
		}
	}
	if current != 1 {
		t.Fatalf("current flags: %d", current)
	}
	expect(t, a.do("DELETE", "/me/sessions/"+otherID, nil, ""), http.StatusNoContent)
	revoked := 0
	for _, x := range []*client{b, c} {
		if x.do("GET", "/me", nil, "").Status == http.StatusUnauthorized {
			revoked++
		}
	}
	if revoked != 1 {
		t.Fatalf("revoking one session signed out %d", revoked)
	}
	// Sessions of another account are invisible (R-MUT-6).
	other, _ := newAppAccount(t, uniqueEmail("other"))
	if r := other.do("DELETE", "/me/sessions/"+otherID, nil, ""); r.Status != http.StatusNotFound {
		t.Fatalf("revoking another account's session: %d", r.Status)
	}
	expect(t, a.do("POST", "/me/sessions/revoke-all", nil, ""), http.StatusNoContent)
	if a.do("GET", "/me", nil, "").Status != http.StatusUnauthorized {
		t.Fatal("revoke-all kept this session")
	}
	// Logout revokes only the current session.
	d := loginApp(t, email, testPassword)
	e := loginApp(t, email, testPassword)
	expect(t, d.do("POST", "/auth/logout", nil, ""), http.StatusNoContent)
	if d.do("GET", "/me", nil, "").Status != http.StatusUnauthorized || e.do("GET", "/me", nil, "").Status != 200 {
		t.Fatal("logout revoked the wrong sessions")
	}
}

func TestTermsGate(t *testing.T) {
	c, acc := newAppAccount(t, uniqueEmail("terms"))
	if _, err := deps.Pool.Exec(context.Background(), `UPDATE users SET terms_version = '2025-old' WHERE id = $1`, acc["id"]); err != nil {
		t.Fatal(err)
	}
	me := expect(t, c.do("GET", "/me", nil, ""), 200)
	if me["terms_outdated"] != true {
		t.Fatalf("terms_outdated: %v", me)
	}
	if r := c.do("GET", "/me/playlists", nil, ""); r.code() != "terms_outdated" {
		t.Fatalf("nothing else is reachable before accepting: %s", r.Raw)
	}
	if r := c.do("POST", "/me/terms", map[string]any{"terms_version": "2025-old"}, ""); r.code() != "terms_required" {
		t.Fatalf("accepting a stale version: %s", r.Raw)
	}
	me = expect(t, c.do("POST", "/me/terms", map[string]any{"terms_version": "2026-10-draft"}, ""), 200)
	if me["terms_outdated"] != false {
		t.Fatalf("after accepting: %v", me)
	}
	expect(t, c.do("GET", "/me/playlists", nil, ""), 200)
}

func TestSuspension(t *testing.T) {
	email := uniqueEmail("suspend")
	c, acc := newAppAccount(t, email)
	ctx := context.Background()
	if _, err := deps.Pool.Exec(ctx, `UPDATE users SET suspended_at = now() WHERE id = $1`, acc["id"]); err != nil {
		t.Fatal(err)
	}
	if r := c.do("GET", "/me/playlists", nil, ""); r.code() != "account_suspended" {
		t.Fatalf("suspended request: %s", r.Raw)
	}
	if r := newClient(t, "desktop").do("POST", "/auth/login", map[string]any{"email": email, "password": testPassword, "device": map[string]any{"installation_id": "s"}}, ""); r.code() != "account_suspended" {
		t.Fatalf("suspended login: %s", r.Raw)
	}
	_, _ = deps.Pool.Exec(ctx, `UPDATE users SET suspended_at = NULL WHERE id = $1`, acc["id"])
}

func TestLocaleAppliesToAccount(t *testing.T) {
	c, _ := newAppAccount(t, uniqueEmail("locale"))
	cursor := expect(t, c.do("GET", "/me/catalog/snapshot", nil, ""), 200)["cursor"].(float64)
	me := expect(t, c.mutate("PATCH", "/me", map[string]any{"locale": "pt-BR"}), 200)
	if me["locale"] != "pt-BR" {
		t.Fatalf("locale: %v", me)
	}
	if r := c.mutate("PATCH", "/me", map[string]any{"locale": "fr"}); r.code() != "invalid_field" {
		t.Fatalf("unsupported locale: %s", r.Raw)
	}
	ch := expect(t, c.do("GET", "/me/catalog/changes?cursor="+itoa(int(cursor)), nil, ""), 200)
	if ch["account_changed"] != true {
		t.Fatal("other devices aren't told the language changed")
	}
}

func TestReauthenticationAndAccountDeletion(t *testing.T) {
	requireFixtures(t, "mp3")
	email := syncEmails[0]
	c := newWebSyncAccount(t, 0)
	app := loginApp(t, email, testPassword)
	uid := userID(t, c)
	// Give the account audio in the bucket and some organization.
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "published", 20*time.Second, func() bool { tr := track(c, u.trackID); return tr != nil && tr["state"] == "ready" })
	contentID := track(c, u.trackID)["current_content_id"].(string)
	expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Doomed"}), 201)

	if r := c.do("POST", "/auth/reauthenticate", map[string]any{"password": "wrong-password!", "purpose": "delete_account"}, ""); r.code() != "invalid_credentials" {
		t.Fatalf("reauth with a wrong password: %s", r.Raw)
	}
	if r := c.mutate("POST", "/me/account-deletion", map[string]any{"proof": "made-up", "confirmed": true}); r.code() != "reauthentication_required" {
		t.Fatalf("deletion without a proof: %s", r.Raw)
	}
	proof := expect(t, c.do("POST", "/auth/reauthenticate", map[string]any{"password": testPassword, "purpose": "delete_account"}, ""), 200)["proof"].(string)
	// A proof is bound to the session that asked for it.
	if r := app.mutate("POST", "/me/account-deletion", map[string]any{"proof": proof, "confirmed": true}); r.code() != "reauthentication_required" {
		t.Fatalf("proof used from another session: %s", r.Raw)
	}
	if r := c.mutate("POST", "/me/account-deletion", map[string]any{"proof": proof, "confirmed": false}); r.code() != "invalid_field" {
		t.Fatalf("deletion without confirmation: %s", r.Raw)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := sseEvents(t, app, ctx)
	nextEvent(t, events, "ready", 5*time.Second)
	expect(t, c.mutate("POST", "/me/account-deletion", map[string]any{"proof": proof, "confirmed": true}), http.StatusAccepted)
	// Every device gets the terminal hint and loses its session.
	nextEvent(t, events, "account_deleted", 10*time.Second)
	if r := app.do("GET", "/me", nil, ""); r.Status == 200 {
		t.Fatal("another device's session survived the deletion")
	}
	waitFor(t, "user row deleted", 30*time.Second, func() bool {
		var n int
		_ = deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, uid).Scan(&n)
		return n == 0
	})
	if _, err := deps.Store.Head(context.Background(), storage.FinalKey(uid, contentID)); err == nil {
		t.Fatal("the song's audio is still in the bucket")
	}
	var phase string
	if err := deps.Pool.QueryRow(context.Background(), `SELECT phase FROM account_deletion_requests WHERE original_user_id = $1`, uid).Scan(&phase); err != nil || phase != "completed" {
		t.Fatalf("deletion ledger: %q %v", phase, err)
	}
	if r := newClient(t, "web").do("POST", "/auth/login", map[string]any{"email": email, "password": testPassword}, ""); r.code() != "invalid_credentials" {
		t.Fatalf("login after deletion: %s", r.Raw)
	}
	// The same email can register again, as a new account.
	again := newWebSyncAccount(t, 0)
	if userID(t, again) == uid {
		t.Fatal("re-registration reused the deleted account's ID")
	}
}

func TestAccessAcrossAccounts(t *testing.T) {
	a := signIn(t, owner)
	p := expect(t, a.mutate("POST", "/me/playlists", map[string]any{"name": "Private"}), 201)["playlist"].(map[string]any)
	b, _ := newAppAccount(t, uniqueEmail("intruder"))
	pid := p["id"].(string)
	for _, r := range []resp{
		b.mutate("PATCH", "/me/playlists/"+pid, map[string]any{"name": "Mine now", "expected_revision": p["revision"]}),
		b.mutate("DELETE", "/me/playlists/"+pid, nil),
		b.mutate("POST", "/me/playlists/"+pid+"/songs", map[string]any{"track_ids": []string{pid}, "expected_revision": 1}),
	} {
		if r.code() != "not_found" {
			t.Fatalf("cross-account access: %s", r.Raw)
		}
	}
	if q := expect(t, b.do("GET", "/me/queue-ids?source=playlist:"+pid, nil, ""), 200); len(q["track_ids"].([]any)) != 0 {
		t.Fatal("queue IDs leaked another account's playlist")
	}
}

func itoa(n int) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func sha(path string) string {
	data, _ := os.ReadFile(path)
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// Installed apps get a restricted cleanup credential at login (R-AUTH-6, §7.11):
// it reads only this account's deletion status, outlives the sessions and the
// user row, and acknowledges the device's purge.
func TestCleanupCredentials(t *testing.T) {
	resetRateLimits(t)
	email := uniqueEmail("cleanup")
	install := uuid.NewString()
	auth := func(path string, extra map[string]any) map[string]any {
		body := map[string]any{"email": email, "password": testPassword, "device": map[string]any{"installation_id": install, "label": "Test PC"}}
		for k, v := range extra {
			body[k] = v
		}
		status := http.StatusOK
		if path == "/auth/register" {
			status = http.StatusCreated
		}
		return expect(t, newClient(t, "desktop").do("POST", path, body, ""), status)
	}
	status := func(cred string) resp {
		return newClient(t, "desktop").do("POST", "/account-deletion/status", map[string]any{"credential": cred}, "")
	}
	ack := func(cred string) resp {
		return newClient(t, "desktop").do("POST", "/account-deletion/ack", map[string]any{"credential": cred}, "")
	}

	reg := auth("/auth/register", map[string]any{"accepted_terms": true, "terms_version": "2026-10-draft"})
	first, _ := reg["cleanup_credential"].(string)
	if first == "" || first == reg["token"] {
		t.Fatalf("registration from an installed app: no separate cleanup credential: %v", reg)
	}
	if b := expect(t, status(first), 200); b["deleted"] != false {
		t.Fatalf("status of a live account: %v", b)
	}
	if r := status("made-up"); r.Status != 404 {
		t.Fatalf("unknown credential: %d", r.Status)
	}
	if r := ack(first); r.Status != 404 {
		t.Fatalf("ack before any deletion: %d", r.Status)
	}

	// A new login on the same installation replaces the device's credential.
	login := auth("/auth/login", nil)
	second := login["cleanup_credential"].(string)
	if second == first || status(first).Status != 404 || status(second).Status != 200 {
		t.Fatal("the device's earlier credential still works after a new login")
	}
	app := newClient(t, "desktop")
	app.token = login["token"].(string)
	uid := userID(t, app)
	// A second installation has its own receipt; acknowledging this one leaves it.
	loginApp(t, email, testPassword)

	proof := expect(t, app.do("POST", "/auth/reauthenticate", map[string]any{"password": testPassword, "purpose": "delete_account"}, ""), 200)["proof"].(string)
	expect(t, app.mutate("POST", "/me/account-deletion", map[string]any{"proof": proof, "confirmed": true}), http.StatusAccepted)

	if b := expect(t, status(second), 200); b["deleted"] != true {
		t.Fatalf("status after deletion: %v", b)
	}
	// It keeps answering after the user row is gone (offline devices come back later).
	waitFor(t, "user row deleted", 30*time.Second, func() bool {
		var n int
		_ = deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, uid).Scan(&n)
		return n == 0
	})
	if b := expect(t, status(second), 200); b["deleted"] != true {
		t.Fatalf("status after the user row is gone: %v", b)
	}
	if r := ack(second); r.Status != http.StatusNoContent {
		t.Fatalf("ack: %d %s", r.Status, r.Raw)
	}
	if r := ack(second); r.Status != http.StatusNoContent {
		t.Fatalf("a repeated ack must succeed too: %d", r.Status)
	}
	var acked int
	_ = deps.Pool.QueryRow(context.Background(), `SELECT count(*) FROM device_cleanup_receipts WHERE original_user_id = $1 AND acknowledged_at IS NOT NULL`, uid).Scan(&acked)
	if acked != 1 {
		t.Fatalf("acknowledged receipts: %d, want only this device's", acked)
	}
}

// The web never gets a cleanup credential: it stores nothing to clean (R-WEB).
func TestNoCleanupCredentialOnTheWeb(t *testing.T) {
	resetRateLimits(t)
	r := newClient(t, "web").do("POST", "/auth/login", map[string]any{"email": owner, "password": "saltwater-hymn"}, "")
	if r.Status != 200 {
		r = newClient(t, "web").do("POST", "/auth/register", map[string]any{"email": owner, "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft"}, "")
	}
	if r.Status/100 != 2 {
		t.Fatalf("web sign-in: %d %s", r.Status, r.Raw)
	}
	if _, ok := r.Body["cleanup_credential"]; ok {
		t.Fatal("the web got a cleanup credential")
	}
}
