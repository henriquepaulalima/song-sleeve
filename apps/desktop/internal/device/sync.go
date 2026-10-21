package device

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/api"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/probe"
	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// syncWork is the payload of a pending_work row of kind "sync".
type syncWork struct {
	RequestID   string `json:"request_id"`
	TrackID     string `json:"track_id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	CopyName    string `json:"copy_name,omitempty"`
	Uploaded    bool   `json:"uploaded,omitempty"`
}

// QueueSync puts local songs in the device's sync queue (Sync to cloud, R-CLOUD-1).
// Queued songs are still local; the server knows nothing about them (R-CLOUD-9).
func (d *Device) QueueSync(ctx context.Context, songIDs []string) (int, error) {
	if !d.cloudSync() {
		return 0, &api.Error{Status: 403, Code: "cloud_sync_unavailable", Message: "Cloud sync is coming soon."}
	}
	n := 0
	for _, id := range songIDs {
		ok, err := d.enqueue(ctx, id, false)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	d.kick()
	d.lis.LocalSongsChanged()
	return n, nil
}

// RetryFailed re-queues failed syncs: one song (Try again) or a list's failed songs
// (Retry failed syncs). Never automatic (D-25).
func (d *Device) RetryFailed(ctx context.Context, songIDs []string) (int, error) {
	n := 0
	for _, id := range songIDs {
		ok, err := d.enqueue(ctx, id, true)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	d.kick()
	d.lis.LocalSongsChanged()
	return n, nil
}

func (d *Device) enqueue(ctx context.Context, songID string, retry bool) (bool, error) {
	s, err := d.lib.Song(ctx, songID)
	if err != nil {
		return false, err
	}
	allowed := s.SyncState == "local_only" || (retry && (s.SyncState == "sync_failed" || s.SyncState == "needs_attention"))
	if !allowed || s.Health != "ready" {
		return false, nil
	}
	work, _ := json.Marshal(syncWork{RequestID: uuid.NewString()})
	err = d.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE local_songs SET sync_state = 'queued', sync_failure = NULL WHERE id = ?`, songID); err != nil {
			return err
		}
		id, _ := uuid.NewV7()
		_, err := tx.ExecContext(ctx, `INSERT INTO pending_work (id, kind, subject_id, state, payload, created_at) VALUES (?, 'sync', ?, 'queued', ?, ?)`,
			id.String(), songID, string(work), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
	return err == nil, err
}

// autoSync queues local songs that are in enabled scopes, except failed ones (§7.9 step 6).
func (d *Device) autoSync(ctx context.Context) (int, error) {
	scopes, err := d.db.Scopes(ctx)
	if err != nil {
		return 0, err
	}
	library, playlists := false, map[string]bool{}
	groups, _ := d.db.Groups(ctx)
	for _, s := range scopes {
		if s.State != "enabled" {
			continue
		}
		switch s.Type {
		case "library":
			library = true
		case "playlist":
			if s.CollectionID != nil {
				playlists[*s.CollectionID] = true
			}
		case "group":
			for _, g := range groups {
				if s.CollectionID != nil && g.ID == *s.CollectionID {
					for _, m := range g.Playlists {
						playlists[m.PlaylistID] = true
					}
				}
			}
		}
	}
	if !library && len(playlists) == 0 {
		return 0, nil
	}
	songs, err := d.lib.Songs(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range songs {
		if s.SyncState != "local_only" || s.Health != "ready" {
			continue
		}
		in := library
		if !in {
			entries, _ := d.lib.OverlayEntries(ctx, s.ID)
			for _, p := range entries {
				if playlists[p] {
					in = true
					break
				}
			}
		}
		if in {
			if ok, err := d.enqueue(ctx, s.ID, false); err == nil && ok {
				n++
			}
		}
	}
	if n > 0 {
		d.kick()
	}
	return n, nil
}

// CancelSync cancels a queued or syncing local song. A queued one simply leaves the
// queue; a syncing one fails the sync on the server so other devices drop it (§7.8).
func (d *Device) CancelSync(ctx context.Context, songID string) error {
	var workID, raw, state string
	err := d.db.QueryRowContext(ctx, `SELECT id, payload, state FROM pending_work WHERE kind = 'sync' AND subject_id = ?`, songID).Scan(&workID, &raw, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var w syncWork
	_ = json.Unmarshal([]byte(raw), &w)
	if w.TrackID != "" {
		if err := d.api.Mutate(ctx, "DELETE", "/me/tracks/"+w.TrackID+"?reason=cancel_pending", "", nil, nil); err != nil && api.Code(err) != "not_found" && api.Code(err) != "operation_not_active" {
			return err
		}
	}
	d.runningMu.Lock()
	if cancel, ok := d.running[songID]; ok {
		cancel()
	}
	d.runningMu.Unlock()
	d.cleanupSync(ctx, songID, workID, w, "local_only", "")
	return nil
}

func (d *Device) worker(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-d.stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		workID, songID, w, ok := d.claim(ctx)
		if !ok {
			select {
			case <-d.stop:
				return
			case <-ctx.Done():
				return
			case <-d.syncKick:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		d.runningMu.Lock()
		d.running[songID] = cancel
		d.runningMu.Unlock()
		d.runSync(runCtx, workID, songID, w)
		cancel()
		d.runningMu.Lock()
		delete(d.running, songID)
		d.runningMu.Unlock()
		d.kick()
	}
}

// claim takes the oldest queued sync, atomically.
func (d *Device) claim(ctx context.Context) (string, string, syncWork, bool) {
	var workID, songID, raw string
	err := d.db.InTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id, subject_id, payload FROM pending_work WHERE kind = 'sync' AND state = 'queued' ORDER BY created_at LIMIT 1`).Scan(&workID, &songID, &raw); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE pending_work SET state = 'running' WHERE id = ?`, workID)
		return err
	})
	if err != nil {
		return "", "", syncWork{}, false
	}
	var w syncWork
	_ = json.Unmarshal([]byte(raw), &w)
	return workID, songID, w, true
}

