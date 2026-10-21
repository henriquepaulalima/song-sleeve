//go:build integration

package shell

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/testserver"
)

var (
	baseURL   string
	cloudUser = fmt.Sprintf("shell-%s@example.com", uuid.NewString()[:8])
)

func TestMain(m *testing.M) { testserver.Main(m, []string{cloudUser}, &baseURL) }

type events struct {
	mu    sync.Mutex
	names []string
}

func (e *events) emit(name string, _ any) {
	e.mu.Lock()
	e.names = append(e.names, name)
	e.mu.Unlock()
}

func (e *events) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, x := range e.names {
		if x == name {
			n++
		}
	}
	return n
}

// app is one desktop installation: a shell behind an httptest server, standing in
// for the webview.
type app struct {
	t    *testing.T
	sh   *Shell
	srv  *httptest.Server
	ev   *events
	root string
}

func newApp(t *testing.T, root, apiBase string) *app {
	t.Helper()
	ev := &events{}
	sh, err := New(Config{
		APIBase: apiBase, Version: "0.1.0", DataDir: filepath.Join(root, "data"), SleeveRoot: filepath.Join(root, "Song Sleeve"),
		DeviceLabel: "Test PC", FileCredentials: true, Hooks: Hooks{Emit: ev.emit, Open: func(string) error { return nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{t: t, sh: sh, srv: httptest.NewServer(sh), ev: ev, root: root}
	t.Cleanup(func() {
		a.srv.Close()
		sh.Shutdown()
	})
	return a
}

// call sends a request the way the UI's API client does and decodes the answer.
func (a *app) call(method, path string, body any, out any) (int, http.Header) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Platform", "desktop")
	req.Header.Set("X-Client-Version", "0.1.0")
	if method != "GET" && strings.HasPrefix(path, "/api/me") {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			a.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return res.StatusCode, res.Header
}

func (a *app) must(method, path string, body any, out any) {
	a.t.Helper()
	var raw json.RawMessage
	status, _ := a.call(method, path, body, &raw)
	if status >= 300 {
		a.t.Fatalf("%s %s: %d %s", method, path, status, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			a.t.Fatal(err)
		}
	}
}

func (a *app) catalog() *catalogDoc {
	a.t.Helper()
	var doc catalogDoc
	a.must("GET", "/local/catalog", nil, &doc)
	return &doc
}

func (a *app) state() stateDoc {
	var st stateDoc
	a.must("GET", "/local/state", nil, &st)
	return st
}

func wav(t *testing.T, path string, seconds float64, fill byte) {
	t.Helper()
	n := int(8000 * seconds)
	b := make([]byte, 44+n)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 8000)
	binary.LittleEndian.PutUint32(b[28:], 8000)
	binary.LittleEndian.PutUint16(b[32:], 1)
	binary.LittleEndian.PutUint16(b[34:], 8)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	for i := 44; i < len(b); i++ {
		b[i] = fill + byte(i%7)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (a *app) register(email string) {
	a.t.Helper()
	var auth map[string]json.RawMessage
	body := map[string]any{"email": email, "password": "saltwater-hymn", "accepted_terms": true, "terms_version": "2026-10-draft"}
	a.must("POST", "/api/auth/register", body, &auth)
	if _, ok := auth["token"]; ok {
		a.t.Fatal("the token reached the webview")
	}
}

// importSongs adds WAV files through the Add songs flow and returns their IDs.
func (a *app) importSongs(names ...string) []string {
	a.t.Helper()
	var paths []string
	for i, n := range names {
		p := filepath.Join(a.root, "Music", n+".wav")
		wav(a.t, p, 1+float64(i)/2, byte(i*13))
		paths = append(paths, p)
	}
	var an struct {
		Candidates []map[string]any `json:"candidates"`
	}
	a.must("POST", "/local/analyze", map[string]any{"paths": paths}, &an)
	if len(an.Candidates) != len(names) {
		a.t.Fatalf("analyze: %+v", an)
	}
	for _, c := range an.Candidates {
		c["artist"] = nil
	}
	a.must("POST", "/local/import", map[string]any{"items": an.Candidates}, nil)
	var ids []string
	for _, tr := range a.catalog().Tracks {
		if tr.Device.Local {
			ids = append(ids, tr.ID)
		}
	}
	return ids
}

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	end := time.Now().Add(40 * time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSignInLocalSongsAndOverlay(t *testing.T) {
	a := newApp(t, t.TempDir(), baseURL)
	if a.state().SignedIn {
		t.Fatal("signed in before registering")
	}
	email := fmt.Sprintf("shell-local-%s@example.com", uuid.NewString()[:8])
	a.register(email)
	st := a.state()
	if !st.SignedIn || st.Me.Email != email {
		t.Fatalf("state after register: %+v", st)
	}
	var me map[string]any
	a.must("GET", "/api/me", nil, &me)
	if me["email"] != email {
		t.Fatalf("me: %+v", me)
	}

	ids := a.importSongs("Harbor Lights", "Copper Sky")
	if len(ids) != 2 {
		t.Fatalf("local songs: %v", ids)
	}

	// Edit a local song through the server route; the shell answers it.
	var edited struct {
		Track catalogTrack `json:"track"`
	}
	a.must("PATCH", "/api/me/tracks/"+ids[0], map[string]any{"name": "Harbor Lights (live)", "artist": map[string]any{"name": "Marina Okafor"}, "expected_revision": 0}, &edited)
	if edited.Track.Name != "Harbor Lights (live)" || edited.Track.ArtistID == nil || !strings.HasPrefix(*edited.Track.ArtistID, localArtistPrefix) {
		t.Fatalf("edited: %+v", edited.Track)
	}
	found := false
	for _, ar := range a.catalog().Artists {
		found = found || ar.Name == "Marina Okafor"
	}
	if !found {
		t.Fatal("the local-only artist is missing from the catalog")
	}

	// A playlist on the server, a local song in its overlay.
	var pl struct {
		Playlist struct {
			ID       string `json:"id"`
			Revision int64  `json:"revision"`
			Items    []struct {
				TrackID string `json:"track_id"`
			} `json:"items"`
		} `json:"playlist"`
	}
	a.must("POST", "/api/me/playlists", map[string]any{"name": "Sunday Drive"}, &pl)
	a.must("POST", "/api/me/playlists/"+pl.Playlist.ID+"/songs", map[string]any{"track_ids": []string{ids[1], ids[0]}, "expected_revision": pl.Playlist.Revision}, &pl)
	if len(pl.Playlist.Items) != 2 {
		t.Fatalf("overlay items: %+v", pl.Playlist.Items)
	}
	// Reorder inside the merged playlist: move the second song to the top.
	second := pl.Playlist.Items[1].TrackID
	first := pl.Playlist.Items[0].TrackID
	a.must("PUT", "/api/me/playlists/"+pl.Playlist.ID+"/songs/order", map[string]any{"item_id": second, "after_id": nil, "before_id": first, "expected_revision": pl.Playlist.Revision}, &pl)
	if pl.Playlist.Items[0].TrackID != second {
		t.Fatalf("reorder: %+v", pl.Playlist.Items)
	}
	waitFor(t, "the playlist in the replica", func() bool {
		for _, p := range a.catalog().Playlists {
			if p.ID == pl.Playlist.ID && len(p.Items) == 2 {
				return true
			}
		}
		return false
	})

	// Playback from the source file, with seeking (Range).
	var play struct {
		URL string `json:"url"`
	}
	a.must("POST", "/local/play", map[string]any{"track_id": ids[1]}, &play)
	req, _ := http.NewRequest("GET", play.URL, nil)
	req.Header.Set("Range", "bytes=0-99")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || len(body) != 100 || res.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("media: %d %d %q", res.StatusCode, len(body), res.Header.Get("Content-Type"))
	}
	bad, _ := http.Get(strings.Replace(play.URL, st.MediaURL[len(st.MediaURL)-10:], "0000000000", 1))
	if bad.StatusCode != 404 {
		t.Fatalf("media without the secret: %d", bad.StatusCode)
	}
	bad.Body.Close()

	// Local delete keeps the source file (R-LOCAL-4).
	a.must("DELETE", "/api/me/tracks/"+ids[1]+"?reason=delete", nil, nil)
	if _, err := os.Stat(filepath.Join(a.root, "Music", "Copper Sky.wav")); err != nil {
		t.Fatal("the source file was touched:", err)
	}
	if n := len(a.catalog().Tracks); n != 1 {
		t.Fatalf("tracks after delete: %d", n)
	}
	if a.ev.count("catalog") == 0 {
		t.Fatal("no catalog events")
	}

	// A restart signs back in from the stored token.
	a.sh.Shutdown()
	b := newApp(t, a.root, baseURL)
	b.sh.Restore(context.Background())
	if st := b.state(); !st.SignedIn || st.Me.Email != email {
		t.Fatalf("after restart: %+v", st)
	}
	if n := len(b.catalog().Tracks); n != 1 {
		t.Fatalf("tracks after restart: %d", n)
	}

	// Offline: the account and its local songs still work, server changes report
	// connection_required (R-MUT-1).
	b.sh.Shutdown()
	off := newApp(t, a.root, "http://127.0.0.1:1/api")
	off.sh.Restore(context.Background())
	var offMe map[string]any
	off.must("GET", "/api/me", nil, &offMe)
	if offMe["email"] != email {
		t.Fatalf("offline me: %+v", offMe)
	}
	off.must("PATCH", "/api/me/tracks/"+ids[0], map[string]any{"name": "Offline edit", "expected_revision": 0}, nil)
	status, h := off.call("POST", "/api/me/playlists", map[string]any{"name": "Nope"}, nil)
	if status != 503 || h.Get(HeaderOffline) == "" {
		t.Fatalf("offline server change: %d", status)
	}
	off.sh.Shutdown()

	// Log out and remove from this device purges the namespace.
	c := newApp(t, a.root, baseURL)
	c.sh.Restore(context.Background())
	acct := c.state().Me.ID
	c.must("POST", "/local/logout", map[string]any{"remove": true}, nil)
	if c.state().SignedIn {
		t.Fatal("still signed in")
	}
	if _, err := os.Stat(filepath.Join(a.root, "data", "accounts", acct)); !os.IsNotExist(err) {
		t.Fatal("account data survived the purge:", err)
	}
	if _, err := os.Stat(filepath.Join(a.root, "Music", "Harbor Lights.wav")); err != nil {
		t.Fatal("the purge touched a source file:", err)
	}
}

func TestSyncThroughTheShell(t *testing.T) {
	a := newApp(t, t.TempDir(), baseURL)
	a.register(cloudUser)
	if !a.state().Me.CloudSyncEnabled {
		t.Fatal("cloud sync is off for the allowlisted account")
	}
	ids := a.importSongs("Low Tide")
	a.must("POST", "/local/sync", map[string]any{"ids": ids}, nil)
	var synced *catalogTrack
	waitFor(t, "the synced track", func() bool {
		for _, tr := range a.catalog().Tracks {
			if !tr.Device.Local && tr.State == "ready" && tr.Device.Downloaded {
				synced = &tr
				return true
			}
		}
		return false
	})
	if n := len(a.catalog().Tracks); n != 1 {
		t.Fatalf("the song shows %d times", n)
	}
	// It plays from the Songs folder now.
	var play struct {
		URL string `json:"url"`
	}
	a.must("POST", "/local/play", map[string]any{"track_id": synced.ID}, &play)
	res, err := http.Get(play.URL)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || n != synced.ByteSize {
		t.Fatalf("synced media: %d %d/%d", res.StatusCode, n, synced.ByteSize)
	}
	a.must("POST", "/local/play", map[string]any{"track_id": nil}, nil)

	var check map[string]any
	a.must("POST", "/local/check-folder", nil, &check)
	if check["caught_up"] != true {
		t.Fatalf("folder check: %+v", check)
	}
}
