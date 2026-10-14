// Package mutations implements the request contract for server-data changes
// (R-MUT-1…4): an Idempotency-Key per request, one transaction for the change, its
// receipt and its account event, replay of the original result for a repeated key,
// and status recovery through GET /me/mutations/{requestID}.
package mutations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/httpx"
)

// Result is what a mutation returns on success.
type Result struct {
	Status      int
	Body        any
	OperationID *uuid.UUID
}

func OK(body any) Result { return Result{Status: http.StatusOK, Body: body} }

type Func func(ctx context.Context, tx pgx.Tx, s *httpx.Session) (Result, error)

// Run executes fn under the mutation contract and writes the response.
func Run(c *gin.Context, d *app.Deps, fn Func) {
	s := httpx.CurrentSession(c)
	key := c.GetHeader("Idempotency-Key")
	if key == "" || len(key) > 100 {
		httpx.Fail(c, apperr.New(apperr.IdempotencyKeyRequired, "Idempotency-Key header is required"))
		return
	}
	ctx := c.Request.Context()
	if replayed := replay(c, d, s.UserID, key); replayed {
		return
	}

	var res Result
	err := db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		var err error
		res, err = fn(ctx, tx, s)
		if err != nil {
			return err
		}
		body, err := json.Marshal(res.Body)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO mutation_receipts (user_id, request_id, status, http_status, result, operation_id)
			VALUES ($1, $2, 'committed', $3, $4, $5)`, s.UserID, key, res.Status, body, res.OperationID)
		return err
	})
	switch {
	case err == nil:
		c.JSON(res.Status, res.Body)
	case db.IsUniqueViolation(err, "mutation_receipts_pkey"):
		// A concurrent request with the same key committed first; ours rolled back.
		if !replay(c, d, s.UserID, key) {
			httpx.Fail(c, errors.New("receipt vanished"))
		}
	default:
		if e, ok := apperr.As(err); ok {
			// A rejection is the original result too: store it so a retry with the
			// same key returns it instead of trying again.
			body, _ := json.Marshal(apperr.Envelope{Error: e})
			_, _ = d.Pool.Exec(ctx, `
				INSERT INTO mutation_receipts (user_id, request_id, status, http_status, result)
				VALUES ($1, $2, 'rejected', $3, $4) ON CONFLICT DO NOTHING`, s.UserID, key, e.Status(), body)
		}
		httpx.Fail(c, err)
	}
}

func replay(c *gin.Context, d *app.Deps, userID uuid.UUID, key string) bool {
	var status int
	var body []byte
	err := d.Pool.QueryRow(c.Request.Context(),
		`SELECT http_status, result FROM mutation_receipts WHERE user_id = $1 AND request_id = $2`, userID, key).Scan(&status, &body)
	if err != nil {
		return false
	}
	c.Header("Idempotent-Replay", "true")
	c.Data(status, "application/json; charset=utf-8", body)
	c.Abort()
	return true
}

// StatusHandler serves GET /me/mutations/{requestID} (R-MUT-4).
func StatusHandler(d *app.Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := httpx.CurrentSession(c)
		var status string
		var httpStatus int
		var result json.RawMessage
		var opID *uuid.UUID
		err := d.Pool.QueryRow(c.Request.Context(), `
			SELECT status, http_status, result, operation_id FROM mutation_receipts
			WHERE user_id = $1 AND request_id = $2`, s.UserID, c.Param("requestID")).Scan(&status, &httpStatus, &result, &opID)
		if db.IsNoRows(err) {
			// The server never committed this request: it was not applied.
			c.JSON(http.StatusOK, gin.H{"status": "not_applied"})
			return
		}
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": status, "http_status": httpStatus, "result": result, "operation_id": opID})
	}
}
