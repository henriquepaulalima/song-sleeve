package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/catalog"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/media"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

// ErrRetry asks the job runner to retry a transient failure.
var ErrRetry = errors.New("transient failure")

var contentTypes = map[string]string{"mp3": "audio/mpeg", "aac": "audio/mp4", "flac": "audio/flac", "wav": "audio/wav"}

// Publish verifies the actual staging bytes and publishes them to a fresh final key
// that never received a client grant (R-CLOUD-5):
//  1. verify length, SHA-256, duration and codec (R-LIM-1, D-8);
//  2. write a fresh final key; 3. verify the final object again;
//  4. recheck the operation and account fences; 5. switch content in one transaction.
//
// A rejection fails the operation; infrastructure errors return ErrRetry.
func Publish(ctx context.Context, d *app.Deps, userID, opID uuid.UUID) error {
	op, err := LoadOperation(ctx, d.Pool, userID, opID)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Code == apperr.NotFound {
			return nil // the sync was cancelled meanwhile
		}
		return err
	}
	if op.State != "active" || op.Phase != "verifying" {
		return nil
	}

	tmp, err := os.CreateTemp("", "songsleeve-verify-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	hasher := sha256.New()
	n, err := d.Store.Download(ctx, *op.StagingKey, io.MultiWriter(tmp, hasher), min(*op.DeclaredBytes, media.MaxFileBytes))
	if errors.Is(err, storage.ErrTooLarge) {
		return reject(ctx, d, op, ReasonSizeMismatch)
	}
	if err != nil {
		slog.Warn("staging download failed", "operation_id", op.ID, "error", err)
		return ErrRetry
	}
	if n != *op.DeclaredBytes {
		return reject(ctx, d, op, ReasonSizeMismatch)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != *op.DeclaredSHA256 {
		return reject(ctx, d, op, ReasonChecksumMismatch)
	}
	probe, err := media.Verify(ctx, d.Cfg.FFprobePath, tmp.Name())
	if err != nil {
		reason := ReasonInvalidAudio
		if e, ok := apperr.As(err); ok {
			switch e.Code {
			case apperr.UnsupportedFormat:
				reason = ReasonUnsupported
			case apperr.SongTooLong:
				reason = ReasonTooLong
			}
		}
		return reject(ctx, d, op, reason)
	}
	if probe.Format != *op.DeclaredFormat || !media.DurationsMatch(int64(*op.DeclaredDurationMs), probe.DurationMs) {
		return reject(ctx, d, op, ReasonInvalidAudio)
	}

	contentID, _ := uuid.NewV7()
	finalKey := storage.FinalKey(userID.String(), contentID.String())
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Record the candidate first so a failure cleans the final object up too.
	if _, err := d.Pool.Exec(ctx, `UPDATE track_sync_operations SET candidate_content_id = $2 WHERE id = $1`, op.ID, contentID); err != nil {
		return err
	}
	op.CandidateContentID = &contentID
	if err := d.Store.Put(ctx, finalKey, tmp, n, contentTypes[probe.Format]); err != nil {
		slog.Warn("final put failed", "operation_id", op.ID, "error", err)
		return ErrRetry
	}
	if size, err := d.Store.Head(ctx, finalKey); err != nil || size != n {
		slog.Warn("final verification failed", "operation_id", op.ID)
		return ErrRetry
	}

	return db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		locked, err := LockOperation(ctx, tx, userID, opID)
		if err != nil {
			if e, ok := apperr.As(err); ok && e.Code == apperr.NotFound {
				// Cancelled while verifying: the track (and this row) are gone.
				return enqueueCleanup(ctx, d, tx, jobs.CleanupObjects{Keys: []string{finalKey}}, time.Time{})
			}
			return err
		}
		var deleting *time.Time
		if err := tx.QueryRow(ctx, `SELECT deletion_requested_at FROM users WHERE id = $1`, userID).Scan(&deleting); err != nil {
			return err
		}
		if locked.State != "active" || locked.Phase != "verifying" || deleting != nil {
			return enqueueCleanup(ctx, d, tx, jobs.CleanupObjects{Keys: []string{finalKey}}, time.Time{})
		}
		return commitContent(ctx, d, tx, locked, contentID, finalKey, probe, n)
	})
}

