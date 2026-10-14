//go:build integration

// Package e2e runs the API, the worker and the upload pipeline in-process against
// the local PostgreSQL and S3 from docker-compose (§4.4 testing). Run with:
//
//	go test -tags integration ./internal/e2e/
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/server"
	"github.com/henriquepaulalima/song-sleeve/server/internal/worker"
)

var (
	srv      *httptest.Server
	fixtures = map[string]string{}
	owner    string
	// syncEmails are extra allowlisted accounts for tests that need their own
	// cloud-sync account (deletion, conflicts).
	syncEmails []string
	deps       *app.Deps
)

func TestMain(m *testing.M) {
	owner = fmt.Sprintf("owner-%s@example.com", uuid.NewString()[:8])
	run := uuid.NewString()[:8]
	for i := 0; i < 4; i++ {
		syncEmails = append(syncEmails, fmt.Sprintf("sync%d-%s@example.com", i, run))
	}
	os.Setenv("CLOUD_SYNC_ALLOWLIST", owner+","+strings.Join(syncEmails, ","))
	os.Setenv("STALL_TIMEOUT", "3s")
	os.Setenv("UPLOAD_GRANT_TTL", "2s")
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	// A database of their own, so a running dev API (and its workers) never
	// shares jobs or data with the tests.
	if err := useTestDatabase(ctx); err != nil {
		fmt.Println("skipping integration tests: infrastructure unavailable:", err)
		os.Exit(0)
	}
	d, err := app.Bootstrap(ctx, true)
	if err != nil {
		fmt.Println("skipping integration tests: infrastructure unavailable:", err)
		os.Exit(0)
	}
	deps = d
	// Test database only: start from fresh rate-limit windows.
	if _, err := d.Pool.Exec(ctx, `DELETE FROM rate_limit_buckets`); err != nil {
		panic(err)
	}
	d.River, err = worker.NewClient(d)
	if err != nil {
		panic(err)
	}
	if err := d.River.Start(ctx); err != nil {
		panic(err)
	}
	go d.Hub.Run(ctx)
	srv = httptest.NewServer(server.New(d))

	dir, _ := os.MkdirTemp("", "songsleeve-fixtures")
	for name, args := range map[string][]string{
		"mp3":  {"-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-b:a", "96k"},
		"flac": {"-f", "lavfi", "-i", "sine=frequency=550:duration=5", "-c:a", "flac"},
		"aac":  {"-f", "lavfi", "-i", "sine=frequency=660:duration=4", "-c:a", "aac", "-b:a", "64k"},
		"wav":  {"-f", "lavfi", "-i", "sine=frequency=330:duration=3"},
		"ogg":  {"-f", "lavfi", "-i", "sine=frequency=330:duration=3", "-c:a", "libvorbis"},
	} {
		ext := map[string]string{"aac": "m4a"}[name]
		if ext == "" {
			ext = name
		}
		out := filepath.Join(dir, "tone."+ext)
		cmd := exec.Command("ffmpeg", append(append([]string{"-loglevel", "error", "-y"}, args...), out)...)
		if err := cmd.Run(); err == nil {
			fixtures[name] = out
		}
	}
	code := m.Run()
	srv.Close()
	cancel()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type client struct {
	t        *testing.T
	http     *http.Client
	platform string
	csrf     string
	token    string
}

func newClient(t *testing.T, platform string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}, platform: platform}
}

type resp struct {
	Status int
	Body   map[string]any
	Raw    []byte
}

func (r resp) code() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (c *client) do(method, path string, body any, key string) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+"/api"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Platform", c.platform)
	req.Header.Set("X-Client-Version", "0.1.0")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: raw}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func (c *client) mutate(method, path string, body any) resp {
	return c.do(method, path, body, uuid.NewString())
}

func expect(t *testing.T, r resp, status int) map[string]any {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d (want %d): %s", r.Status, status, r.Raw)
	}
	return r.Body
}

var (
	ownerOnce   sync.Once
	ownerShared *client
)

// signIn signs the allowlisted owner in once per run and hands each test a client
// sharing that web session.
func signIn(t *testing.T, email string) *client {
	ownerOnce.Do(func() { ownerShared = signInFresh(t, email) })
	return &client{t: t, http: ownerShared.http, platform: "web", csrf: ownerShared.csrf}
}

