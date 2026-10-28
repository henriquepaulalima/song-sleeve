// Package device is the Device Sync Service for one signed-in account on an
// installed app (R-DEV-1): one reconciliation coordinator that startup, resume,
// reconnect and event hints all call (§7.9), the Songs-folder downloads and pins
// (R-PIN), and the restart-safe sync queue with three workers (R-CLOUD-9, §7.3).
package device

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/accountdb"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/library"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// Listener receives what changed, for the UI and the player.
type Listener interface {
	CatalogChanged(res *accountdb.ApplyResult)
	LocalSongsChanged()
	FolderChanged()
	SyncProgress(localSongID string, confirmed, total int64)
	LargeDeletion(n int) bool // the "Delete them permanently?" prompt (§7.13)
}

type nopListener struct{}

func (nopListener) CatalogChanged(*accountdb.ApplyResult) {}
func (nopListener) LocalSongsChanged()                    {}
func (nopListener) FolderChanged()                        {}
func (nopListener) SyncProgress(string, int64, int64)     {}
func (nopListener) LargeDeletion(int) bool                { return false }

type Config struct {
	DB                  *accountdb.DB
	Library             *library.Library
	Dir                 *songsdir.Dir
	API                 *api.Client
	Me                  func() *api.Me // the signed-in account (cloud sync flag, device ID)
	Listener            Listener
	Workers             int // sync workers (R-CLOUD-9), 3 by default
	TempDir             string
	OtherAccountFolders []string
	// SyncDelay holds each sync just before its upload, after the track document
	// exists. Development only (SONG_SLEEVE_DEV_SYNC_DELAY in dev builds), to try
	// what happens mid-sync on a fast local network.
	SyncDelay time.Duration
}

type Device struct {
	cfg Config
	db  *accountdb.DB
	lib *library.Library
	dir *songsdir.Dir
	api *api.Client
	lis Listener

	reconcileMu sync.Mutex // one reconciliation at a time; file work is serialized
	pinsMu      sync.Mutex
	pins        map[string]bool // track IDs whose playing file is pinned (R-PIN-1)

	syncKick  chan struct{}
	stopOnce  sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup
	runningMu sync.Mutex
	running   map[string]context.CancelFunc // local song ID → running sync
}

func New(cfg Config) *Device {
	if cfg.Listener == nil {
		cfg.Listener = nopListener{}
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 3
	}
	return &Device{
		cfg: cfg, db: cfg.DB, lib: cfg.Library, dir: cfg.Dir, api: cfg.API, lis: cfg.Listener,
		pins: map[string]bool{}, syncKick: make(chan struct{}, 1), stop: make(chan struct{}), running: map[string]context.CancelFunc{},
	}
}

// Start launches the sync workers. Queued syncs survive restarts (R-CLOUD-9).
func (d *Device) Start(ctx context.Context) {
	// A sync interrupted by a crash or a quit goes back to the queue.
	_, _ = d.db.ExecContext(ctx, `UPDATE pending_work SET state = 'queued' WHERE kind = 'sync' AND state = 'running'`)
	for i := 0; i < d.cfg.Workers; i++ {
		d.wg.Add(1)
		go d.worker(ctx)
	}
	d.kick()
}

// Stop stops the workers; running uploads stop and resume later.
func (d *Device) Stop() {
	d.stopOnce.Do(func() { close(d.stop) })
	d.runningMu.Lock()
	for _, cancel := range d.running {
		cancel()
	}
	d.runningMu.Unlock()
	d.wg.Wait()
}

func (d *Device) kick() {
	select {
	case d.syncKick <- struct{}{}:
	default:
	}
}

func (d *Device) cloudSync() bool {
	me := d.cfg.Me()
	return me != nil && me.CloudSyncEnabled
}

// Pins ----------------------------------------------------------------------------

// Pin holds the playing track's file: accepted changes to it wait (R-PIN-1).
func (d *Device) Pin(trackID string) {
	d.pinsMu.Lock()
	d.pins[trackID] = true
	d.pinsMu.Unlock()
}

// Unpin releases the pin at a playback boundary and applies what waited (R-PIN-2,3).
func (d *Device) Unpin(ctx context.Context, trackID string) error {
	d.pinsMu.Lock()
	delete(d.pins, trackID)
	d.pinsMu.Unlock()
	return d.Reconcile(ctx)
}

