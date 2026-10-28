// Package update keeps the desktop app current (§9.4, D-28). Each GitHub Release
// carries latest.json: a manifest of the build's assets, signed with the release
// key (ed25519), whose public half is built into the app. The AppImage downloads
// the new file, checks it against the signed manifest, swaps itself and restarts.
// The .deb belongs to the package manager, so it only says a new version exists
// and links to the download. Development builds don't update.
package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind is how this copy of the app was installed.
type Kind string

const (
	AppImage Kind = "appimage"
	Deb      Kind = "deb"
	Dev      Kind = "" // run from a build folder: no updates
)

// Asset keys in the manifest.
const (
	AssetAppImage = "linux-appimage"
	AssetDeb      = "linux-deb"
)

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	Version   string           `json:"version"`
	Published time.Time        `json:"published"`
	PageURL   string           `json:"page_url"` // the release page, for manual downloads
	Assets    map[string]Asset `json:"assets"`
}

// Signed is latest.json: the manifest's exact bytes and their signature, so
// nothing is re-serialized before checking.
type Signed struct {
	Manifest  string `json:"manifest"`
	Signature string `json:"signature"` // base64 ed25519 over Manifest
}

// Sign wraps a manifest for publishing (cmd/release).
func Sign(m Manifest, key ed25519.PrivateKey) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(Signed{Manifest: string(b), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))}, "", "  ")
}

var ErrSignature = errors.New("update manifest signature is invalid")

// Verify checks latest.json and returns its manifest.
func Verify(data []byte, pub ed25519.PublicKey) (Manifest, error) {
	var s Signed
	var m Manifest
	if err := json.Unmarshal(data, &s); err != nil {
		return m, fmt.Errorf("update manifest: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(s.Manifest), sig) {
		return m, ErrSignature
	}
	if err := json.Unmarshal([]byte(s.Manifest), &m); err != nil {
		return m, fmt.Errorf("update manifest: %w", err)
	}
	return m, nil
}

// Newer reports whether version a is newer than b (MAJOR.MINOR.PATCH; anything
// after "-" or "+" is ignored).
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// Detect finds how the running app was installed. The AppImage runtime sets
// $APPIMAGE to the file it runs from; the .deb installs under /usr.
func Detect() (Kind, string) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return AppImage, p
	}
	exe, err := os.Executable()
	if err != nil {
		return Dev, ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if strings.HasPrefix(exe, "/usr/") || strings.HasPrefix(exe, "/opt/") {
		return Deb, exe
	}
	return Dev, exe
}

// Status is what Settings shows; it is also emitted as the "update" event.
type Status struct {
	Kind     Kind    `json:"kind"`
	Current  string  `json:"current"`
	State    string  `json:"state"` // idle, checking, current, available, downloading, ready, error
	Version  string  `json:"version,omitempty"`
	Progress float64 `json:"progress,omitempty"` // 0..1 while downloading
	// Download is where to get the new version by hand: the .deb, or the release
	// page when the AppImage can't replace itself.
	Download string    `json:"download,omitempty"`
	Error    string    `json:"error,omitempty"`
	Checked  time.Time `json:"checked,omitzero"`
}

type Config struct {
	URL       string // latest.json; empty disables updates
	PublicKey ed25519.PublicKey
	Current   string
	Kind      Kind
	Path      string // the AppImage file, for AppImage installs
	Emit      func(Status)
	HTTP      *http.Client
}

type Updater struct {
	cfg  Config
	mu   sync.Mutex
	st   Status
	busy bool
}

func New(cfg Config) *Updater {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Minute}
	}
	if cfg.Emit == nil {
		cfg.Emit = func(Status) {}
	}
	return &Updater{cfg: cfg, st: Status{Kind: cfg.Kind, Current: cfg.Current, State: "idle"}}
}

// Enabled is false for development builds and builds without a release channel.
func (u *Updater) Enabled() bool {
	return u.cfg.Kind != Dev && u.cfg.URL != "" && len(u.cfg.PublicKey) == ed25519.PublicKeySize
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *Updater) set(f func(*Status)) {
	u.mu.Lock()
	f(&u.st)
	st := u.st
	u.mu.Unlock()
	u.cfg.Emit(st)
}