func signInFresh(t *testing.T, email string) *client {
	c := newClient(t, "web")
	r := c.do("POST", "/auth/register", map[string]any{"email": email, "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft"}, "")
	if r.Status == http.StatusConflict {
		r = c.do("POST", "/auth/login", map[string]any{"email": email, "password": "saltwater-hymn"}, "")
	}
	if r.Status/100 != 2 {
		t.Fatalf("sign in: %d %s", r.Status, r.Raw)
	}
	c.csrf = r.Body["account"].(map[string]any)["csrf_token"].(string)
	return c
}

func probeMs(t *testing.T, path string) int64 {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return int64(f * 1000)
}

type upload struct {
	trackID string
	opID    string
	op      map[string]any
	data    []byte
}

func startUpload(t *testing.T, c *client, format, path string, sha string) upload {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		s := sha256.Sum256(data)
		sha = hex.EncodeToString(s[:])
	}
	body := expect(t, c.mutate("POST", "/me/tracks", map[string]any{
		"name": "Tone " + format, "artist": map[string]any{"name": "Test Artist"},
		"duration_ms": probeMs(t, path), "byte_size": len(data), "format": format, "sha256": sha,
	}), http.StatusCreated)
	op := body["operation"].(map[string]any)
	return upload{trackID: body["track"].(map[string]any)["id"].(string), opID: op["id"].(string), op: op, data: data}
}

func sendParts(t *testing.T, c *client, opID string, op map[string]any, data []byte) {
	t.Helper()
	size := int(op["part_size"].(float64))
	count := int(op["part_count"].(float64))
	nums := make([]int, count)
	for i := range nums {
		nums[i] = i + 1
	}
	g := expect(t, c.do("POST", "/me/operations/"+opID+"/upload-grant", map[string]any{"part_numbers": nums}, ""), 200)
	var parts []map[string]any
	for _, p := range g["parts"].([]any) {
		pm := p.(map[string]any)
		n := int(pm["part_number"].(float64))
		end := min(n*size, len(data))
		req, _ := http.NewRequest("PUT", pm["url"].(string), bytes.NewReader(data[(n-1)*size:end]))
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("part %d: %v %v", n, err, res)
		}
		res.Body.Close()
		parts = append(parts, map[string]any{"part_number": n, "etag": res.Header.Get("ETag")})
		expect(t, c.do("POST", "/me/operations/"+opID+"/heartbeat", map[string]any{"confirmed_bytes": end}, ""), 200)
	}
	expect(t, c.do("POST", "/me/operations/"+opID+"/uploaded", map[string]any{"parts": parts}, ""), 200)
}

