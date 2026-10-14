package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/catalog"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/media"
	"github.com/henriquepaulalima/song-sleeve/server/internal/mutations"
	"github.com/henriquepaulalima/song-sleeve/server/internal/ratelimit"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

type Handlers struct{ d *app.Deps }

func New(d *app.Deps) *Handlers { return &Handlers{d: d} }

// OperationDoc is the client view of an operation and its upload plan.
type OperationDoc struct {
	ID             uuid.UUID `json:"id"`
	TrackID        uuid.UUID `json:"track_id"`
	Kind           string    `json:"kind"`
	State          string    `json:"state"`
	Phase          string    `json:"phase"`
	ConfirmedBytes int64     `json:"confirmed_bytes"`
	DeclaredBytes  int64     `json:"declared_bytes"`
	PartSize       int64     `json:"part_size"`
	PartCount      int       `json:"part_count"`
	FailureReason  *string   `json:"failure_reason"`
	HeartbeatSecs  int       `json:"heartbeat_interval_seconds"`
}

func (h *Handlers) doc(op *Operation) OperationDoc {
	d := OperationDoc{ID: op.ID, TrackID: op.TrackID, Kind: op.Kind, State: op.State, Phase: op.Phase,
		ConfirmedBytes: op.ConfirmedBytes, FailureReason: op.FailureReason, PartSize: h.d.Cfg.UploadPartSize,
		HeartbeatSecs: int(h.d.Cfg.HeartbeatInterval.Seconds())}
	if op.DeclaredBytes != nil {
		d.DeclaredBytes = *op.DeclaredBytes
		d.PartCount = int((*op.DeclaredBytes + h.d.Cfg.UploadPartSize - 1) / h.d.Cfg.UploadPartSize)
	}
	return d
}

type uploadFacts struct {
	DurationMs int64  `json:"duration_ms"`
	ByteSize   int64  `json:"byte_size"`
	Format     string `json:"format"`
	SHA256     string `json:"sha256"`
}

type createTrackRequest struct {
	uploadFacts
	Name        string             `json:"name"`
	Artist      *catalog.ArtistRef `json:"artist"`
	PlaylistIDs []uuid.UUID        `json:"playlist_ids"`
}

