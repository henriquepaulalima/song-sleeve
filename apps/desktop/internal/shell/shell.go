// Package shell is the desktop app's native side behind the webview: the signed-in
// account's services (SQLite, the Song Sleeve folder, the Device Sync Service), the
// listener that keeps running while the window is hidden (§9.4), and the HTTP
// surface the React UI talks to. It has no Wails dependency, so it is tested with
// plain httptest.
//
// The UI keeps calling the server API at /api as on the web. The shell forwards
// those calls with the bearer token from the OS credential store, so the token never
// reaches the webview, and answers the ones that touch local songs itself.
package shell

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/accountdb"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/creds"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/device"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// Hooks are the platform pieces the Wails layer provides.
type Hooks struct {
	Emit      func(name string, data any)
	PickFiles func(folder bool, title string) ([]string, error)
	Confirm   func(title, message string) bool
	Open      func(path string) error // shows a file or folder in the file manager
}

type Config struct {
	APIBase     string // server API root, e.g. http://localhost:8088/api
	Version     string
	DataDir     string // app-private data: databases, staging, state
	SleeveRoot  string // the user-visible Song Sleeve folder (~/Song Sleeve)
	DeviceLabel string
	Hooks       Hooks
	// FileCredentials keeps the token in a private file instead of the OS
	// credential store (tests; SONG_SLEEVE_FILE_CREDENTIALS=1).
	FileCredentials bool
}

// state is the small file that remembers who was signed in.
type state struct {
	InstallationID string            `json:"installation_id"`
	AccountID      string            `json:"account_id,omitempty"`
	Known          map[string]string `json:"known_accounts,omitempty"` // account ID → email, for folder checks
}

type Shell struct {
	cfg    Config
	client *api.Client
	creds  *creds.Store
	media  *mediaServer

	mu   sync.Mutex
	st   state
	acct *account

	online atomic.Bool
}

type account struct {
	id     string
	dir    string
	db     *accountdb.DB
	lib    *library.Library
	songs  *songsdir.Dir
	dev    *device.Device
	cancel context.CancelFunc
	done   sync.WaitGroup
	kick   chan struct{}

	meMu sync.RWMutex
	me   api.Me

	playingMu sync.Mutex
	playing   string // pinned track (R-PIN-1)
}

func (a *account) Me() *api.Me {
	a.meMu.RLock()
	defer a.meMu.RUnlock()
	m := a.me
	return &m
}

func (a *account) setMe(m api.Me) {
	a.meMu.Lock()
	a.me = m
	a.meMu.Unlock()
}

func (a *account) reconcileSoon() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func New(cfg Config) (*Shell, error) {
	if cfg.Hooks.Emit == nil {
		cfg.Hooks.Emit = func(string, any) {}
	}
	if cfg.Hooks.Confirm == nil {
		cfg.Hooks.Confirm = func(string, string) bool { return false }
	}
	if cfg.DeviceLabel == "" {
		host, _ := os.Hostname()
		cfg.DeviceLabel = host
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	store := creds.New(cfg.DataDir)
	if cfg.FileCredentials {
		store = creds.NewFile(cfg.DataDir)
	}
	s := &Shell{cfg: cfg, client: api.New(cfg.APIBase, "desktop", cfg.Version), creds: store}
	if err := s.loadState(); err != nil {
		return nil, err
	}
	m, err := startMedia(s)
	if err != nil {
		return nil, err
	}
	s.media = m
	return s, nil
}

func (s *Shell) statePath() string { return filepath.Join(s.cfg.DataDir, "state.json") }

func (s *Shell) loadState() error {
	b, err := os.ReadFile(s.statePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return err
		}
	}
	if s.st.InstallationID == "" {
		s.st.InstallationID = uuid.NewString()
		return s.saveState()
	}
	return nil
}

func (s *Shell) saveState() error {
	b, _ := json.MarshalIndent(s.st, "", "  ")
	tmp := s.statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath())
}

func (s *Shell) emit(name string, data any) { s.cfg.Hooks.Emit(name, data) }