func track(c *client, id string) map[string]any {
	r := c.do("GET", "/me/tracks", nil, "")
	for _, x := range r.Body["tracks"].([]any) {
		if m := x.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

func waitFor(t *testing.T, what string, timeout time.Duration, fn func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func requireFixtures(t *testing.T, names ...string) {
	for _, n := range names {
		if fixtures[n] == "" {
			t.Skip("ffmpeg fixtures unavailable")
		}
	}
}

func TestAccessGates(t *testing.T) {
	web := newClient(t, "web")
	r := web.do("POST", "/auth/register", map[string]any{"email": "x-" + uuid.NewString()[:6] + "@example.com", "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft"}, "")
	if r.Status != 403 || r.code() != "web_app_unavailable" {
		t.Fatalf("web registration outside the allowlist: %d %s", r.Status, r.Raw)
	}
	app := newClient(t, "desktop")
	body := expect(t, app.do("POST", "/auth/register", map[string]any{
		"email": "x-" + uuid.NewString()[:6] + "@example.com", "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft",
		"device": map[string]any{"installation_id": uuid.NewString(), "label": "Test PC"},
	}, ""), 201)
	app.token = body["token"].(string)
	if r := app.mutate("POST", "/me/tracks", map[string]any{"name": "x", "duration_ms": 1000, "byte_size": 10, "format": "mp3", "sha256": strings.Repeat("0", 64)}); r.code() != "cloud_sync_unavailable" {
		t.Fatalf("sync without cloud sync: %s", r.Raw)
	}
	expect(t, app.mutate("POST", "/me/playlists", map[string]any{"name": "Local stuff"}), 201)

	// A bearer token is never accepted as a cookie.
	req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
	req.Header.Set("X-Client-Platform", "web")
	req.Header.Set("X-Client-Version", "0.1.0")
	req.AddCookie(&http.Cookie{Name: "ss_session", Value: app.token})
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("bearer token as cookie: %d", res.StatusCode)
	}
	// Terms and password policy.
	r = newClient(t, "desktop").do("POST", "/auth/register", map[string]any{"email": "y@example.com", "password": "saltwater-hymn", "accepted_terms": false, "terms_version": "2026-10-draft", "device": map[string]any{"installation_id": "a"}}, "")
	if r.code() != "terms_required" {
		t.Fatalf("no terms: %s", r.Raw)
	}
	for pw, code := range map[string]string{"123456789": "password_invalid", "qwertyuiop": "password_too_common"} {
		r = newClient(t, "desktop").do("POST", "/auth/register", map[string]any{"email": "z@example.com", "password": pw, "accepted_terms": true, "terms_version": "2026-10-draft", "device": map[string]any{"installation_id": "a"}}, "")
		if r.code() != code {
			t.Fatalf("password %q: %s", pw, r.Raw)
		}
	}
}

func TestMutationContract(t *testing.T) {
	c := signIn(t, owner)
	if r := c.do("POST", "/me/playlists", map[string]any{"name": "No key"}, ""); r.code() != "idempotency_key_required" {
		t.Fatalf("missing key: %s", r.Raw)
	}
	key := uuid.NewString()
	a := expect(t, c.do("POST", "/me/playlists", map[string]any{"name": "Once"}, key), 201)
	b := expect(t, c.do("POST", "/me/playlists", map[string]any{"name": "Once"}, key), 201)
	if a["playlist"].(map[string]any)["id"] != b["playlist"].(map[string]any)["id"] {
		t.Fatal("a repeated request ID created a second playlist")
	}
	st := expect(t, c.do("GET", "/me/mutations/"+key, nil, ""), 200)
	if st["status"] != "committed" {
		t.Fatalf("mutation status: %v", st)
	}
	if st := expect(t, c.do("GET", "/me/mutations/"+uuid.NewString(), nil, ""), 200); st["status"] != "not_applied" {
		t.Fatalf("unknown request status: %v", st)
	}
	id := a["playlist"].(map[string]any)["id"].(string)
	if r := c.mutate("PATCH", "/me/playlists/"+id, map[string]any{"name": "Stale", "expected_revision": 0}); r.code() != "revision_conflict" {
		t.Fatalf("stale revision: %s", r.Raw)
	}
	if r := c.mutate("POST", "/me/playlists", map[string]any{"name": "x", "parent_id": uuid.NewString()}); r.code() != "invalid_field" {
		t.Fatalf("parent field accepted: %s", r.Raw)
	}
	if r := c.mutate("POST", "/me/playlists", map[string]any{"name": strings.Repeat("a", 101)}); r.code() != "invalid_field" {
		t.Fatalf("too-long name accepted: %s", r.Raw)
	}
	saved := c.csrf
	c.csrf = "bad"
	if r := c.mutate("POST", "/me/playlists", map[string]any{"name": "csrf"}); r.code() != "csrf_failed" {
		t.Fatalf("missing CSRF accepted: %s", r.Raw)
	}
	c.csrf = saved
}

func TestLimits(t *testing.T) {
	c := signIn(t, owner)
	sha := strings.Repeat("a", 64)
	cases := []struct {
		ms, size int64
		code     string
	}{
		{1800000, 134217728, ""},
		{1800001, 1000, "song_too_long"},
		{1000, 134217729, "song_too_large"},
		{0, 1000, "invalid_audio_metadata"},
	}
	for _, cs := range cases {
		r := c.mutate("POST", "/me/tracks", map[string]any{"name": "Limit", "duration_ms": cs.ms, "byte_size": cs.size, "format": "mp3", "sha256": sha})
		if cs.code == "" {
			body := expect(t, r, 201)
			// Clean up: cancelling a pending track fails the sync.
			expect(t, c.mutate("DELETE", "/me/tracks/"+body["track"].(map[string]any)["id"].(string)+"?reason=cancel_pending", nil), 200)
			continue
		}
		if r.code() != cs.code {
			t.Fatalf("%d ms / %d bytes: %s", cs.ms, cs.size, r.Raw)
		}
	}
}

func TestUploadPipelineAllFormats(t *testing.T) {
	requireFixtures(t, "mp3", "flac", "aac", "wav")
	c := signIn(t, owner)
	for _, f := range []string{"mp3", "flac", "aac", "wav"} {
		u := startUpload(t, c, f, fixtures[f], "")
		if tr := track(c, u.trackID); tr == nil || tr["state"] != "pending_upload" {
			t.Fatalf("%s: pending track not listed", f)
		}
		sendParts(t, c, u.opID, u.op, u.data)
		waitFor(t, f+" published", 20*time.Second, func() bool {
			tr := track(c, u.trackID)
			return tr != nil && tr["state"] == "ready" && tr["operation"] == nil
		})
		tr := track(c, u.trackID)
		g := expect(t, c.do("POST", "/me/content/"+tr["current_content_id"].(string)+"/download-grant", nil, ""), 200)
		res, err := http.Get(g["url"].(string))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !bytes.Equal(got, u.data) {
			t.Fatalf("%s: downloaded bytes differ", f)
		}
	}
}

func TestRejectionsFailTheSync(t *testing.T) {
	requireFixtures(t, "mp3", "ogg")
	c := signIn(t, owner)
	// A wrong declared checksum is caught by the worker.
	u := startUpload(t, c, "mp3", fixtures["mp3"], strings.Repeat("b", 64))
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "checksum rejection", 20*time.Second, func() bool { return track(c, u.trackID) == nil })
	// An unsupported codec declared as MP3 is caught from the bytes (D-8).
	u = startUpload(t, c, "mp3", fixtures["ogg"], "")
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "codec rejection", 20*time.Second, func() bool { return track(c, u.trackID) == nil })
	ch := expect(t, c.do("GET", "/me/catalog/changes?cursor=0", nil, ""), 200)
	failed := 0
	for _, e := range ch["events"].([]any) {
		if e.(map[string]any)["event_type"] == "track_sync_failed" {
			failed++
		}
	}
	if failed < 2 {
		t.Fatalf("expected track_sync_failed events, got %d", failed)
	}
	summ := expect(t, c.do("GET", "/me/summary", nil, ""), 200)
	if summ["storage_reserved_bytes"].(float64) != 0 {
		t.Fatalf("reservation not released: %v", summ)
	}
}

