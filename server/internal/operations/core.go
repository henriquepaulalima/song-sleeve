// Package operations runs the per-track operation slot and the upload pipeline:
// create a pending track, grant staging uploads, track progress, verify and
// publish, and undo everything on failure (R-ID-3, R-ID-4, R-SYNC, R-LIM-5).
package operations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

// Failure reasons shown as "Sync failed: {reason}".
const (
	ReasonCancelled        = "cancelled"
	ReasonDeleted          = "deleted"
	ReasonStalled          = "stalled"
	ReasonChecksumMismatch = "checksum_mismatch"
	ReasonSizeMismatch     = "size_mismatch"
	ReasonTooLong          = "song_too_long"
	ReasonTooLarge         = "song_too_large"
	ReasonUnsupported      = "unsupported_format"
	ReasonInvalidAudio     = "invalid_audio_metadata"
	ReasonUploadError      = "upload_error"
	ReasonAccountDeleted   = "account_deleted"
)

type Operation struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	TrackID            uuid.UUID
	Kind               string
	State              string
	Phase              string
	RequestID          string
	OwnerDeviceID      *uuid.UUID
	CandidateContentID *uuid.UUID
	StagingKey         *string
	MultipartUploadID  *string
	DeclaredBytes      *int64
	DeclaredSHA256     *string
	DeclaredDurationMs *int32
	DeclaredFormat     *string
	ChosenMetadata     []byte
	BaseMetadata       []byte
	ReservedBytes      int64
	ConfirmedBytes     int64
	LastProgressAt     *time.Time
	LastHintAt         *time.Time
	GrantExpiresAt     *time.Time
	FailureReason      *string
	CreatedAt          time.Time
}

const opColumns = `id, user_id, track_id, kind, state, phase, request_id, owner_device_id, candidate_content_id,
	staging_key, multipart_upload_id, declared_bytes, declared_sha256, declared_duration_ms, declared_format,
	chosen_metadata, base_metadata, reserved_bytes, confirmed_bytes, last_progress_at, last_hint_at, grant_expires_at,
	failure_reason, created_at`

func scanOp(row pgx.Row) (*Operation, error) {
	var o Operation
	err := row.Scan(&o.ID, &o.UserID, &o.TrackID, &o.Kind, &o.State, &o.Phase, &o.RequestID, &o.OwnerDeviceID, &o.CandidateContentID,
		&o.StagingKey, &o.MultipartUploadID, &o.DeclaredBytes, &o.DeclaredSHA256, &o.DeclaredDurationMs, &o.DeclaredFormat,
		&o.ChosenMetadata, &o.BaseMetadata, &o.ReservedBytes, &o.ConfirmedBytes, &o.LastProgressAt, &o.LastHintAt, &o.GrantExpiresAt,
		&o.FailureReason, &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// LockOperation loads an operation FOR UPDATE, scoped to its owner (R-MUT-6).
func LockOperation(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID) (*Operation, error) {
	op, err := scanOp(tx.QueryRow(ctx, `SELECT `+opColumns+` FROM track_sync_operations WHERE user_id = $1 AND id = $2 FOR UPDATE`, userID, id))
	if db.IsNoRows(err) {
		return nil, apperr.New(apperr.NotFound, "Operation not found")
	}
	return op, err
}

func LoadOperation(ctx context.Context, q db.Querier, userID, id uuid.UUID) (*Operation, error) {
	op, err := scanOp(q.QueryRow(ctx, `SELECT `+opColumns+` FROM track_sync_operations WHERE user_id = $1 AND id = $2`, userID, id))
	if db.IsNoRows(err) {
		return nil, apperr.New(apperr.NotFound, "Operation not found")
	}
	return op, err
}

// slotBusy reports whether a track's slot is taken: an active operation, or a
// failed one whose upload grants could still write (R-SYNC-4).
func slotBusy(ctx context.Context, q db.Querier, userID, trackID uuid.UUID) (bool, error) {
	var busy bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM track_sync_operations
			WHERE user_id = $1 AND track_id = $2
			  AND (state = 'active' OR (state IN ('failed', 'cancelled') AND grant_expires_at > now()))
		)`, userID, trackID).Scan(&busy)
	return busy, err
}

func lockTrack(ctx context.Context, tx pgx.Tx, userID, trackID uuid.UUID) (state string, revision int64, err error) {
	err = tx.QueryRow(ctx, `SELECT state, revision FROM tracks WHERE user_id = $1 AND id = $2 FOR UPDATE`, userID, trackID).Scan(&state, &revision)
	if db.IsNoRows(err) {
		err = apperr.New(apperr.NotFound, "Song not found")
	}
	return
}

func trackEvent(ctx context.Context, tx pgx.Tx, userID, trackID uuid.UUID, payload map[string]any) error {
	var rev int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM tracks WHERE id = $1`, trackID).Scan(&rev); err != nil {
		return err
	}
	_, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityTrack, EntityID: trackID, EventType: "track_upserted", Revision: rev, Payload: payload})
	return err
}

