package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"v0.2.0", "0.1.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.2.0-rc.1", "0.1.0", true},
		{"0.2.0+build.5", "0.2.0", false},
		{"garbage", "0.1.0", false},
		{"0.2", "0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestVerify(t *testing.T) {
	pub, priv := keys(t)
	b, err := Sign(Manifest{Version: "1.2.3"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Verify(b, pub)
	if err != nil || m.Version != "1.2.3" {
		t.Fatalf("Verify = %+v, %v", m, err)
	}
	other, _ := keys(t)
	if _, err := Verify(b, other); !errors.Is(err, ErrSignature) {
		t.Fatalf("another key: %v", err)
	}
	tampered := []byte(string(b[:len(b)/2]) + "9" + string(b[len(b)/2+1:]))
	if _, err := Verify(tampered, pub); err == nil {
		t.Fatal("a changed manifest verified")
	}
}

// release serves latest.json and one AppImage, like a GitHub Release.
type release struct {
	srv      *httptest.Server
	manifest []byte
	file     []byte
}

func newRelease(t *testing.T, priv ed25519.PrivateKey, version string, file []byte, mutate func(*Manifest)) *release {
	r := &release{file: file}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(r.manifest) })
	mux.HandleFunc("/Song-Sleeve-x86_64.AppImage", func(w http.ResponseWriter, _ *http.Request) { w.Write(r.file) })
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	sum := sha256.Sum256(file)
	m := Manifest{Version: version, PageURL: r.srv.URL + "/page", Assets: map[string]Asset{
		AssetAppImage: {Name: "Song-Sleeve-x86_64.AppImage", URL: r.srv.URL + "/Song-Sleeve-x86_64.AppImage", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(file))},
		AssetDeb:      {Name: "song-sleeve_amd64.deb", URL: r.srv.URL + "/song-sleeve_amd64.deb", SHA256: hex.EncodeToString(sum[:]), Size: 1},
	}}
	if mutate != nil {
		mutate(&m)
	}
	b, err := Sign(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	r.manifest = b
	return r
}

func appImage(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "Song-Sleeve-x86_64.AppImage")
	if err := os.WriteFile(p, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAppImageUpdates(t *testing.T) {
	pub, priv := keys(t)
	rel := newRelease(t, priv, "0.2.0", []byte("new build"), nil)
	path := appImage(t)
	var events []Status
	u := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: AppImage, Path: path, Emit: func(s Status) { events = append(events, s) }})

	st := u.Check(context.Background())
	if st.State != "ready" || st.Version != "0.2.0" || st.Download != rel.srv.URL+"/page" {
		t.Fatalf("status = %+v", st)
	}
	if b, _ := os.ReadFile(path); string(b) != "new build" {
		t.Fatalf("file = %q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm()&0o111 == 0 {
		t.Fatal("the new AppImage isn't executable")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".song-sleeve-update-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.State] = true
	}
	for _, s := range []string{"checking", "available", "downloading", "ready"} {
		if !seen[s] {
			t.Errorf("no %q event in %+v", s, events)
		}
	}
	// Ready waits for the restart; checking again changes nothing.
	if st := u.Check(context.Background()); st.State != "ready" {
		t.Fatalf("second check = %+v", st)
	}
}

func TestAppImageRejectsBadDownloads(t *testing.T) {
	pub, priv := keys(t)
	for name, mutate := range map[string]func(*Manifest){
		"checksum": func(m *Manifest) {
			a := m.Assets[AssetAppImage]
			a.SHA256 = hex.EncodeToString(make([]byte, 32))
			m.Assets[AssetAppImage] = a
		},
		"size": func(m *Manifest) {
			a := m.Assets[AssetAppImage]
			a.Size--
			m.Assets[AssetAppImage] = a
		},
	} {
		t.Run(name, func(t *testing.T) {
			rel := newRelease(t, priv, "0.2.0", []byte("new build"), mutate)
			path := appImage(t)
			u := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: AppImage, Path: path})
			st := u.Check(context.Background())
			if st.State != "available" || st.Error == "" || st.Download == "" {
				t.Fatalf("status = %+v", st)
			}
			if b, _ := os.ReadFile(path); string(b) != "old build" {
				t.Fatalf("the old AppImage changed: %q", b)
			}
			if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".song-sleeve-update-*")); len(left) != 0 {
				t.Fatalf("temp files left: %v", left)
			}
		})
	}
}

func TestAppImageInReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	pub, priv := keys(t)
	rel := newRelease(t, priv, "0.2.0", []byte("new build"), nil)
	path := appImage(t)
	os.Chmod(filepath.Dir(path), 0o555)
	t.Cleanup(func() { os.Chmod(filepath.Dir(path), 0o755) })
	st := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: AppImage, Path: path}).Check(context.Background())
	if st.State != "available" || st.Download != rel.srv.URL+"/page" || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDebOnlyNotifies(t *testing.T) {
	pub, priv := keys(t)
	rel := newRelease(t, priv, "0.2.0", []byte("new build"), nil)
	st := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: Deb, Path: "/usr/bin/song-sleeve"}).Check(context.Background())
	if st.State != "available" || st.Download != rel.srv.URL+"/song-sleeve_amd64.deb" || st.Progress != 0 {
		t.Fatalf("status = %+v", st)
	}
}

func TestNoUpdate(t *testing.T) {
	pub, priv := keys(t)
	path := appImage(t)
	for _, v := range []string{"0.1.0", "0.0.9"} {
		rel := newRelease(t, priv, v, []byte("new build"), nil)
		st := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: AppImage, Path: path}).Check(context.Background())
		if st.State != "current" {
			t.Fatalf("release %s: status = %+v", v, st)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "old build" {
		t.Fatal("an older or equal release replaced the file")
	}
}

func TestForgedManifest(t *testing.T) {
	pub, _ := keys(t)
	_, attacker := keys(t)
	rel := newRelease(t, attacker, "9.0.0", []byte("evil"), nil)
	path := appImage(t)
	st := New(Config{URL: rel.srv.URL + "/latest.json", PublicKey: pub, Current: "0.1.0", Kind: AppImage, Path: path}).Check(context.Background())
	if st.State != "error" {
		t.Fatalf("status = %+v", st)
	}
	if b, _ := os.ReadFile(path); string(b) != "old build" {
		t.Fatal("a forged release replaced the file")
	}
}

func TestDisabled(t *testing.T) {
	pub, _ := keys(t)
	for name, cfg := range map[string]Config{
		"dev build":  {URL: "http://x/latest.json", PublicKey: pub, Kind: Dev},
		"no channel": {PublicKey: pub, Kind: AppImage},
		"no key":     {URL: "http://x/latest.json", Kind: AppImage},
	} {
		u := New(cfg)
		if u.Enabled() || u.Check(context.Background()).State != "idle" {
			t.Errorf("%s: should be disabled", name)
		}
	}
}
