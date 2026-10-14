package operations

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
)

// StallSweep fails uploads that made no progress for the stall timeout (R-ID-4).
// Before failing, it reads the parts already in the bucket: a mobile upload that the
// OS moved to the background still counts while parts keep arriving.
func StallSweep(ctx context.Context, d *app.Deps) error {
	rows, err := d.Pool.Query(ctx, `
		SELECT user_id, id FROM track_sync_operations
		WHERE state = 'active' AND phase = 'uploading' AND kind <> 'delete' AND last_progress_at < $1
		LIMIT 200`, time.Now().Add(-d.Cfg.StallTimeout))
	if err != nil {
		return err
	}
	type ref struct{ user, op uuid.UUID }
	var stalled []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.user, &r.op); err != nil {
			rows.Close()
			return err
		}
		stalled = append(stalled, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range stalled {
		if err := checkStalled(ctx, d, r.user, r.op); err != nil {
			slog.Warn("stall check failed", "operation_id", r.op, "error", err)
		}
	}
	return nil
}

func checkStalled(ctx context.Context, d *app.Deps, userID, opID uuid.UUID) error {
	op, err := LoadOperation(ctx, d.Pool, userID, opID)
	if err != nil {
		return nil
	}
	var bucketBytes int64
	if op.StagingKey != nil && op.MultipartUploadID != nil {
		if parts, err := d.Store.ListParts(ctx, *op.StagingKey, *op.MultipartUploadID); err == nil {
			for _, p := range parts {
				bucketBytes += p.Size
			}
		}
	}
	return db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		locked, err := LockOperation(ctx, tx, userID, opID)
		if err != nil {
			return nil
		}
		if locked.State != "active" || locked.Phase != "uploading" || locked.LastProgressAt == nil ||
			time.Since(*locked.LastProgressAt) < d.Cfg.StallTimeout {
			return nil
		}
		if bucketBytes > locked.ConfirmedBytes {
			_, err := tx.Exec(ctx, `UPDATE track_sync_operations SET confirmed_bytes = $2, last_progress_at = now() WHERE id = $1`, opID, bucketBytes)
			return err
		}
		slog.Info("upload stalled", "operation_id", opID, "kind", locked.Kind)
		return Fail(ctx, d, tx, locked, ReasonStalled)
	})
}

// PromoteSweep starts queued operations whose slot has become free, for example
// after a failed replacement's last upload grant expired (R-SYNC-4).
func PromoteSweep(ctx context.Context, d *app.Deps) error {
	rows, err := d.Pool.Query(ctx, `SELECT DISTINCT user_id, track_id FROM track_sync_operations WHERE state = 'waiting' LIMIT 500`)
	if err != nil {
		return err
	}
	type ref struct{ user, track uuid.UUID }
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.user, &r.track); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, r)
	}
	rows.Close()
	for _, r := range refs {
		err := db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
			// Serialize with request handlers that lock the track row.
			if _, err := tx.Exec(ctx, `SELECT 1 FROM tracks WHERE user_id = $1 AND id = $2 FOR UPDATE`, r.user, r.track); err != nil {
				return err
			}
			return PromoteWaiting(ctx, d, tx, r.user, r.track)
		})
		if err != nil {
			slog.Warn("promote failed", "track_id", r.track, "error", err)
		}
	}
	return nil
}