// touchPlaylistsOf bumps and announces every playlist that contains a track that is
// about to disappear, so other devices drop the membership.
func touchPlaylistsOf(ctx context.Context, tx pgx.Tx, userID, trackID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		UPDATE playlists SET revision = revision + 1
		WHERE user_id = $1 AND id IN (SELECT playlist_id FROM playlist_items WHERE user_id = $1 AND track_id = $2)
		RETURNING id, revision`, userID, trackID)
	if err != nil {
		return err
	}
	type pr struct {
		id  uuid.UUID
		rev int64
	}
	var touched []pr
	for rows.Next() {
		var p pr
		if err := rows.Scan(&p.id, &p.rev); err != nil {
			rows.Close()
			return err
		}
		touched = append(touched, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range touched {
		if _, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityPlaylist, EntityID: p.id, EventType: "playlist_upserted", Revision: p.rev}); err != nil {
			return err
		}
	}
	return nil
}

func enqueueCleanup(ctx context.Context, d *app.Deps, tx pgx.Tx, args jobs.CleanupObjects, at time.Time) error {
	if len(args.Keys) == 0 && len(args.Uploads) == 0 {
		return nil
	}
	opts := &river.InsertOpts{MaxAttempts: 25}
	if at.After(time.Now()) {
		opts.ScheduledAt = at
	}
	_, err := d.River.InsertTx(ctx, tx, args, opts)
	return err
}

func stagingCleanup(op *Operation) jobs.CleanupObjects {
	var args jobs.CleanupObjects
	if op.StagingKey != nil {
		args.Keys = append(args.Keys, *op.StagingKey)
		if op.MultipartUploadID != nil {
			args.Uploads = append(args.Uploads, jobs.MultipartAbort{Key: *op.StagingKey, UploadID: *op.MultipartUploadID})
		}
	}
	if op.CandidateContentID != nil {
		args.Keys = append(args.Keys, storage.FinalKey(op.UserID.String(), op.CandidateContentID.String()))
	}
	return args
}

// FailInitialUpload undoes a sync in one transaction: the track document, its
// memberships and its operation are deleted, the reservation is released, and a
// track_sync_failed event tells every device to drop the song (R-ID-4).
func FailInitialUpload(ctx context.Context, d *app.Deps, tx pgx.Tx, op *Operation, reason string) error {
	if err := touchPlaylistsOf(ctx, tx, op.UserID, op.TrackID); err != nil {
		return err
	}
	var originDevice *uuid.UUID
	var name string
	err := tx.QueryRow(ctx, `DELETE FROM tracks WHERE user_id = $1 AND id = $2 RETURNING origin_device_id, name`, op.UserID, op.TrackID).Scan(&originDevice, &name)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET storage_reserved_bytes = GREATEST(storage_reserved_bytes - $2, 0) WHERE id = $1`, op.UserID, op.ReservedBytes); err != nil {
		return err
	}
	payload := map[string]any{"reason": reason, "operation_id": op.ID, "request_id": op.RequestID}
	if originDevice != nil {
		payload["origin_device_id"] = *originDevice
	}
	if _, err := events.Append(ctx, tx, events.Event{UserID: op.UserID, EntityType: events.EntityTrack, EntityID: op.TrackID, EventType: "track_sync_failed", Payload: payload}); err != nil {
		return err
	}
	return enqueueCleanup(ctx, d, tx, stagingCleanup(op), time.Time{})
}

// FailReplacement ends a replacement without touching the song: the old content
// stays current and playable (R-ID-4). The slot frees once the operation's last
// upload grant expires (R-SYNC-4).
func FailReplacement(ctx context.Context, d *app.Deps, tx pgx.Tx, op *Operation, state, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE track_sync_operations SET state = $3, failure_reason = $4, finished_at = now()
		WHERE user_id = $1 AND id = $2`, op.UserID, op.ID, state, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET storage_reserved_bytes = GREATEST(storage_reserved_bytes - $2, 0) WHERE id = $1`, op.UserID, op.ReservedBytes); err != nil {
		return err
	}
	if err := trackEvent(ctx, tx, op.UserID, op.TrackID, map[string]any{"replacement_failed": reason, "operation_id": op.ID}); err != nil {
		return err
	}
	if err := enqueueCleanup(ctx, d, tx, stagingCleanup(op), time.Time{}); err != nil {
		return err
	}
	if op.State == "waiting" {
		// A waiting operation never had grants; the next one may start now.
		return PromoteWaiting(ctx, d, tx, op.UserID, op.TrackID)
	}
	return nil
}

// Fail routes an operation failure by kind.
func Fail(ctx context.Context, d *app.Deps, tx pgx.Tx, op *Operation, reason string) error {
	switch op.Kind {
	case "initial_upload":
		return FailInitialUpload(ctx, d, tx, op, reason)
	case "replace":
		state := "failed"
		if reason == ReasonCancelled {
			state = "cancelled"
		}
		return FailReplacement(ctx, d, tx, op, state, reason)
	default: // delete
		_, err := tx.Exec(ctx, `UPDATE track_sync_operations SET state = 'cancelled', failure_reason = $3, finished_at = now() WHERE user_id = $1 AND id = $2`, op.UserID, op.ID, reason)
		if err != nil {
			return err
		}
		if err := trackEvent(ctx, tx, op.UserID, op.TrackID, nil); err != nil {
			return err
		}
		return PromoteWaiting(ctx, d, tx, op.UserID, op.TrackID)
	}
}