func (d *Device) saveWork(ctx context.Context, workID string, w syncWork) {
	raw, _ := json.Marshal(w)
	_, _ = d.db.ExecContext(ctx, `UPDATE pending_work SET payload = ? WHERE id = ?`, string(raw), workID)
}

type createTrackResp struct {
	Track     api.Track        `json:"track"`
	Operation api.OperationDoc `json:"operation"`
}

// runSync is §7.3 for one song: pre-check, create the track document, place the
// file in Songs, upload, then wait for publication (finished by syncPublished).
func (d *Device) runSync(ctx context.Context, workID, songID string, w syncWork) {
	s, err := d.lib.Song(ctx, songID)
	if err != nil {
		_, _ = d.db.ExecContext(ctx, `DELETE FROM pending_work WHERE id = ?`, workID)
		return
	}
	if w.Uploaded {
		// Uploaded before a restart: publication finishes through reconciliation.
		_, _ = d.db.ExecContext(ctx, `UPDATE pending_work SET state = 'verifying' WHERE id = ?`, workID)
		return
	}

	// Pre-check (F-21): re-read the source and compare with the saved facts.
	if w.TrackID == "" {
		facts, err := probe.File(s.SourcePath)
		if err != nil || facts.SHA256 != s.SHA256 || facts.Bytes != s.ByteSize {
			d.cleanupSync(ctx, songID, workID, w, "needs_attention", "source_changed")
			return
		}
	}

	// Step 1: create the track document (only now, when a worker starts it).
	if w.TrackID == "" {
		body := map[string]any{"name": s.Name, "duration_ms": s.DurationMs, "byte_size": s.ByteSize, "format": s.Format, "sha256": s.SHA256}
		if s.ArtistName != nil {
			ref := map[string]any{"name": *s.ArtistName}
			if s.ArtistID != nil {
				ref["id"] = *s.ArtistID // the server falls back to the name if the ID is stale
			}
			body["artist"] = ref
		}
		entries, _ := d.lib.OverlayEntries(ctx, songID)
		body["playlist_ids"] = entries
		var resp createTrackResp
		err := d.mutateWithRecovery(ctx, "POST", "/me/tracks", w.RequestID, body, &resp)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			reason := api.Code(err)
			if reason == "" || reason == "connection_required" {
				// Not reachable: leave it queued for the next attempt.
				_, _ = d.db.ExecContext(ctx, `UPDATE pending_work SET state = 'queued' WHERE id = ?`, workID)
				time.Sleep(2 * time.Second)
				return
			}
			d.cleanupSync(ctx, songID, workID, w, "sync_failed", reason)
			return
		}
		w.TrackID, w.OperationID = resp.Track.ID, resp.Operation.ID
		w.CopyName = songsdir.SyncCopyName(resp.Track.ID, s.Format)
		d.saveWork(ctx, workID, w)
		_, _ = d.db.ExecContext(ctx, `UPDATE local_songs SET sync_state = 'syncing', syncing_track_id = ? WHERE id = ?`, resp.Track.ID, songID)
		d.lis.LocalSongsChanged()
	}

	// Step 2: copy the source into Songs and verify it (R-CLOUD-4).
	copyPath := d.dir.Path(w.CopyName)
	if st, err := os.Stat(copyPath); err != nil || st.Size() != s.ByteSize {
		tmp, err := d.dir.TempFile()
		if err != nil {
			return
		}
		src, err := os.Open(s.SourcePath)
		if err == nil {
			_, err = io.Copy(tmp, src)
			src.Close()
		}
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			d.failServer(ctx, songID, workID, w, "needs_attention", "source_changed")
			return
		}
		if err := d.dir.PlaceSyncCopy(tmp.Name(), w.CopyName, s.SHA256, s.ByteSize); err != nil {
			// The file changed between the pre-check and the copy (§7.3 step 2).
			d.failServer(ctx, songID, workID, w, "needs_attention", "source_changed")
			return
		}
	}

	// Step 3: upload exactly those bytes.
	if err := d.upload(ctx, songID, w, copyPath, s.ByteSize); err != nil {
		if ctx.Err() != nil {
			return
		}
		switch api.Code(err) {
		case "not_found", "operation_not_active":
			// Cancelled or failed elsewhere: the failure event finishes it.
			return
		}
		slog.Warn("upload failed", "track", w.TrackID, "error", err)
		d.failServer(ctx, songID, workID, w, "sync_failed", "upload_error")
		return
	}
	w.Uploaded = true
	d.saveWork(ctx, workID, w)
	_, _ = d.db.ExecContext(ctx, `UPDATE pending_work SET state = 'verifying' WHERE id = ?`, workID)
	// Steps 4–5 finish when reconciliation sees the track ready (syncPublished).
}