// Restore signs the remembered account back in on startup. It works offline: the
// account's last known profile and its replica are local (R-PLAY-1).
func (s *Shell) Restore(ctx context.Context) {
	s.mu.Lock()
	id := s.st.AccountID
	s.mu.Unlock()
	if id == "" {
		return
	}
	token, err := s.creds.Get(id)
	if err != nil || token == "" {
		slog.Warn("no stored credentials", "err", err)
		return
	}
	var me api.Me
	b, err := os.ReadFile(filepath.Join(s.accountDir(id), "me.json"))
	if err != nil || json.Unmarshal(b, &me) != nil {
		// No cached profile yet: ask the server.
		s.client.SetToken(token)
		m, err := s.client.Me(ctx)
		if err != nil {
			s.client.SetToken("")
			return
		}
		me = *m
	}
	s.client.SetToken(token)
	if err := s.startAccount(me); err != nil {
		slog.Error("start account", "err", err)
	}
}

func (s *Shell) accountDir(id string) string {
	return filepath.Join(s.cfg.DataDir, "accounts", filepath.Base(id))
}

// signedIn stores the token and starts the account's services after a login or a
// registration through the proxy.
func (s *Shell) signedIn(me api.Me, token string) error {
	if err := s.creds.Set(me.ID, token); err != nil {
		return err
	}
	s.client.SetToken(token)
	s.mu.Lock()
	cur := s.acct
	s.mu.Unlock()
	if cur != nil && cur.id == me.ID {
		cur.setMe(me)
		s.saveMe(cur)
		return nil
	}
	if cur != nil {
		s.stopAccount(false)
	}
	return s.startAccount(me)
}

func (s *Shell) startAccount(me api.Me) error {
	dir := s.accountDir(me.ID)
	db, err := accountdb.Open(dir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.st.Known == nil {
		s.st.Known = map[string]string{}
	}
	s.st.Known[me.ID] = me.Email
	s.st.AccountID = me.ID
	var others []string
	for id, email := range s.st.Known {
		if id != me.ID {
			others = append(others, songsdir.AccountFolderName(email))
		}
	}
	_ = s.saveState()
	s.mu.Unlock()

	songs, err := songsdir.Open(s.cfg.SleeveRoot, songsdir.AccountFolderName(me.Email), me.ID, filepath.Join(dir, "tmp"))
	if err != nil {
		db.Close()
		return err
	}
	lib := library.New(db, s.cfg.SleeveRoot)
	ctx, cancel := context.WithCancel(context.Background())
	a := &account{id: me.ID, dir: dir, db: db, lib: lib, songs: songs, cancel: cancel, kick: make(chan struct{}, 1), me: me}
	a.dev = device.New(device.Config{
		DB: db, Library: lib, Dir: songs, API: s.client, Me: a.Me,
		Listener: &listener{s: s, a: a}, TempDir: filepath.Join(dir, "tmp"), OtherAccountFolders: others,
	})
	s.saveMe(a)
	s.mu.Lock()
	s.acct = a
	s.mu.Unlock()
	a.dev.Start(ctx)
	a.done.Add(2)
	go s.reconciler(ctx, a)
	go s.listen(ctx, a)
	go func() {
		// Local health on open (§7.6): sources moved or changed since last time.
		if changed, err := lib.CheckHealth(ctx, false); err == nil && len(changed) > 0 {
			s.emit("catalog", nil)
		}
	}()
	s.emit("account", nil)
	return nil
}

func (s *Shell) saveMe(a *account) {
	b, _ := json.Marshal(a.Me())
	_ = os.WriteFile(filepath.Join(a.dir, "me.json"), b, 0o600)
}

// stopAccount stops the account's services. remove also purges its namespace on
// this device (Log out and remove, account deletion; §7.12).
func (s *Shell) stopAccount(remove bool) error {
	s.mu.Lock()
	a := s.acct
	s.acct = nil
	s.mu.Unlock()
	if a == nil {
		return nil
	}
	a.cancel()
	a.done.Wait()
	var err error
	if remove {
		err = a.dev.PurgeLocal()
		_ = os.RemoveAll(a.dir)
		s.mu.Lock()
		delete(s.st.Known, a.id)
		s.mu.Unlock()
	} else {
		a.dev.Stop()
		a.db.Close()
	}
	return err
}

// SignOut ends the session on this device. The server session is revoked when
// possible; local data stays unless remove is set (§7.12).
func (s *Shell) SignOut(ctx context.Context, remove bool) error {
	s.mu.Lock()
	a := s.acct
	s.mu.Unlock()
	if a != nil && s.client.Token() != "" {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = s.client.Do(rctx, "POST", "/auth/logout", nil, nil)
		cancel()
	}
	return s.dropSession(remove)
}

// dropSession forgets the token and stops the account without calling the server
// (it already ended the session: 401, revoke-all, account deleted).
func (s *Shell) dropSession(remove bool) error {
	s.mu.Lock()
	id := s.st.AccountID
	s.st.AccountID = ""
	_ = s.saveState()
	s.mu.Unlock()
	s.client.SetToken("")
	if id != "" {
		_ = s.creds.Delete(id)
	}
	err := s.stopAccount(remove)
	s.mu.Lock()
	_ = s.saveState()
	s.mu.Unlock()
	s.emit("account", nil)
	return err
}

func (s *Shell) current() *account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acct
}