// ExecuteDelete deletes a ready track, its memberships and content rows, frees its
// storage, and schedules the bucket cleanup after outstanding download grants
// expire (§7.8, R-SYNC-1).
func ExecuteDelete(ctx context.Context, d *app.Deps, tx pgx.Tx, userID, trackID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT object_key, byte_size, state, last_grant_expires_at FROM track_content WHERE user_id = $1 AND track_id = $2`, userID, trackID)
	if err != nil {
		return err
	}
	var keys []string
	var freed int64
	cleanupAt := time.Now()
	for rows.Next() {
		var key, state string
		var size int64
		var grant *time.Time
		if err := rows.Scan(&key, &size, &state, &grant); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
		if state == "current" {
			freed += size
		}
		if grant != nil && grant.After(cleanupAt) {
			cleanupAt = *grant
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Waiting operations held reservations; release them with the track.
	var waitingReserved int64
	var pendingCleanup []jobs.CleanupObjects
	opRows, err := tx.Query(ctx, `SELECT `+opColumns+` FROM track_sync_operations WHERE user_id = $1 AND track_id = $2 AND state IN ('waiting', 'active')`, userID, trackID)
	if err != nil {
		return err
	}
	for opRows.Next() {
		op, err := scanOp(opRows)
		if err != nil {
			opRows.Close()
			return err
		}
		if op.Kind == "replace" {
			waitingReserved += op.ReservedBytes
			pendingCleanup = append(pendingCleanup, stagingCleanup(op))
		}
	}
	opRows.Close()
	if err := touchPlaylistsOf(ctx, tx, userID, trackID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tracks WHERE user_id = $1 AND id = $2`, userID, trackID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET storage_used_bytes = GREATEST(storage_used_bytes - $2, 0),
		                 storage_reserved_bytes = GREATEST(storage_reserved_bytes - $3, 0)
		WHERE id = $1`, userID, freed, waitingReserved); err != nil {
		return err
	}
	if _, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityTrack, EntityID: trackID, EventType: "track_deleted"}); err != nil {
		return err
	}
	for _, c := range pendingCleanup {
		if err := enqueueCleanup(ctx, d, tx, c, time.Time{}); err != nil {
			return err
		}
	}
	return enqueueCleanup(ctx, d, tx, jobs.CleanupObjects{Keys: keys}, cleanupAt.Add(time.Minute))
}

// PromoteWaiting starts the next queued operation of a track when its slot is free.
// A queued delete runs at once; a queued replacement becomes active and its owner
// device starts uploading (R-SYNC-2, R-SYNC-3).
func PromoteWaiting(ctx context.Context, d *app.Deps, tx pgx.Tx, userID, trackID uuid.UUID) error {
	busy, err := slotBusy(ctx, tx, userID, trackID)
	if err != nil || busy {
		return err
	}
	op, err := scanOp(tx.QueryRow(ctx, `
		SELECT `+opColumns+` FROM track_sync_operations
		WHERE user_id = $1 AND track_id = $2 AND state = 'waiting'
		ORDER BY created_at LIMIT 1 FOR UPDATE`, userID, trackID))
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Recheck the target before starting (R-SYNC-3).
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM tracks WHERE user_id = $1 AND id = $2 FOR UPDATE`, userID, trackID).Scan(&state); err != nil {
		if db.IsNoRows(err) {
			_, err = tx.Exec(ctx, `UPDATE track_sync_operations SET state = 'cancelled', failure_reason = 'target_gone', finished_at = now() WHERE id = $1`, op.ID)
		}
		return err
	}
	switch op.Kind {
	case "delete":
		if _, err := tx.Exec(ctx, `UPDATE track_sync_operations SET state = 'completed', activated_at = now(), finished_at = now() WHERE id = $1`, op.ID); err != nil {
			return err
		}
		return ExecuteDelete(ctx, d, tx, userID, trackID)
	case "replace":
		uploadID, err := d.Store.CreateMultipart(ctx, *op.StagingKey)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE track_sync_operations SET state = 'active', activated_at = now(), last_progress_at = now(), multipart_upload_id = $2
			WHERE id = $1`, op.ID, uploadID); err != nil {
			return err
		}
		return trackEvent(ctx, tx, userID, trackID, nil)
	}
	return nil
}

// Chosen replacement metadata (R-MUT-5).
type chosenMetadata struct {
	Name   string          `json:"name"`
	Artist json.RawMessage `json:"artist"` // ArtistRef or null
}

type baseMetadata struct {
	Name     string     `json:"name"`
	ArtistID *uuid.UUID `json:"artist_id"`
}