func (h *Handlers) reserve(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bytes int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE users SET storage_reserved_bytes = storage_reserved_bytes + $2
		WHERE id = $1 AND cloud_sync_enabled AND storage_used_bytes + storage_reserved_bytes + $2 <= storage_quota_bytes`, userID, bytes)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.StorageQuotaExceeded, "Your cloud storage is full. Delete synced songs to free space.")
	}
	return nil
}

// CreateTrack starts a sync (R-ID-3 step 1): it creates the pending track document
// with its metadata, library position and memberships, reserves its size, and opens
// the initial_upload operation. Every device now shows Syncing… (R-ID-4).
func (h *Handlers) CreateTrack(c *gin.Context) {
	var req createTrackRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		if err := media.CheckDeclared(req.DurationMs, req.ByteSize, req.Format, req.SHA256); err != nil {
			return mutations.Result{}, err
		}
		name, err := fields.Name("name", req.Name, fields.MaxSongName)
		if err != nil {
			return mutations.Result{}, err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE user_id = $1`, s.UserID).Scan(&count); err != nil {
			return mutations.Result{}, err
		}
		if count >= catalog.MaxSyncedSongs {
			return mutations.Result{}, apperr.WithDetails(apperr.LimitReached, "Song limit reached", map[string]any{"limit": catalog.MaxSyncedSongs})
		}
		if err := h.reserve(ctx, tx, s.UserID, req.ByteSize); err != nil {
			return mutations.Result{}, err
		}
		artistID, err := catalog.ResolveArtist(ctx, tx, s.UserID, req.Artist)
		if err != nil {
			return mutations.Result{}, err
		}
		pos, err := catalog.NextLibraryPosition(ctx, tx, s.UserID)
		if err != nil {
			return mutations.Result{}, err
		}
		trackID, _ := uuid.NewV7()
		if _, err := tx.Exec(ctx, `
			INSERT INTO tracks (id, user_id, name, artist_id, duration_ms, byte_size, format, sha256, state, origin_device_id, library_position, pending_since)
			VALUES ($1, $2, $3, $4, $5, $6, $7, lower($8), 'pending_upload', $9, $10, now())`,
			trackID, s.UserID, name, artistID, req.DurationMs, req.ByteSize, req.Format, req.SHA256, s.DeviceID, pos); err != nil {
			return mutations.Result{}, err
		}
		// Memberships to playlists that no longer exist are dropped, never a
		// reason to reject the sync (F-10).
		dropped := []uuid.UUID{}
		for _, pid := range dedupe(req.PlaylistIDs) {
			var rev int64
			err := tx.QueryRow(ctx, `UPDATE playlists SET revision = revision + 1 WHERE user_id = $1 AND id = $2 RETURNING revision`, s.UserID, pid).Scan(&rev)
			if db.IsNoRows(err) {
				dropped = append(dropped, pid)
				continue
			}
			if err != nil {
				return mutations.Result{}, err
			}
			if err := catalog.AppendPlaylistTracks(ctx, tx, s.UserID, pid, []uuid.UUID{trackID}); err != nil {
				return mutations.Result{}, err
			}
			if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityPlaylist, EntityID: pid, EventType: "playlist_upserted", Revision: rev}); err != nil {
				return mutations.Result{}, err
			}
		}
		opID, _ := uuid.NewV7()
		attempt, _ := uuid.NewV7()
		key := storage.StagingKey(s.UserID.String(), opID.String(), attempt.String())
		uploadID, err := h.d.Store.CreateMultipart(ctx, key)
		if err != nil {
			return mutations.Result{}, err
		}
		requestID := c.GetHeader("Idempotency-Key")
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_sync_operations (id, user_id, track_id, kind, state, request_id, owner_device_id, staging_key, multipart_upload_id,
				declared_bytes, declared_sha256, declared_duration_ms, declared_format, reserved_bytes, last_progress_at, activated_at)
			VALUES ($1, $2, $3, 'initial_upload', 'active', $4, $5, $6, $7, $8, lower($9), $10, $11, $8, now(), now())`,
			opID, s.UserID, trackID, requestID, s.DeviceID, key, uploadID, req.ByteSize, req.SHA256, req.DurationMs, req.Format); err != nil {
			return mutations.Result{}, err
		}
		if _, err := events.Append(ctx, tx, events.Event{UserID: s.UserID, EntityType: events.EntityTrack, EntityID: trackID, EventType: "track_upserted", Revision: 1}); err != nil {
			return mutations.Result{}, err
		}
		track, err := catalog.LoadTrack(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		op, err := LoadOperation(ctx, tx, s.UserID, opID)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.Result{Status: http.StatusCreated, OperationID: &opID,
			Body: gin.H{"track": track, "operation": h.doc(op), "dropped_playlist_ids": dropped}}, nil
	})
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

type replacementRequest struct {
	uploadFacts
	ExpectedRevision int64           `json:"expected_revision"`
	Name             string          `json:"name"`
	Artist           json.RawMessage `json:"artist"`
}

// Replacement queues a replacement after the suggestion step (§7.7, R-MUT-5). The
// chosen values commit together with the new content; the old content stays
// current and playable until then (R-ID-4). Every replacement creates fresh
// content, even for an identical file (R-ID-5).
func (h *Handlers) Replacement(c *gin.Context) {
	trackID, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req replacementRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		if err := media.CheckDeclared(req.DurationMs, req.ByteSize, req.Format, req.SHA256); err != nil {
			return mutations.Result{}, err
		}
		name, err := fields.Name("name", req.Name, fields.MaxSongName)
		if err != nil {
			return mutations.Result{}, err
		}
		if len(req.Artist) > 0 && string(req.Artist) != "null" {
			var ref catalog.ArtistRef
			if json.Unmarshal(req.Artist, &ref) != nil || (ref.ID == nil && ref.Name == nil) {
				return mutations.Result{}, apperr.Field("artist", "invalid")
			}
			if ref.Name != nil {
				if _, err := fields.Name("artist", *ref.Name, fields.MaxArtistName); err != nil {
					return mutations.Result{}, err
				}
			}
		}
		state, rev, err := lockTrack(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		if state != "ready" {
			return mutations.Result{}, apperr.New(apperr.OperationNotActive, "This song is still syncing.")
		}
		if rev != req.ExpectedRevision {
			return mutations.Result{}, apperr.WithDetails(apperr.RevisionConflict, "This song changed on another device. Review it and try again.", map[string]any{"current_revision": rev})
		}
		var base baseMetadata
		if err := tx.QueryRow(ctx, `SELECT name, artist_id FROM tracks WHERE id = $1`, trackID).Scan(&base.Name, &base.ArtistID); err != nil {
			return mutations.Result{}, err
		}
		if err := h.reserve(ctx, tx, s.UserID, req.ByteSize); err != nil {
			return mutations.Result{}, err
		}
		busy, err := slotBusy(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		opID, _ := uuid.NewV7()
		attempt, _ := uuid.NewV7()
		key := storage.StagingKey(s.UserID.String(), opID.String(), attempt.String())
		state = "waiting"
		var uploadID *string
		if !busy {
			id, err := h.d.Store.CreateMultipart(ctx, key)
			if err != nil {
				return mutations.Result{}, err
			}
			state, uploadID = "active", &id
		}
		chosen, _ := json.Marshal(chosenMetadata{Name: name, Artist: req.Artist})
		baseRaw, _ := json.Marshal(base)
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_sync_operations (id, user_id, track_id, kind, state, request_id, owner_device_id, staging_key, multipart_upload_id,
				declared_bytes, declared_sha256, declared_duration_ms, declared_format, chosen_metadata, base_metadata, reserved_bytes,
				last_progress_at, activated_at)
			VALUES ($1, $2, $3, 'replace', $4, $5, $6, $7, $8, $9, lower($10), $11, $12, $13, $14, $9,
				CASE WHEN $4 = 'active' THEN now() END, CASE WHEN $4 = 'active' THEN now() END)`,
			opID, s.UserID, trackID, state, c.GetHeader("Idempotency-Key"), s.DeviceID, key, uploadID,
			req.ByteSize, req.SHA256, req.DurationMs, req.Format, chosen, baseRaw); err != nil {
			return mutations.Result{}, err
		}
		if err := trackEvent(ctx, tx, s.UserID, trackID, nil); err != nil {
			return mutations.Result{}, err
		}
		op, err := LoadOperation(ctx, tx, s.UserID, opID)
		if err != nil {
			return mutations.Result{}, err
		}
		track, err := catalog.LoadTrack(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.Result{Status: http.StatusAccepted, OperationID: &opID, Body: gin.H{"track": track, "operation": h.doc(op)}}, nil
	})
}