// Run checks shortly after start and then every 6 hours.
func (u *Updater) Run(ctx context.Context) {
	if !u.Enabled() {
		return
	}
	t := time.NewTimer(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.Check(ctx)
			t.Reset(6 * time.Hour)
		}
	}
}

// Check looks for a newer release and, for the AppImage, downloads it. Calls
// while one is running return the current status.
func (u *Updater) Check(ctx context.Context) Status {
	if !u.Enabled() {
		return u.Status()
	}
	u.mu.Lock()
	if u.busy || u.st.State == "ready" {
		st := u.st
		u.mu.Unlock()
		return st
	}
	u.busy = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
	}()

	u.set(func(s *Status) { s.State, s.Error, s.Progress = "checking", "", 0 })
	m, err := u.fetch(ctx)
	if err != nil {
		u.set(func(s *Status) { s.State, s.Error, s.Checked = "error", err.Error(), time.Now() })
		return u.Status()
	}
	if !Newer(m.Version, u.cfg.Current) {
		u.set(func(s *Status) { s.State, s.Version, s.Download, s.Checked = "current", "", "", time.Now() })
		return u.Status()
	}
	key := AssetDeb
	if u.cfg.Kind == AppImage {
		key = AssetAppImage
	}
	a, ok := m.Assets[key]
	if !ok {
		// This release doesn't ship this package: nothing to offer.
		u.set(func(s *Status) { s.State, s.Checked = "current", time.Now() })
		return u.Status()
	}
	download := a.URL
	if u.cfg.Kind == AppImage {
		download = m.PageURL
	}
	u.set(func(s *Status) {
		s.State, s.Version, s.Download, s.Checked = "available", m.Version, download, time.Now()
	})
	if u.cfg.Kind != AppImage {
		return u.Status()
	}
	if err := u.install(ctx, a); err != nil {
		// Still "available": the release page link lets the user do it by hand.
		u.set(func(s *Status) { s.State, s.Error, s.Progress = "available", err.Error(), 0 })
		return u.Status()
	}
	u.set(func(s *Status) { s.State, s.Progress = "ready", 1 })
	return u.Status()
}

func (u *Updater) fetch(ctx context.Context) (Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.cfg.URL, nil)
	if err != nil {
		return Manifest{}, err
	}
	res, err := u.cfg.HTTP.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Manifest{}, fmt.Errorf("update manifest: HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Manifest{}, err
	}
	return Verify(b, u.cfg.PublicKey)
}

// install downloads the new AppImage next to the running one, checks its size
// and SHA-256 against the signed manifest, and renames it over the old file. The
// running copy keeps its mounted image, so nothing changes until a restart.
func (u *Updater) install(ctx context.Context, a Asset) error {
	want, err := hex.DecodeString(a.SHA256)
	if err != nil || len(want) != sha256.Size || a.Size <= 0 {
		return errors.New("update manifest: bad asset checksum")
	}
	dir := filepath.Dir(u.cfg.Path)
	tmp, err := os.CreateTemp(dir, ".song-sleeve-update-*")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", u.cfg.Path, err)
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	defer tmp.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	res, err := u.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("update download: HTTP %d", res.StatusCode)
	}
	h := sha256.New()
	pw := &progress{total: a.Size, report: func(p float64) { u.set(func(s *Status) { s.State, s.Progress = "downloading", p }) }}
	n, err := io.Copy(io.MultiWriter(tmp, h, pw), io.LimitReader(res.Body, a.Size+1))
	if err != nil {
		return err
	}
	if n != a.Size || !bytes.Equal(h.Sum(nil), want) {
		return errors.New("the downloaded update doesn't match the release")
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(u.cfg.Path); err == nil {
		mode = fi.Mode().Perm() | 0o111
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), u.cfg.Path)
}

// progress reports at most every whole percent.
type progress struct {
	total, done int64
	last        int
	report      func(float64)
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if pct := int(p.done * 100 / p.total); pct != p.last {
		p.last = pct
		p.report(min(float64(p.done)/float64(p.total), 1))
	}
	return len(b), nil
}
