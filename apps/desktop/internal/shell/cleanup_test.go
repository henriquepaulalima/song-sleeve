package shell

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// A purge interrupted by a quit resumes at the next start (§7.11): the account
// isn't started again, and its namespace goes.
func TestInterruptedPurgeResumes(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	const id, email = "01a00000-0000-7000-8000-000000000001", "gone@example.com"
	folder := filepath.Join(root, "Song Sleeve", songsdir.AccountFolderName(email))
	for _, d := range []string{folder, filepath.Join(data, "accounts", id)} {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "leftover"), []byte("x"), 0o644)
	}
	st, _ := json.Marshal(state{InstallationID: "inst", AccountID: id, Known: map[string]string{id: email}, Purges: map[string]string{id: email}})
	os.WriteFile(filepath.Join(data, "state.json"), st, 0o600)

	sh, err := New(Config{APIBase: "http://127.0.0.1:1/api", Version: "0.1.0", DataDir: data, SleeveRoot: filepath.Join(root, "Song Sleeve"), FileCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Shutdown()
	sh.creds.Set(id, "old-token")
	sh.Restore(t.Context())
	if sh.current() != nil {
		t.Fatal("an account being purged was started again")
	}
	end := time.Now().Add(5 * time.Second)
	for {
		sh.mu.Lock()
		_, pending := sh.st.Purges[id]
		sh.mu.Unlock()
		_, e1 := os.Stat(folder)
		_, e2 := os.Stat(filepath.Join(data, "accounts", id))
		tok, _ := sh.creds.Get(id)
		if !pending && os.IsNotExist(e1) && os.IsNotExist(e2) && tok == "" {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("purge didn't resume: pending=%v folder=%v data=%v token=%q", pending, e1, e2, tok)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Send crash reports (D-35): on by default, saved across restarts, and shown to
// the page with this build's DSN.
func TestCrashReportsSetting(t *testing.T) {
	root := t.TempDir()
	open := func() *Shell {
		sh, err := New(Config{APIBase: "http://127.0.0.1:1/api", Version: "0.1.0", DataDir: filepath.Join(root, "data"), SleeveRoot: filepath.Join(root, "Song Sleeve"),
			FileCredentials: true, SentryDSN: "https://key@example.invalid/1", Environment: "test"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sh.Shutdown)
		return sh
	}
	state := func(sh *Shell) crashDoc {
		var st stateDoc
		json.Unmarshal(do(sh, "GET", "/local/state", "").Body.Bytes(), &st)
		return st.Crash
	}
	sh := open()
	if c := state(sh); !c.Enabled || c.DSN != "https://key@example.invalid/1" || c.Release != "0.1.0" || c.Environment != "test" {
		t.Fatalf("defaults: %+v", c)
	}
	if rec := do(sh, "PUT", "/local/settings", `{"crash_reports":false}`); rec.Code != 204 {
		t.Fatalf("PUT /local/settings = %d", rec.Code)
	}
	if rec := do(sh, "PUT", "/local/settings", `{}`); rec.Code != 400 {
		t.Fatalf("an empty change = %d", rec.Code)
	}
	sh.Shutdown()
	if again := open(); again.CrashReports() || state(again).Enabled {
		t.Fatal("turning crash reports off didn't survive a restart")
	}
}

// Deletion status and ack carry only the cleanup credential, never the signed-in
// account's session token (they may be about another account on this device).
func TestCleanupCallsCarryNoSessionToken(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.URL.Path+" "+r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, "/status") {
			w.Write([]byte(`{"deleted":true}`))
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	root := t.TempDir()
	sh, err := New(Config{APIBase: srv.URL + "/api", Version: "0.1.0", DataDir: filepath.Join(root, "data"), SleeveRoot: filepath.Join(root, "Song Sleeve"), FileCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Shutdown()
	sh.client.SetToken("session-token-of-another-account")
	const id = "01a00000-0000-7000-8000-000000000002"
	sh.creds.Set(cleanupKey(id), "cleanup-cred")
	sh.mu.Lock()
	sh.st.Known = map[string]string{id: "other@example.com"}
	sh.mu.Unlock()
	sh.sweepDeletions(t.Context())
	if len(auths) != 2 {
		t.Fatalf("calls: %v", auths)
	}
	for _, a := range auths {
		if strings.Contains(a, "session-token") {
			t.Fatalf("a cleanup call carried a session token: %q", a)
		}
	}
}
