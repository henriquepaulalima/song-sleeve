package shell

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/update"
)

func updateShell(t *testing.T, u *update.Updater, opened *[]string, restarts *int) *Shell {
	root := t.TempDir()
	sh, err := New(Config{
		APIBase: "http://127.0.0.1:1/api", Version: "0.1.0", DataDir: filepath.Join(root, "data"), SleeveRoot: filepath.Join(root, "Song Sleeve"),
		FileCredentials: true, Updater: u,
		Hooks: Hooks{
			Open:    func(p string) error { *opened = append(*opened, p); return nil },
			Restart: func() error { *restarts++; return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sh.Shutdown)
	return sh
}

func do(sh *Shell, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	sh.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestUpdateRoutesDisabled(t *testing.T) {
	var opened []string
	var restarts int
	sh := updateShell(t, update.New(update.Config{Current: "0.1.0", Kind: update.Dev}), &opened, &restarts)

	var doc updateDoc
	rec := do(sh, "GET", "/local/update", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc.Enabled || doc.Current != "0.1.0" || doc.State != "idle" {
		t.Fatalf("GET /local/update = %s", rec.Body)
	}
	if rec := do(sh, "POST", "/local/update/check", ""); rec.Code != 409 {
		t.Fatalf("check on a dev build = %d", rec.Code)
	}
	if rec := do(sh, "POST", "/local/update/restart", ""); rec.Code != 409 || restarts != 0 {
		t.Fatalf("restart with nothing ready = %d, restarts %d", rec.Code, restarts)
	}
}

func TestUpdateCheckRuns(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit <- struct{}{}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	var opened []string
	var restarts int
	sh := updateShell(t, update.New(update.Config{URL: srv.URL, PublicKey: pub, Current: "0.1.0", Kind: update.Deb}), &opened, &restarts)
	if rec := do(sh, "POST", "/local/update/check", ""); rec.Code != 202 {
		t.Fatalf("check = %d", rec.Code)
	}
	<-hit
}

func TestOpenURL(t *testing.T) {
	var opened []string
	var restarts int
	sh := updateShell(t, nil, &opened, &restarts)
	if rec := do(sh, "POST", "/local/open-url", `{"url":"https://github.com/henriquepaulalima/song-sleeve/releases/latest"}`); rec.Code != 204 {
		t.Fatalf("https link = %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"/home/me"}`, `{"url":"javascript:alert(1)"}`, `{"url":"https://"}`} {
		if rec := do(sh, "POST", "/local/open-url", bad); rec.Code != 400 {
			t.Errorf("%s = %d", bad, rec.Code)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("opened %v", opened)
	}
	// No updater at all (tests, other shells): the card still reads.
	if rec := do(sh, "GET", "/local/update", ""); rec.Code != 200 {
		t.Fatalf("GET /local/update without an updater = %d", rec.Code)
	}
}
