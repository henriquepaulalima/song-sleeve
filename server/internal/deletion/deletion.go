// Package deletion implements irreversible account deletion (R-AUTH-5, §7.11) and
// the restricted cleanup-credential endpoints for device purges (R-AUTH-6).
package deletion

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/accounts"
	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/crypt"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

type Handlers struct{ d *app.Deps }

func New(d *app.Deps) *Handlers { return &Handlers{d: d} }

type requestBody struct {
	Proof     string `json:"proof"`
	Confirmed bool   `json:"confirmed"`
}

// Request accepts a deletion. The request and the account gate commit together;
// there is no cancel. Every session is revoked and terminal hints go out.
func (h *Handlers) Request(c *gin.Context) {
	var req requestBody
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if !req.Confirmed {
		httpx.Fail(c, apperr.Field("confirmed", "required"))
		return
	}
	s := httpx.CurrentSession(c)
	ctx := c.Request.Context()
	requestID, _ := uuid.NewV7()
	err := db.InTx(ctx, h.d.Pool, func(tx pgx.Tx) error {
		if err := accounts.ConsumeProof(ctx, tx, s, req.Proof, "delete_account"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET deletion_requested_at = now() WHERE id = $1 AND deletion_requested_at IS NULL`, s.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.New(apperr.AccountDeleted, "This account is already being deleted.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_deletion_requests (id, original_user_id, phase) VALUES ($1, $2, 'accepted')`, requestID, s.UserID); err != nil {
			return err
		}
		// Receipts for this account's devices survive the user row so offline
		// installed apps can confirm the deletion later.
		if _, err := tx.Exec(ctx, `UPDATE device_cleanup_receipts SET deletion_request_id = $2 WHERE original_user_id = $1`, s.UserID, requestID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, s.UserID); err != nil {
			return err
		}
		if err := events.Hint(ctx, tx, s.UserID, "account_deleted", gin.H{}); err != nil {
			return err
		}
		_, err = h.d.River.InsertTx(ctx, tx, jobs.AccountDeletion{RequestID: requestID, UserID: s.UserID}, &river.InsertOpts{MaxAttempts: 100})
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "deletion_id": requestID})
}

// Run wipes everything and deletes the user row last. It is idempotent: a retry
// rebuilds its inventory from what remains.
func Run(ctx context.Context, d *app.Deps, args jobs.AccountDeletion) error {
	setPhase := func(phase string) error {
		_, err := d.Pool.Exec(ctx, `UPDATE account_deletion_requests SET phase = $2 WHERE id = $1 AND phase <> 'completed'`, args.RequestID, phase)
		return err
	}
	if err := setPhase("draining"); err != nil {
		return err
	}
	// Drain media: stop operations so no new grants or publications happen.
	if _, err := d.Pool.Exec(ctx, `
		UPDATE track_sync_operations SET state = 'cancelled', failure_reason = 'account_deleted', finished_at = now()
		WHERE user_id = $1 AND state IN ('waiting', 'active')`, args.UserID); err != nil {
		return err
	}
	prefix := storage.UserPrefix(args.UserID.String())
	if _, err := d.Store.AbortUploadsUnder(ctx, prefix); err != nil {
		return err
	}
	if err := setPhase("wiping"); err != nil {
		return err
	}
	n, err := d.Store.DeletePrefix(ctx, prefix)
	if err != nil {
		return err
	}
	// Re-scan for late writes before declaring the bucket clean.
	if _, err := d.Store.AbortUploadsUnder(ctx, prefix); err != nil {
		return err
	}
	if _, err := d.Store.DeletePrefix(ctx, prefix); err != nil {
		return err
	}
	// Deleting the user row cascades every user-owned table; it happens last.
	if _, err := d.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, args.UserID); err != nil {
		return err
	}
	if _, err := d.Pool.Exec(ctx, `
		UPDATE account_deletion_requests SET phase = 'completed', completed_at = now(),
			checkpoints = jsonb_build_object('objects_deleted', $2::int)
		WHERE id = $1`, args.RequestID, n); err != nil {
		return err
	}
	slog.Info("account deleted", "deletion_id", args.RequestID)
	return nil
}

type statusRequest struct {
	Credential string `json:"credential"`
}

// Status answers the restricted cleanup-credential query an installed app makes
// before trusting anything else about a possibly deleted account (§7.9 step 1).
func (h *Handlers) Status(c *gin.Context) {
	var req statusRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	var deletionID *uuid.UUID
	var phase *string
	err := h.d.Pool.QueryRow(c.Request.Context(), `
		SELECT r.deletion_request_id, d.phase FROM device_cleanup_receipts r
		LEFT JOIN account_deletion_requests d ON d.id = r.deletion_request_id
		WHERE r.credential_hash = $1`, crypt.HashToken(req.Credential)).Scan(&deletionID, &phase)
	if db.IsNoRows(err) {
		httpx.Fail(c, apperr.New(apperr.NotFound, "Unknown credential"))
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deletionID != nil, "phase": phase})
}

// Ack records that a device finished its purge.
func (h *Handlers) Ack(c *gin.Context) {
	var req statusRequest
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	tag, err := h.d.Pool.Exec(c.Request.Context(), `
		UPDATE device_cleanup_receipts SET acknowledged_at = COALESCE(acknowledged_at, now())
		WHERE credential_hash = $1 AND deletion_request_id IS NOT NULL`, crypt.HashToken(req.Credential))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(c, apperr.New(apperr.NotFound, "Unknown credential"))
		return
	}
	c.Status(http.StatusNoContent)
}