// Shutdown stops everything when the app quits; running uploads resume later.
func (s *Shell) Shutdown() {
	_ = s.stopAccount(false)
	s.media.close()
}

func (s *Shell) setOnline(v bool) {
	if s.online.Swap(v) != v {
		s.emit("connection", map[string]bool{"online": v})
	}
}

// reconciler runs one reconciliation at a time and coalesces hints (§7.9).
func (s *Shell) reconciler(ctx context.Context, a *account) {
	defer a.done.Done()
	tick := time.NewTicker(5 * time.Minute) // catches up after sleep even without hints
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.kick:
		case <-tick.C:
		}
		err := a.dev.Reconcile(ctx)
		switch {
		case err == nil:
			s.setOnline(true)
		case ctx.Err() != nil:
			return
		case errors.Is(err, api.ErrConnection):
			s.setOnline(false)
		case api.Code(err) == "unauthenticated":
			go s.dropSession(false)
			return
		case api.Code(err) == "account_deleted":
			go s.dropSession(true)
			return
		default:
			slog.Warn("reconcile", "err", err)
		}
	}
}

// listen keeps the event stream open while the app runs, even with the window
// hidden. Each connect starts with a catch-up; events are only hints (R-EVT-1).
func (s *Shell) listen(ctx context.Context, a *account) {
	defer a.done.Done()
	backoff := time.Second
	for ctx.Err() == nil {
		a.reconcileSoon()
		cursor, _ := a.db.Cursor(ctx)
		start := time.Now()
		err := s.client.Events(ctx, cursor, func(ev api.Event) {
			s.setOnline(true)
			switch ev.Name {
			case "progress":
				var p struct {
					TrackID   string `json:"track_id"`
					Confirmed int64  `json:"confirmed_bytes"`
					Declared  int64  `json:"declared_bytes"`
				}
				if json.Unmarshal(ev.Data, &p) == nil {
					s.emit("progress", map[string]any{"id": p.TrackID, "confirmed": p.Confirmed, "total": p.Declared})
				}
			case "account_deleted":
				go s.dropSession(true)
			case "changes":
				a.reconcileSoon()
			}
		})
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, api.ErrConnection) {
			s.setOnline(false)
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// refreshMe reloads the account profile (locale, cloud sync, terms).
func (s *Shell) refreshMe(ctx context.Context) {
	a := s.current()
	if a == nil {
		return
	}
	m, err := s.client.Me(ctx)
	if err != nil {
		return
	}
	a.setMe(*m)
	s.saveMe(a)
	s.emit("account", nil)
}

type listener struct {
	s *Shell
	a *account
}

func (l *listener) CatalogChanged(res *accountdb.ApplyResult) {
	l.s.emit("catalog", nil)
	for _, f := range res.SyncFailures {
		l.s.emit("syncFailed", f)
	}
	if res.AccountChanged {
		go l.s.refreshMe(context.Background())
	}
}
func (l *listener) LocalSongsChanged() { l.s.emit("catalog", nil) }
func (l *listener) FolderChanged()     { l.s.emit("folder", nil) }
func (l *listener) SyncProgress(id string, confirmed, total int64) {
	// A syncing song shows as its server track; progress goes to that row.
	target := localID(id)
	if song, err := l.a.lib.Song(context.Background(), id); err == nil && song.SyncingTrackID != nil {
		target = *song.SyncingTrackID
	}
	l.s.emit("progress", map[string]any{"id": target, "confirmed": confirmed, "total": total})
}
func (l *listener) LargeDeletion(n int) bool {
	return l.s.cfg.Hooks.Confirm("Delete them permanently?",
		formatLarge(n))
}