func (d *Device) pinned(trackID string) bool {
	d.pinsMu.Lock()
	defer d.pinsMu.Unlock()
	return d.pins[trackID]
}

// syncedFile is a synced_files row.
type syncedFile struct {
	TrackID   string
	ContentID sql.NullString
	FileName  sql.NullString
	Readiness string
}

func (d *Device) syncedFiles(ctx context.Context) (map[string]syncedFile, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT track_id, content_id, file_name, readiness FROM synced_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]syncedFile{}
	for rows.Next() {
		var f syncedFile
		if err := rows.Scan(&f.TrackID, &f.ContentID, &f.FileName, &f.Readiness); err != nil {
			return nil, err
		}
		out[f.TrackID] = f
	}
	return out, rows.Err()
}

// PlayablePath returns the file a synced track plays from: its Songs mirror
// (R-PLAY-1), or "" if it needs a download.
func (d *Device) PlayablePath(ctx context.Context, trackID string) (string, error) {
	var name sql.NullString
	var readiness string
	err := d.db.QueryRowContext(ctx, `SELECT file_name, readiness FROM synced_files WHERE track_id = ?`, trackID).Scan(&name, &readiness)
	if errors.Is(err, sql.ErrNoRows) || !name.Valid || (readiness != "ready" && readiness != "pending_apply" && readiness != "cleanup_pending") {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return d.dir.Path(name.String), nil
}

// Reconciliation (§7.9) ------------------------------------------------------------

// ReconcileReport summarizes one pass.
type ReconcileReport struct {
	CaughtUp   bool
	Downloaded int
	Removed    int
	Check      *songsdir.Report
}

// Reconcile is the single coordinator for startup, resume, reconnect, event hints
// and pin releases. It never blocks the UI: callers run it in the background.
func (d *Device) Reconcile(ctx context.Context) error {
	_, err := d.ReconcileReport(ctx, songsdir.Quick)
	return err
}

func (d *Device) ReconcileReport(ctx context.Context, mode songsdir.Mode) (*ReconcileReport, error) {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	rep := &ReconcileReport{}

	// Step 2: catch up on the catalog (snapshot when the cursor expired or on first run).
	caughtUp, err := d.catchUp(ctx)
	if err != nil && !errors.Is(err, api.ErrConnection) && api.Code(err) != "unauthenticated" {
		return rep, err
	}
	rep.CaughtUp = caughtUp

	// Step 3: remove before downloading.
	removed, err := d.removeRetired(ctx)
	if err != nil {
		return rep, err
	}
	rep.Removed = removed
	// Staged replacements whose pin was released are installed now (R-PIN-3).
	d.applyStaged(ctx)

	// Step 4: folder check (quick on every open), then downloads.
	check, err := d.checkFolder(ctx, mode, caughtUp)
	if err != nil {
		return rep, err
	}
	rep.Check = check
	if caughtUp {
		rep.Downloaded = d.downloadMissing(ctx, check.NeedsDownload)
	} else {
		d.markNeedsDownload(ctx, check.NeedsDownload, "offline")
	}

	// Step 5: local song health (quick stat check on every open).
	if changed, err := d.lib.CheckHealth(ctx, false); err == nil && len(changed) > 0 {
		d.lis.LocalSongsChanged()
	}

	// Step 6: auto-sync local songs in enabled scopes.
	if caughtUp && d.cloudSync() {
		if n, err := d.autoSync(ctx); err == nil && n > 0 {
			d.lis.LocalSongsChanged()
		}
	}
	return rep, nil
}

func (d *Device) catchUp(ctx context.Context) (bool, error) {
	var have bool
	var cursor int64
	have, err := d.db.GetMeta(ctx, "cursor", &cursor)
	if err != nil {
		return false, err
	}
	if !have {
		return d.snapshot(ctx)
	}
	for {
		ch, err := d.api.Changes(ctx, cursor)
		if api.Code(err) == "cursor_expired" {
			return d.snapshot(ctx)
		}
		if err != nil {
			return false, err
		}
		res, err := d.db.ApplyChanges(ctx, ch)
		if err != nil {
			return false, err
		}
		d.afterApply(ctx, res)
		cursor = ch.Cursor
		if !ch.HasMore {
			return true, nil
		}
	}
}

func (d *Device) snapshot(ctx context.Context) (bool, error) {
	pages, err := d.api.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	res, err := d.db.ApplySnapshot(ctx, pages)
	if err != nil {
		return false, err
	}
	d.afterApply(ctx, res)
	return true, nil
}

// afterApply finishes this device's syncs that published or failed (§7.3 steps 5–6).
func (d *Device) afterApply(ctx context.Context, res *accountdb.ApplyResult) {
	for _, e := range res.SyncFailures {
		d.syncFailed(ctx, e.EntityID, fmt.Sprint(e.Payload["reason"]))
	}
	for _, c := range res.Tracks {
		if c.After != nil && c.After.State == "ready" && c.After.CurrentContentID != nil {
			d.syncPublished(ctx, c.After)
		}
	}
	if len(res.Tracks) > 0 || len(res.DeletedPlaylists) > 0 || res.AccountChanged {
		d.lis.CatalogChanged(res)
	}
}

// removeRetired deletes mirrors of deleted tracks and retired content, unless the
// file is pinned: then it waits for the pin's release (R-PIN-3, R-DEV-3).
func (d *Device) removeRetired(ctx context.Context) (int, error) {
	files, err := d.syncedFiles(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for id, f := range files {
		t, err := d.db.Track(ctx, id)
		if err != nil {
			return removed, err
		}
		current := t != nil && t.CurrentContentID != nil && f.ContentID.Valid && *t.CurrentContentID == f.ContentID.String
		if current {
			continue
		}
		if d.pinned(id) {
			_, _ = d.db.ExecContext(ctx, `UPDATE synced_files SET readiness = 'cleanup_pending' WHERE track_id = ?`, id)
			continue
		}
		if f.FileName.Valid && f.ContentID.Valid {
			if err := d.dir.Remove(f.ContentID.String, f.FileName.String); err != nil {
				return removed, err
			}
			removed++
		}
		if _, err := d.db.ExecContext(ctx, `DELETE FROM synced_files WHERE track_id = ?`, id); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// expected builds the set of files that belong in the folder: every ready track's
// current content, plus this device's pending sync copies (§7.13 step 2).
func (d *Device) expected(ctx context.Context) ([]songsdir.Expected, map[string]bool, map[string]bool, error) {
	tracks, err := d.db.Tracks(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	files, err := d.syncedFiles(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	var out []songsdir.Expected
	pinnedNames := map[string]bool{}
	for _, t := range tracks {
		if t.State != "ready" || t.CurrentContentID == nil {
			continue
		}
		name := songsdir.FileName(*t.CurrentContentID, t.Format)
		if f, ok := files[t.ID]; ok && f.FileName.Valid && d.pinned(t.ID) {
			// The playing file stays as it is until released.
			pinnedNames[f.FileName.String] = true
		}
		out = append(out, songsdir.Expected{ContentID: *t.CurrentContentID, FileName: name, Size: t.ByteSize, SHA256: t.SHA256})
	}
	for _, f := range files {
		if f.Readiness == "cleanup_pending" && f.FileName.Valid {
			pinnedNames[f.FileName.String] = true
		}
	}
	inProgress := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, `SELECT json_extract(payload, '$.copy_name') FROM pending_work WHERE kind = 'sync' AND json_extract(payload, '$.copy_name') IS NOT NULL`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			inProgress[n] = true
		}
	}
	return out, pinnedNames, inProgress, rows.Err()
}

func (d *Device) checkFolder(ctx context.Context, mode songsdir.Mode, caughtUp bool) (*songsdir.Report, error) {
	exp, pinnedNames, inProgress, err := d.expected(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := d.dir.Check(ctx, exp, songsdir.CheckOptions{
		Mode: mode, CaughtUp: caughtUp, Pinned: pinnedNames, InProgress: inProgress,
		KnownAccountFolders: d.cfg.OtherAccountFolders,
		ConfirmLarge:        d.lis.LargeDeletion,
	})
	if err != nil {
		return nil, err
	}
	// Verified files are ready; the rest wait for a download.
	need := map[string]bool{}
	for _, e := range rep.NeedsDownload {
		need[e.ContentID] = true
	}
	tracks, _ := d.db.Tracks(ctx)
	for _, t := range tracks {
		if t.State != "ready" || t.CurrentContentID == nil || need[*t.CurrentContentID] {
			continue
		}
		name := songsdir.FileName(*t.CurrentContentID, t.Format)
		if pinnedNames[name] {
			continue
		}
		var readiness sql.NullString
		_ = d.db.QueryRowContext(ctx, `SELECT readiness FROM synced_files WHERE track_id = ?`, t.ID).Scan(&readiness)
		if readiness.String == "pending_apply" || readiness.String == "cleanup_pending" {
			continue
		}
		_, _ = d.db.ExecContext(ctx, `
			INSERT INTO synced_files (track_id, content_id, file_name, readiness) VALUES (?, ?, ?, 'ready')
			ON CONFLICT (track_id) DO UPDATE SET content_id = excluded.content_id, file_name = excluded.file_name, readiness = 'ready', detail = NULL`,
			t.ID, *t.CurrentContentID, name)
	}
	if rep.Removed > 0 || rep.Renamed > 0 || rep.Restored > 0 {
		d.lis.FolderChanged()
	}
	return rep, nil
}

func (d *Device) trackForContent(ctx context.Context, contentID string) (*api.Track, error) {
	var id string
	err := d.db.QueryRowContext(ctx, `SELECT id FROM replica_tracks WHERE content_id = ?`, contentID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return d.db.Track(ctx, id)
}

func (d *Device) markNeedsDownload(ctx context.Context, list []songsdir.Expected, detail string) {
	for _, e := range list {
		t, err := d.trackForContent(ctx, e.ContentID)
		if err != nil || t == nil {
			continue
		}
		_, _ = d.db.ExecContext(ctx, `
			INSERT INTO synced_files (track_id, readiness, detail) VALUES (?, 'needs_download', ?)
			ON CONFLICT (track_id) DO UPDATE SET readiness = CASE WHEN synced_files.readiness IN ('pending_apply', 'cleanup_pending') THEN synced_files.readiness ELSE 'needs_download' END, detail = excluded.detail`,
			t.ID, detail)
	}
}

// downloadMissing downloads each missing mirror: grant, temp file with byte
// counting, SHA-256 check, atomic install — or staging when the song is pinned.
func (d *Device) downloadMissing(ctx context.Context, list []songsdir.Expected) int {
	n := 0
	for _, e := range list {
		if ctx.Err() != nil {
			break
		}
		t, err := d.trackForContent(ctx, e.ContentID)
		if err != nil || t == nil {
			continue
		}
		_, _ = d.db.ExecContext(ctx, `
			INSERT INTO synced_files (track_id, readiness) VALUES (?, 'downloading')
			ON CONFLICT (track_id) DO UPDATE SET readiness = CASE WHEN synced_files.readiness IN ('pending_apply', 'cleanup_pending') THEN synced_files.readiness ELSE 'downloading' END`, t.ID)
		tmp, err := d.download(ctx, e)
		if err != nil {
			slog.Warn("download failed", "track", t.ID, "error", err)
			d.markNeedsDownload(ctx, []songsdir.Expected{e}, "download_failed")
			continue
		}
		if d.pinned(t.ID) && d.playingFileExists(ctx, t.ID) {
			// Staged; installed atomically when the pin is released (R-PIN-3).
			_, _ = d.db.ExecContext(ctx, `UPDATE synced_files SET readiness = 'pending_apply', staged_content_id = ?, staged_path = ? WHERE track_id = ?`, e.ContentID, tmp, t.ID)
			continue
		}
		if err := d.dir.Install(ctx, e, tmp); err != nil {
			d.markNeedsDownload(ctx, []songsdir.Expected{e}, "download_failed")
			continue
		}
		_, _ = d.db.ExecContext(ctx, `
			INSERT INTO synced_files (track_id, content_id, file_name, readiness) VALUES (?, ?, ?, 'ready')
			ON CONFLICT (track_id) DO UPDATE SET content_id = excluded.content_id, file_name = excluded.file_name, readiness = 'ready', staged_content_id = NULL, staged_path = NULL, detail = NULL`,
			t.ID, e.ContentID, e.FileName)
		n++
		d.lis.FolderChanged()
	}
	return n
}

// playingFileExists reports whether the pinned track's file is still on disk. If
// it was deleted by hand there's nothing to protect, so its download installs at
// once and playback can go on.
func (d *Device) playingFileExists(ctx context.Context, trackID string) bool {
	var name sql.NullString
	if err := d.db.QueryRowContext(ctx, `SELECT file_name FROM synced_files WHERE track_id = ?`, trackID).Scan(&name); err != nil || !name.Valid {
		return false
	}
	_, err := os.Stat(d.dir.Path(name.String))
	return err == nil
}

func (d *Device) download(ctx context.Context, e songsdir.Expected) (string, error) {
	g, err := d.api.DownloadGrant(ctx, e.ContentID)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", g.URL, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("download: status %d", res.StatusCode)
	}
	tmp, err := d.dir.TempFile()
	if err != nil {
		return "", err
	}
	// Count received bytes whatever Content-Length says (R-LIM-2).
	n, err := io.Copy(tmp, io.LimitReader(res.Body, e.Size+1))
	tmp.Close()
	if err != nil || n != e.Size {
		os.Remove(tmp.Name())
		if err == nil {
			err = fmt.Errorf("download: got %d bytes, want %d", n, e.Size)
		}
		return "", err
	}
	return tmp.Name(), nil
}

// ApplyPending installs a staged replacement and deletes retired files once a pin is
// released; called by Reconcile through Unpin.
func (d *Device) applyStaged(ctx context.Context) {
	rows, err := d.db.QueryContext(ctx, `SELECT track_id, content_id, file_name, staged_content_id, staged_path FROM synced_files WHERE readiness = 'pending_apply'`)
	if err != nil {
		return
	}
	type st struct {
		track                     string
		oldContent, oldName       sql.NullString
		stagedContent, stagedPath sql.NullString
	}
	var list []st
	for rows.Next() {
		var s st
		if rows.Scan(&s.track, &s.oldContent, &s.oldName, &s.stagedContent, &s.stagedPath) == nil {
			list = append(list, s)
		}
	}
	rows.Close()
	for _, s := range list {
		if d.pinned(s.track) {
			continue
		}
		t, err := d.db.Track(ctx, s.track)
		if err != nil || t == nil || t.CurrentContentID == nil || !s.stagedContent.Valid || *t.CurrentContentID != s.stagedContent.String {
			// The newest pending change wins; a superseded staged file is discarded (R-PIN-4).
			if s.stagedPath.Valid {
				os.Remove(s.stagedPath.String)
			}
			_, _ = d.db.ExecContext(ctx, `UPDATE synced_files SET readiness = 'needs_download', staged_content_id = NULL, staged_path = NULL WHERE track_id = ?`, s.track)
			continue
		}
		e := songsdir.Expected{ContentID: *t.CurrentContentID, FileName: songsdir.FileName(*t.CurrentContentID, t.Format), Size: t.ByteSize, SHA256: t.SHA256}
		if err := d.dir.Install(ctx, e, s.stagedPath.String); err != nil {
			continue
		}
		if s.oldName.Valid && s.oldContent.Valid && s.oldContent.String != e.ContentID {
			_ = d.dir.Remove(s.oldContent.String, s.oldName.String)
		}
		_, _ = d.db.ExecContext(ctx, `UPDATE synced_files SET content_id = ?, file_name = ?, readiness = 'ready', staged_content_id = NULL, staged_path = NULL WHERE track_id = ?`,
			e.ContentID, e.FileName, s.track)
		d.lis.FolderChanged()
	}
}

// CheckSongsFolder is Settings → Check Songs folder: the full check and its report.
func (d *Device) CheckSongsFolder(ctx context.Context) (*ReconcileReport, error) {
	return d.ReconcileReport(ctx, songsdir.Full)
}

// PurgeLocal deletes this account's namespace on the device: its Song Sleeve folder
// and database (Log out and remove, account deletion). Source files are never
// touched (R-LOCAL-4, §7.12).
func (d *Device) PurgeLocal() error {
	d.Stop()
	if err := d.dir.Purge(); err != nil {
		return err
	}
	path := d.db.Path
	d.db.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.RemoveAll(filepath.Join(d.cfg.TempDir))
}