func TestStallFailsTheSync(t *testing.T) {
	requireFixtures(t, "mp3")
	c := signIn(t, owner)
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	// No parts and no heartbeats: the sync fails after the stall timeout.
	waitFor(t, "stall", 60*time.Second, func() bool { return track(c, u.trackID) == nil })
}

func TestReplacementAndDelete(t *testing.T) {
	requireFixtures(t, "mp3", "flac")
	c := signIn(t, owner)
	u := startUpload(t, c, "mp3", fixtures["mp3"], "")
	sendParts(t, c, u.opID, u.op, u.data)
	waitFor(t, "published", 20*time.Second, func() bool {
		tr := track(c, u.trackID)
		return tr != nil && tr["state"] == "ready" && tr["operation"] == nil
	})
	first := track(c, u.trackID)
	oldContent := first["current_content_id"].(string)

	// A failed replacement keeps the old content and holds the slot until its
	// upload grants expire (R-SYNC-4).
	data, _ := os.ReadFile(fixtures["flac"])
	sum := sha256.Sum256(data)
	rep := func() map[string]any {
		return expect(t, c.mutate("POST", "/me/tracks/"+u.trackID+"/replacement", map[string]any{
			"expected_revision": track(c, u.trackID)["revision"], "name": "Replaced", "artist": nil,
			"duration_ms": probeMs(t, fixtures["flac"]), "byte_size": len(data), "format": "flac", "sha256": hex.EncodeToString(sum[:]),
		}), 202)["operation"].(map[string]any)
	}
	op := rep()
	expect(t, c.do("POST", "/me/operations/"+op["id"].(string)+"/upload-grant", map[string]any{"part_numbers": []int{1}}, ""), 200)
	expect(t, c.mutate("POST", "/me/operations/"+op["id"].(string)+"/cancel", nil), 200)
	if tr := track(c, u.trackID); tr["current_content_id"] != oldContent || tr["name"] == "Replaced" {
		t.Fatal("a cancelled replacement changed the song")
	}
	op2 := rep()
	if op2["state"] != "waiting" {
		t.Fatalf("the next replacement started before the old grant expired: %v", op2["state"])
	}
	waitFor(t, "promotion after grant expiry", 60*time.Second, func() bool {
		r := c.do("GET", "/me/operations/"+op2["id"].(string), nil, "")
		return r.Body["operation"].(map[string]any)["state"] == "active"
	})
	sendParts(t, c, op2["id"].(string), op2, data)
	waitFor(t, "replacement published", 20*time.Second, func() bool {
		tr := track(c, u.trackID)
		return tr["current_content_id"] != oldContent && tr["operation"] == nil
	})
	tr := track(c, u.trackID)
	if tr["name"] != "Replaced" || tr["artist_id"] != nil || tr["format"] != "flac" {
		t.Fatalf("replacement metadata not applied: %v", tr)
	}
	if r := c.do("POST", "/me/content/"+oldContent+"/download-grant", nil, ""); r.Status != 404 {
		t.Fatalf("retired content still playable: %d", r.Status)
	}

	// Delete removes the song and frees its storage.
	before := expect(t, c.do("GET", "/me/summary", nil, ""), 200)["storage_used_bytes"].(float64)
	expect(t, c.mutate("DELETE", "/me/tracks/"+u.trackID, nil), 200)
	after := expect(t, c.do("GET", "/me/summary", nil, ""), 200)["storage_used_bytes"].(float64)
	if track(c, u.trackID) != nil || after >= before {
		t.Fatalf("delete: track still listed or storage not freed (%v → %v)", before, after)
	}
}