// mutateWithRecovery sends a mutation and recovers an unknown result with the same
// request ID instead of resubmitting it as a new action (R-MUT-4).
func (d *Device) mutateWithRecovery(ctx context.Context, method, path, requestID string, body, out any) error {
	for attempt := 0; ; attempt++ {
		err := d.api.Mutate(ctx, method, path, requestID, body, out)
		var unknown *api.UnknownResult
		if !errors.As(err, &unknown) || attempt >= 5 {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
		st, serr := d.api.MutationStatus(ctx, requestID)
		if serr != nil {
			continue
		}
		switch st.Status {
		case "committed":
			return json.Unmarshal(st.Result, out)
		case "rejected":
			var env struct {
				Error api.Error `json:"error"`
			}
			_ = json.Unmarshal(st.Result, &env)
			env.Error.Status = st.HTTPStatus
			return &env.Error
		}
		// not_applied: safe to send again with the same request ID.
	}
}

func (d *Device) upload(ctx context.Context, songID string, w syncWork, path string, size int64) error {
	var op struct {
		Operation api.OperationDoc `json:"operation"`
	}
	if err := d.api.Do(ctx, "GET", "/me/operations/"+w.OperationID, nil, &op); err != nil {
		return err
	}
	o := op.Operation
	if o.State != "active" {
		return &api.Error{Code: "operation_not_active"}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	type part struct {
		Number int    `json:"part_number"`
		ETag   string `json:"etag"`
	}
	var parts []part
	var confirmed int64
	buf := make([]byte, o.PartSize)
	for n := 1; n <= o.PartCount; n++ {
		k, err := io.ReadFull(f, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return err
		}
		chunk := buf[:k]
		var etag string
		for attempt := 0; ; attempt++ {
			var g struct {
				Parts []struct {
					URL string `json:"url"`
				} `json:"parts"`
			}
			err := d.api.Do(ctx, "POST", "/me/operations/"+o.ID+"/upload-grant", map[string]any{"part_numbers": []int{n}}, &g)
			if err == nil {
				etag, err = putPart(ctx, g.Parts[0].URL, chunk)
			}
			if err == nil {
				break
			}
			var apiErr *api.Error
			if errors.As(err, &apiErr) || attempt >= 8 || ctx.Err() != nil {
				return err
			}
			// Network errors retry on the same operation within the stall window.
			time.Sleep(time.Duration(min(30, 1<<attempt)) * time.Second)
		}
		parts = append(parts, part{Number: n, ETag: etag})
		confirmed += int64(k)
		var hb struct {
			Operation api.OperationDoc `json:"operation"`
		}
		if err := d.api.Do(ctx, "POST", "/me/operations/"+o.ID+"/heartbeat", map[string]any{"confirmed_bytes": confirmed}, &hb); err != nil {
			return err
		}
		if hb.Operation.State != "active" {
			return &api.Error{Code: "operation_not_active"}
		}
		d.lis.SyncProgress(songID, confirmed, size)
	}
	for attempt := 0; ; attempt++ {
		err := d.api.Do(ctx, "POST", "/me/operations/"+o.ID+"/uploaded", map[string]any{"parts": parts}, nil)
		if err == nil {
			return nil
		}
		var apiErr *api.Error
		if errors.As(err, &apiErr) || attempt >= 5 {
			return err
		}
		time.Sleep(time.Second << attempt)
	}
}

func putPart(ctx context.Context, url string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != 200 {
		return "", fmt.Errorf("part upload: status %d", res.StatusCode)
	}
	return res.Header.Get("ETag"), nil
}

// failServer cancels the server-side sync (so every device drops the pending
// track at once) and returns the song to local with the given state.
func (d *Device) failServer(ctx context.Context, songID, workID string, w syncWork, state, reason string) {
	if w.TrackID != "" {
		_ = d.api.Mutate(ctx, "DELETE", "/me/tracks/"+w.TrackID+"?reason=cancel_pending", "", nil, nil)
	}
	d.cleanupSync(ctx, songID, workID, w, state, reason)
}

// cleanupSync removes the Songs copy and the queue entry and sets the song's state.
// The song stays local, pointing at its source file, with its overlay entries.
func (d *Device) cleanupSync(ctx context.Context, songID, workID string, w syncWork, state, reason string) {
	if w.CopyName != "" {
		_ = d.dir.RemoveSyncCopy(w.CopyName)
	}
	var failure any
	if reason != "" {
		failure = reason
	}
	_, _ = d.db.ExecContext(ctx, `UPDATE local_songs SET sync_state = ?, sync_failure = ?, syncing_track_id = NULL WHERE id = ?`, state, failure, songID)
	_, _ = d.db.ExecContext(ctx, `DELETE FROM pending_work WHERE id = ?`, workID)
	d.lis.LocalSongsChanged()
}

// workForTrack finds this device's sync of a track.
func (d *Device) workForTrack(ctx context.Context, trackID string) (string, string, syncWork, bool) {
	var workID, songID, raw string
	err := d.db.QueryRowContext(ctx, `SELECT id, subject_id, payload FROM pending_work WHERE kind = 'sync' AND json_extract(payload, '$.track_id') = ?`, trackID).Scan(&workID, &songID, &raw)
	if err != nil {
		return "", "", syncWork{}, false
	}
	var w syncWork
	_ = json.Unmarshal([]byte(raw), &w)
	return workID, songID, w, true
}

// syncFailed handles track_sync_failed for this device's sync: the song is local
// again, marked failed with Try again (R-ID-4). Never retried automatically (D-25).
func (d *Device) syncFailed(ctx context.Context, trackID, reason string) {
	workID, songID, w, ok := d.workForTrack(ctx, trackID)
	if !ok {
		return
	}
	d.runningMu.Lock()
	if cancel, ok := d.running[songID]; ok {
		cancel()
	}
	d.runningMu.Unlock()
	state := "sync_failed"
	if reason == "cancelled" {
		state = "local_only"
		reason = ""
	}
	d.cleanupSync(ctx, songID, workID, w, state, reason)
}

// syncPublished finishes this device's sync once the track is ready (§7.3 step 5):
// the local song becomes the Songs mirror and its overlay entries are dropped,
// because they are server memberships now. The source file stays where it was.
func (d *Device) syncPublished(ctx context.Context, t *api.Track) {
	workID, songID, w, ok := d.workForTrack(ctx, t.ID)
	if !ok || w.CopyName == "" {
		return
	}
	e := songsdir.Expected{ContentID: *t.CurrentContentID, FileName: songsdir.FileName(*t.CurrentContentID, t.Format), Size: t.ByteSize, SHA256: t.SHA256}
	if err := d.dir.AdoptSyncCopy(w.CopyName, e); err != nil {
		slog.Warn("adopt sync copy", "error", err)
		return
	}
	_ = d.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO synced_files (track_id, content_id, file_name, readiness) VALUES (?, ?, ?, 'ready')
			ON CONFLICT (track_id) DO UPDATE SET content_id = excluded.content_id, file_name = excluded.file_name, readiness = 'ready'`,
			t.ID, e.ContentID, e.FileName); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_songs WHERE id = ?`, songID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM pending_work WHERE id = ?`, workID)
		return err
	})
	d.lis.LocalSongsChanged()
	d.lis.FolderChanged()
}
