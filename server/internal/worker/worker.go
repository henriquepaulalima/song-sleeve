// Package worker runs the River job workers and periodic maintenance (D-2).
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/deletion"
	"github.com/henriquepaulalima/song-sleeve/server/internal/email"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/operations"
	"github.com/henriquepaulalima/song-sleeve/server/internal/ratelimit"
)

// NewInsertOnlyClient is used by the API, which only enqueues jobs.
func NewInsertOnlyClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: slog.Default()})
}

// NewClient builds a River client that runs every worker and periodic job.
func NewClient(d *app.Deps) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &emailWorker{d: d, sender: email.NewSender(d.Cfg)})
	river.AddWorker(workers, &publishWorker{d: d})
	river.AddWorker(workers, &cleanupWorker{d: d})
	river.AddWorker(workers, &deletionWorker{d: d})
	river.AddWorker(workers, &stallWorker{d: d})
	river.AddWorker(workers, &promoteWorker{d: d})
	river.AddWorker(workers, &retentionWorker{d: d})

	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(30*time.Second), func() (river.JobArgs, *river.InsertOpts) {
			return jobs.StallSweep{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Second}}
		}, &river.PeriodicJobOpts{RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(30*time.Second), func() (river.JobArgs, *river.InsertOpts) {
			return jobs.PromoteOperations{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Second}}
		}, &river.PeriodicJobOpts{RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return jobs.Retention{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour}}
		}, &river.PeriodicJobOpts{RunOnStart: true}),
	}

	return river.NewClient(riverpgxv5.New(d.Pool), &river.Config{
		Logger:       slog.Default(),
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 8}},
		Workers:      workers,
		PeriodicJobs: periodic,
	})
}

type emailWorker struct {
	river.WorkerDefaults[jobs.SendEmail]
	d      *app.Deps
	sender email.Sender
}

func (w *emailWorker) Work(ctx context.Context, job *river.Job[jobs.SendEmail]) error {
	a := job.Args
	if time.Now().After(a.ExpiresAt) {
		return nil // retry only until the token expires (§7.12)
	}
	var to, locale string
	var deleting *time.Time
	err := w.d.Pool.QueryRow(ctx, `SELECT email, locale, deletion_requested_at FROM users WHERE id = $1`, a.UserID).Scan(&to, &locale, &deleting)
	if db.IsNoRows(err) || deleting != nil {
		return nil // account deletion cancels pending email
	}
	if err != nil {
		return err
	}
	link := ""
	if a.SealedLink != "" {
		if link, err = w.d.Sealer.Open(a.SealedLink); err != nil {
			return river.JobCancel(err)
		}
	}
	subject, text, ok := email.Render(a.Email, locale, link)
	if !ok {
		return river.JobCancel(errors.New("unknown email kind"))
	}
	return w.sender.Send(ctx, email.Message{To: to, Subject: subject, Text: text, IdempotencyKey: "job-" + itoa(job.ID)})
}

type publishWorker struct {
	river.WorkerDefaults[jobs.VerifyPublish]
	d *app.Deps
}

func (w *publishWorker) Timeout(*river.Job[jobs.VerifyPublish]) time.Duration {
	return 10 * time.Minute
}

func (w *publishWorker) Work(ctx context.Context, job *river.Job[jobs.VerifyPublish]) error {
	err := operations.Publish(ctx, w.d, job.Args.UserID, job.Args.OperationID)
	if err != nil && job.Attempt >= job.MaxAttempts {
		// Out of retries: an error that can't be retried fails the sync (R-ID-4).
		return operations.FailOnLastAttempt(ctx, w.d, job.Args.UserID, job.Args.OperationID)
	}
	return err
}

type cleanupWorker struct {
	river.WorkerDefaults[jobs.CleanupObjects]
	d *app.Deps
}

func (w *cleanupWorker) Work(ctx context.Context, job *river.Job[jobs.CleanupObjects]) error {
	for _, u := range job.Args.Uploads {
		if err := w.d.Store.AbortMultipart(ctx, u.Key, u.UploadID); err != nil {
			return err
		}
	}
	for _, k := range job.Args.Keys {
		if err := w.d.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

type deletionWorker struct {
	river.WorkerDefaults[jobs.AccountDeletion]
	d *app.Deps
}

func (w *deletionWorker) Timeout(*river.Job[jobs.AccountDeletion]) time.Duration {
	return 30 * time.Minute
}

func (w *deletionWorker) Work(ctx context.Context, job *river.Job[jobs.AccountDeletion]) error {
	return deletion.Run(ctx, w.d, job.Args)
}

type stallWorker struct {
	river.WorkerDefaults[jobs.StallSweep]
	d *app.Deps
}

func (w *stallWorker) Work(ctx context.Context, _ *river.Job[jobs.StallSweep]) error {
	return operations.StallSweep(ctx, w.d)
}

type promoteWorker struct {
	river.WorkerDefaults[jobs.PromoteOperations]
	d *app.Deps
}

func (w *promoteWorker) Work(ctx context.Context, _ *river.Job[jobs.PromoteOperations]) error {
	return operations.PromoteSweep(ctx, w.d)
}

type retentionWorker struct {
	river.WorkerDefaults[jobs.Retention]
	d *app.Deps
}

// Work applies the retention rules: events and receipts after 90 days (D-15,
// R-SYNC-5), finished operations, used tokens and old rate-limit windows.
func (w *retentionWorker) Work(ctx context.Context, _ *river.Job[jobs.Retention]) error {
	cutoff := time.Now().Add(-w.d.Cfg.EventRetention)
	// Remember, per user, the newest purged cursor so older cursors get cursor_expired.
	if _, err := w.d.Pool.Exec(ctx, `
		WITH purged AS (
			DELETE FROM account_events WHERE created_at < $1 RETURNING user_id, cursor
		), maxes AS (SELECT user_id, max(cursor) AS c FROM purged GROUP BY user_id)
		UPDATE users u SET events_purged_through = GREATEST(u.events_purged_through, maxes.c)
		FROM maxes WHERE u.id = maxes.user_id`, cutoff); err != nil {
		return err
	}
	stmts := []string{
		`DELETE FROM mutation_receipts WHERE created_at < now() - make_interval(secs => $1)`,
		`DELETE FROM track_sync_operations WHERE state IN ('completed', 'failed', 'cancelled') AND finished_at < now() - interval '1 day' AND (grant_expires_at IS NULL OR grant_expires_at < now())`,
		`DELETE FROM password_reset_tokens WHERE expires_at < now() - interval '1 day'`,
		`DELETE FROM reauthentication_proofs WHERE expires_at < now() - interval '1 day'`,
		`DELETE FROM user_sessions WHERE expires_at < now() - interval '7 days'`,
	}
	for i, s := range stmts {
		var err error
		if i == 0 {
			_, err = w.d.Pool.Exec(ctx, s, w.d.Cfg.ReceiptRetention.Seconds())
		} else {
			_, err = w.d.Pool.Exec(ctx, s)
		}
		if err != nil {
			return err
		}
	}
	return ratelimit.Purge(ctx, w.d.Pool)
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}