func TestPlaylistsAndOrder(t *testing.T) {
	c := signIn(t, owner)
	p := expect(t, c.mutate("POST", "/me/playlists", map[string]any{"name": "Order"}), 201)["playlist"].(map[string]any)
	g := expect(t, c.mutate("POST", "/me/groups", map[string]any{"name": "Grp"}), 201)["group"].(map[string]any)
	g = expect(t, c.mutate("POST", "/me/groups/"+g["id"].(string)+"/playlists", map[string]any{"playlist_ids": []string{p["id"].(string)}, "expected_revision": g["revision"]}), 200)["group"].(map[string]any)
	if len(g["playlists"].([]any)) != 1 {
		t.Fatal("playlist not added to group")
	}
	expect(t, c.mutate("DELETE", "/me/playlists/"+p["id"].(string), nil), 200)
	gs := expect(t, c.do("GET", "/me/groups", nil, ""), 200)["groups"].([]any)
	for _, x := range gs {
		if m := x.(map[string]any); m["id"] == g["id"] && len(m["playlists"].([]any)) != 0 {
			t.Fatal("a deleted playlist stayed in its group")
		}
	}
	prefs := expect(t, c.do("GET", "/me/preferences/library-view", nil, ""), 200)
	if r := c.mutate("PUT", "/me/preferences/library-view", map[string]any{"songs": false, "playlists": false, "groups": false, "expected_revision": prefs["revision"]}); r.code() != "invalid_field" {
		t.Fatalf("library filter with nothing on: %s", r.Raw)
	}
}

// useTestDatabase points DATABASE_URL at songsleeve_test (or TEST_DATABASE_URL),
// creating it on the local server when missing.
func useTestDatabase(ctx context.Context) error {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return os.Setenv("DATABASE_URL", u)
	}
	base := "postgres://songsleeve:songsleeve@localhost:55432/songsleeve?sslmode=disable"
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE DATABASE songsleeve_test`); err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return os.Setenv("DATABASE_URL", strings.Replace(base, "/songsleeve?", "/songsleeve_test?", 1))
}
