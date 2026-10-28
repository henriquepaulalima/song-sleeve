package shell

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tray works signed out: it still shows the window and quits.
func TestTraySignedOut(t *testing.T) {
	root := t.TempDir()
	var got []TrayState
	sh, err := New(Config{
		APIBase: "http://127.0.0.1:1/api", Version: "0.1.0", DataDir: filepath.Join(root, "data"), SleeveRoot: filepath.Join(root, "Song Sleeve"),
		FileCredentials: true, Hooks: Hooks{Tray: func(st TrayState) { got = append(got, st) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Shutdown()

	body := `{"labels":{"show":"Mostrar o Song Sleeve","quit":"Sair do Song Sleeve"},"playing":true,"has_track":true,"title":"Song · Artist"}`
	rec := httptest.NewRecorder()
	sh.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/local/tray", strings.NewReader(body)))
	if rec.Code != 204 {
		t.Fatalf("PUT /local/tray = %d %s", rec.Code, rec.Body)
	}
	if len(got) != 1 || got[0].Labels["show"] != "Mostrar o Song Sleeve" || !got[0].Playing || !got[0].HasTrack || got[0].Title != "Song · Artist" {
		t.Fatalf("tray hook got %+v", got)
	}

	rec = httptest.NewRecorder()
	sh.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/local/tray", strings.NewReader("{")))
	if rec.Code != 400 || len(got) != 1 {
		t.Fatalf("a bad body = %d, hook calls %d", rec.Code, len(got))
	}

	rec = httptest.NewRecorder()
	sh.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/local/state", nil))
	var st stateDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.OS != runtime.GOOS {
		t.Fatalf("state os = %q, %v", st.OS, err)
	}
}