// DeleteTrack deletes a synced song, or cancels a syncing one (§7.8). A delete
// waits in the queue while another operation holds the slot.
func (h *Handlers) DeleteTrack(c *gin.Context) {
	trackID, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	reason := c.DefaultQuery("reason", "delete")
	if reason != "delete" && reason != "cancel_pending" {
		httpx.Fail(c, apperr.Field("reason", "invalid"))
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		state, _, err := lockTrack(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		if state == "pending_upload" {
			op, err := scanOp(tx.QueryRow(ctx, `SELECT `+opColumns+` FROM track_sync_operations WHERE user_id = $1 AND track_id = $2 AND kind = 'initial_upload' AND state = 'active' FOR UPDATE`, s.UserID, trackID))
			if err != nil {
				return mutations.Result{}, err
			}
			why := ReasonCancelled
			if reason == "delete" {
				why = ReasonDeleted
			}
			if err := FailInitialUpload(ctx, h.d, tx, op, why); err != nil {
				return mutations.Result{}, err
			}
			return mutations.OK(gin.H{"deleted": true, "sync_cancelled": true}), nil
		}
		if reason == "cancel_pending" {
			return mutations.Result{}, apperr.New(apperr.OperationNotActive, "This song is not syncing.")
		}
		busy, err := slotBusy(ctx, tx, s.UserID, trackID)
		if err != nil {
			return mutations.Result{}, err
		}
		if !busy {
			var waiting bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM track_sync_operations WHERE user_id = $1 AND track_id = $2 AND state = 'waiting')`, s.UserID, trackID).Scan(&waiting); err != nil {
				return mutations.Result{}, err
			}
			busy = waiting
		}
		if !busy {
			if err := ExecuteDelete(ctx, h.d, tx, s.UserID, trackID); err != nil {
				return mutations.Result{}, err
			}
			return mutations.OK(gin.H{"deleted": true}), nil
		}
		// Waiting for another sync operation (R-SYNC-2).
		var existing uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM track_sync_operations WHERE user_id = $1 AND track_id = $2 AND kind = 'delete' AND state = 'waiting'`, s.UserID, trackID).Scan(&existing)
		if err == nil {
			op, err := LoadOperation(ctx, tx, s.UserID, existing)
			if err != nil {
				return mutations.Result{}, err
			}
			return mutations.Result{Status: http.StatusAccepted, OperationID: &existing, Body: gin.H{"deleted": false, "operation": h.doc(op)}}, nil
		}
		if !db.IsNoRows(err) {
			return mutations.Result{}, err
		}
		opID, _ := uuid.NewV7()
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_sync_operations (id, user_id, track_id, kind, state, request_id, owner_device_id)
			VALUES ($1, $2, $3, 'delete', 'waiting', $4, $5)`, opID, s.UserID, trackID, c.GetHeader("Idempotency-Key"), s.DeviceID); err != nil {
			return mutations.Result{}, err
		}
		if err := trackEvent(ctx, tx, s.UserID, trackID, nil); err != nil {
			return mutations.Result{}, err
		}
		op, err := LoadOperation(ctx, tx, s.UserID, opID)
		if err != nil {
			return mutations.Result{}, err
		}
		return mutations.Result{Status: http.StatusAccepted, OperationID: &opID, Body: gin.H{"deleted": false, "operation": h.doc(op)}}, nil
	})
}

func (h *Handlers) GetOperation(c *gin.Context) {
	s := httpx.CurrentSession(c)
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	op, err := LoadOperation(c.Request.Context(), h.d.Pool, s.UserID, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": h.doc(op)})
}

type grantRequest struct {
	PartNumbers []int32 `json:"part_numbers"`
}

// UploadGrant issues short-lived presigned PUTs for staging parts, only to the
// operation's active owner device (§6).
func (h *Handlers) UploadGrant(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req grantRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if len(req.PartNumbers) == 0 || len(req.PartNumbers) > 32 {
		httpx.Fail(c, apperr.Field("part_numbers", "invalid"))
		return
	}
	if err := h.d.Limiter.Allow(ctx, ratelimit.Grants, s.UserID.String()); err != nil {
		httpx.Fail(c, err)
		return
	}
	type grant struct {
		PartNumber int32  `json:"part_number"`
		URL        string `json:"url"`
	}
	var grants []grant
	expires := time.Now().Add(h.d.Cfg.UploadGrantTTL)
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		op, err := LockOperation(ctx, tx, s.UserID, id)
		if err != nil {
			return err
		}
		if op.State != "active" || op.Phase != "uploading" || op.Kind == "delete" || op.MultipartUploadID == nil {
			return apperr.New(apperr.OperationNotActive, "This upload is not active.")
		}
		if op.OwnerDeviceID == nil || *op.OwnerDeviceID != s.DeviceID {
			return apperr.New(apperr.Forbidden, "Only the device that started this upload can send it.")
		}
		maxPart := int32((*op.DeclaredBytes + h.d.Cfg.UploadPartSize - 1) / h.d.Cfg.UploadPartSize)
		for _, n := range req.PartNumbers {
			if n < 1 || n > maxPart {
				return apperr.Field("part_numbers", "out_of_range")
			}
			url, err := h.d.Store.PresignUploadPart(ctx, *op.StagingKey, *op.MultipartUploadID, n, h.d.Cfg.UploadGrantTTL)
			if err != nil {
				return err
			}
			grants = append(grants, grant{PartNumber: n, URL: url})
		}
		_, err = tx.Exec(ctx, `UPDATE track_sync_operations SET grant_expires_at = GREATEST(COALESCE(grant_expires_at, $2), $2) WHERE id = $1`, op.ID, expires)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"parts": grants, "expires_at": expires})
}

type heartbeatRequest struct {
	ConfirmedBytes int64 `json:"confirmed_bytes"`
}

// Heartbeat records liveness and confirmed bytes. Only growing confirmed bytes
// count as progress for the stall timeout (R-ID-4); progress hints are coalesced.
func (h *Handlers) Heartbeat(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req heartbeatRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	var out *Operation
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		op, err := LockOperation(ctx, tx, s.UserID, id)
		if err != nil {
			return err
		}
		out = op
		if op.State != "active" || op.Phase != "uploading" {
			return nil
		}
		if op.OwnerDeviceID == nil || *op.OwnerDeviceID != s.DeviceID {
			return apperr.New(apperr.Forbidden, "Only the uploading device sends heartbeats.")
		}
		confirmed := min(req.ConfirmedBytes, *op.DeclaredBytes)
		progressed := confirmed > op.ConfirmedBytes
		if _, err := tx.Exec(ctx, `
			UPDATE track_sync_operations SET heartbeat_at = now(),
				confirmed_bytes = GREATEST(confirmed_bytes, $2),
				last_progress_at = CASE WHEN $3 THEN now() ELSE last_progress_at END
			WHERE id = $1`, op.ID, confirmed, progressed); err != nil {
			return err
		}
		if progressed {
			out.ConfirmedBytes = confirmed
			if op.LastHintAt == nil || time.Since(*op.LastHintAt) >= h.d.Cfg.ProgressHintInterval {
				if _, err := tx.Exec(ctx, `UPDATE track_sync_operations SET last_hint_at = now() WHERE id = $1`, op.ID); err != nil {
					return err
				}
				return events.Hint(ctx, tx, s.UserID, "progress", gin.H{
					"track_id": op.TrackID, "operation_id": op.ID, "kind": op.Kind,
					"confirmed_bytes": confirmed, "declared_bytes": *op.DeclaredBytes,
				})
			}
		}
		return nil
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": h.doc(out)})
}

type uploadedRequest struct {
	Parts []storage.Part `json:"parts"`
}

// Uploaded completes the multipart upload and hands the staging object to the
// worker for verification and publication (R-ID-3 steps 3–4).
func (h *Handlers) Uploaded(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req uploadedRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	op, err := LoadOperation(ctx, h.d.Pool, s.UserID, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if op.State != "active" || op.MultipartUploadID == nil {
		httpx.Fail(c, apperr.New(apperr.OperationNotActive, "This upload is not active."))
		return
	}
	if op.OwnerDeviceID == nil || *op.OwnerDeviceID != s.DeviceID {
		httpx.Fail(c, apperr.New(apperr.Forbidden, "Only the device that started this upload can finish it."))
		return
	}
	if op.Phase == "uploading" {
		slices.SortFunc(req.Parts, func(a, b storage.Part) int { return int(a.Number - b.Number) })
		if err := h.d.Store.CompleteMultipart(ctx, *op.StagingKey, *op.MultipartUploadID, req.Parts); err != nil {
			httpx.Fail(c, apperr.New(apperr.InvalidRequest, "The upload is incomplete. Send the missing parts and try again."))
			return
		}
	}
	err = db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		locked, err := LockOperation(ctx, tx, s.UserID, id)
		if err != nil {
			return err
		}
		if locked.State != "active" {
			return apperr.New(apperr.OperationNotActive, "This upload is not active.")
		}
		if locked.Phase == "verifying" {
			return nil // repeated call
		}
		if _, err := tx.Exec(ctx, `
			UPDATE track_sync_operations SET phase = 'verifying', confirmed_bytes = declared_bytes, last_progress_at = now()
			WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := h.d.River.InsertTx(ctx, tx, jobs.VerifyPublish{UserID: s.UserID, OperationID: id},
			&river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil {
			return err
		}
		return trackEvent(ctx, tx, s.UserID, op.TrackID, nil)
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	op, err = LoadOperation(ctx, h.d.Pool, s.UserID, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": h.doc(op)})
}

// Cancel cancels a waiting or active upload or replacement from any of the
// account's devices. An initial upload fails the sync; a replacement keeps the old
// content (§6).
func (h *Handlers) Cancel(c *gin.Context) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mutations.Run(c, h.d, func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (mutations.Result, error) {
		op, err := LockOperation(ctx, tx, s.UserID, id)
		if err != nil {
			return mutations.Result{}, err
		}
		if op.State != "active" && op.State != "waiting" {
			return mutations.Result{}, apperr.New(apperr.OperationNotActive, "This operation already finished.")
		}
		if err := Fail(ctx, h.d, tx, op, ReasonCancelled); err != nil {
			return mutations.Result{}, err
		}
		return mutations.OK(gin.H{"cancelled": true}), nil
	})
}

// DownloadGrant issues a GET grant for the current content of a ready track
// (R-CLOUD-6, R-PLAY-2). Downloads never take the slot (D-19); the grant expiry is
// remembered so retired content outlives every grant issued for it (R-SYNC-1).
func (h *Handlers) DownloadGrant(c *gin.Context) {
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	id, err := httpx.ParamUUID(c, "contentID")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.d.Limiter.Allow(ctx, ratelimit.Grants, s.UserID.String()); err != nil {
		httpx.Fail(c, err)
		return
	}
	expires := time.Now().Add(h.d.Cfg.DownloadGrantTTL)
	var key, sha string
	var size int64
	err = h.d.Pool.QueryRow(ctx, `
		UPDATE track_content tc SET last_grant_expires_at = GREATEST(COALESCE(tc.last_grant_expires_at, $3), $3)
		FROM tracks t
		WHERE tc.user_id = $1 AND tc.id = $2 AND tc.state = 'current'
		  AND t.user_id = tc.user_id AND t.id = tc.track_id AND t.state = 'ready' AND t.current_content_id = tc.id
		RETURNING tc.object_key, tc.byte_size, tc.sha256`, s.UserID, id, expires).Scan(&key, &size, &sha)
	if db.IsNoRows(err) {
		httpx.Fail(c, apperr.New(apperr.NotFound, "This version of the song is no longer available."))
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	url, err := h.d.Store.PresignGet(ctx, key, h.d.Cfg.DownloadGrantTTL)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url, "expires_at": expires, "byte_size": size, "sha256": sha})
}