// commitContent is R-ID-3 step 5: one transaction sets the current content, makes
// the track ready, applies replacement metadata, moves storage accounting, retires
// old content and appends the event.
func commitContent(ctx context.Context, d *app.Deps, tx pgx.Tx, op *Operation, contentID uuid.UUID, key string, probe *media.Probe, size int64) error {
	var oldID *uuid.UUID
	var oldKey *string
	var oldSize int64
	var oldGrant *time.Time
	err := tx.QueryRow(ctx, `
		SELECT id, object_key, byte_size, last_grant_expires_at FROM track_content
		WHERE user_id = $1 AND track_id = $2 AND state = 'current' FOR UPDATE`, op.UserID, op.TrackID).Scan(&oldID, &oldKey, &oldSize, &oldGrant)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if oldID != nil {
		if _, err := tx.Exec(ctx, `UPDATE track_content SET state = 'retired', retired_at = now() WHERE id = $1`, *oldID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO track_content (id, user_id, track_id, sha256, byte_size, duration_ms, format, object_key, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'current')`,
		contentID, op.UserID, op.TrackID, *op.DeclaredSHA256, size, probe.DurationMs, probe.Format, key); err != nil {
		return err
	}

	name, artistSet, artistID, suggestion, err := replacementMetadata(ctx, tx, op)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tracks SET current_content_id = $3, state = 'ready', pending_since = NULL,
			duration_ms = $4, byte_size = $5, format = $6, sha256 = $7,
			name = COALESCE($8, name),
			artist_id = CASE WHEN $9 THEN $10 ELSE artist_id END,
			metadata_suggestion = COALESCE($11, metadata_suggestion),
			revision = revision + 1
		WHERE user_id = $1 AND id = $2`,
		op.UserID, op.TrackID, contentID, probe.DurationMs, size, probe.Format, *op.DeclaredSHA256,
		name, artistSet, artistID, suggestion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET storage_reserved_bytes = GREATEST(storage_reserved_bytes - $2, 0),
		                 storage_used_bytes = GREATEST(storage_used_bytes + $3 - $4, 0)
		WHERE id = $1`, op.UserID, op.ReservedBytes, size, oldSize); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE track_sync_operations SET state = 'completed', finished_at = now() WHERE id = $1`, op.ID); err != nil {
		return err
	}
	if err := trackEvent(ctx, tx, op.UserID, op.TrackID, map[string]any{"published": op.Kind, "operation_id": op.ID}); err != nil {
		return err
	}
	cleanup := jobs.CleanupObjects{}
	if op.StagingKey != nil {
		cleanup.Keys = append(cleanup.Keys, *op.StagingKey)
	}
	if err := enqueueCleanup(ctx, d, tx, cleanup, time.Time{}); err != nil {
		return err
	}
	if oldKey != nil {
		// Retired content can't start new playback (R-ID-7) but its object lives
		// until every download grant issued for it has expired (R-SYNC-1).
		at := time.Now()
		if oldGrant != nil && oldGrant.After(at) {
			at = *oldGrant
		}
		if err := enqueueCleanup(ctx, d, tx, jobs.CleanupObjects{Keys: []string{*oldKey}}, at.Add(time.Minute)); err != nil {
			return err
		}
	}
	// The upload completed, so its part grants can no longer write: the next
	// queued operation may start.
	return PromoteWaiting(ctx, d, tx, op.UserID, op.TrackID)
}

// replacementMetadata applies the suggestion-step choices (R-MUT-5). A field that
// another device changed during the upload keeps the newer value, and the user's
// choice comes back as a suggestion instead of overwriting it silently.
func replacementMetadata(ctx context.Context, tx pgx.Tx, op *Operation) (name *string, artistSet bool, artistID *uuid.UUID, suggestion []byte, err error) {
	if op.Kind != "replace" || len(op.ChosenMetadata) == 0 {
		return nil, false, nil, nil, nil
	}
	var chosen chosenMetadata
	var base baseMetadata
	if err = json.Unmarshal(op.ChosenMetadata, &chosen); err != nil {
		return
	}
	if err = json.Unmarshal(op.BaseMetadata, &base); err != nil {
		return
	}
	var curName string
	var curArtist *uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT name, artist_id FROM tracks WHERE id = $1`, op.TrackID).Scan(&curName, &curArtist); err != nil {
		return
	}
	var ref *catalog.ArtistRef
	if len(chosen.Artist) > 0 && string(chosen.Artist) != "null" {
		ref = &catalog.ArtistRef{}
		if err = json.Unmarshal(chosen.Artist, ref); err != nil {
			return
		}
	}
	conflicts := map[string]any{}
	if curName == base.Name {
		name = &chosen.Name
	} else if chosen.Name != curName {
		conflicts["name"] = chosen.Name
	}
	artistUnchanged := (curArtist == nil && base.ArtistID == nil) || (curArtist != nil && base.ArtistID != nil && *curArtist == *base.ArtistID)
	if artistUnchanged {
		artistSet = true
		if artistID, err = catalog.ResolveArtist(ctx, tx, op.UserID, ref); err != nil {
			return
		}
	} else if ref != nil {
		conflicts["artist"] = ref
	}
	if len(conflicts) > 0 {
		suggestion, err = json.Marshal(conflicts)
	}
	return
}

func reject(ctx context.Context, d *app.Deps, op *Operation, reason string) error {
	return db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		locked, err := LockOperation(ctx, tx, op.UserID, op.ID)
		if err != nil {
			if e, ok := apperr.As(err); ok && e.Code == apperr.NotFound {
				return nil
			}
			return err
		}
		if locked.State != "active" {
			return nil
		}
		return Fail(ctx, d, tx, locked, reason)
	})
}

// FailOnLastAttempt fails an operation whose verification job ran out of retries.
func FailOnLastAttempt(ctx context.Context, d *app.Deps, userID, opID uuid.UUID) error {
	op, err := LoadOperation(ctx, d.Pool, userID, opID)
	if err != nil {
		return nil
	}
	return reject(ctx, d, op, ReasonUploadError)
}
