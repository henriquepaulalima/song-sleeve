// Command admin is the operator CLI (§10.5), run through the Railway shell against
// one environment. Every command writes an admin_actions row; it never prints
// tokens or song content.
//
//	admin usage <email>
//	admin set-quota <email> <bytes>
//	admin cloud-sync <email> on|off
//	admin suspend <email> | unsuspend <email>
//	admin delete-account <email>
//	admin stalled
//	admin migrate
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/config"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/fields"
	"github.com/henriquepaulalima/song-sleeve/server/internal/jobs"
	"github.com/henriquepaulalima/song-sleeve/server/internal/worker"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New(`usage:
  admin usage <email>
  admin set-quota <email> <bytes>
  admin cloud-sync <email> on|off
  admin suspend <email>
  admin unsuspend <email>
  admin delete-account <email>
  admin stalled
  admin migrate`)
}

func operator() string {
	if v := os.Getenv("ADMIN_OPERATOR"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		return err
	}
	defer pool.Close()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		fmt.Println("migrations applied")
		return nil
	case "stalled":
		return stalled(ctx, pool, cfg)
	}
	if len(rest) < 1 {
		return usage()
	}
	userID, err := lookup(ctx, pool, rest[0])
	if err != nil {
		return err
	}
	switch cmd {
	case "usage":
		return showUsage(ctx, pool, userID)
	case "set-quota":
		if len(rest) != 2 {
			return usage()
		}
		n, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil || n < 0 {
			return errors.New("quota must be a byte count")
		}
		return act(ctx, pool, userID, "set_quota", map[string]any{"bytes": n}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE users SET storage_quota_bytes = $2 WHERE id = $1`, userID, n)
			return err
		})
	case "cloud-sync":
		if len(rest) != 2 || (rest[1] != "on" && rest[1] != "off") {
			return usage()
		}
		on := rest[1] == "on"
		return act(ctx, pool, userID, "cloud_sync", map[string]any{"enabled": on}, func(tx pgx.Tx) error {
			var rev int64
			if err := tx.QueryRow(ctx, `UPDATE users SET cloud_sync_enabled = $2, revision = revision + 1 WHERE id = $1 RETURNING revision`, userID, on).Scan(&rev); err != nil {
				return err
			}
			_, err := events.Append(ctx, tx, events.Event{UserID: userID, EntityType: events.EntityAccount, EntityID: userID, EventType: "account_changed", Revision: rev})
			return err
		})
	case "suspend":
		// Suspension revokes every session and blocks login, writes and grants.
		return act(ctx, pool, userID, "suspend", nil, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE users SET suspended_at = now() WHERE id = $1`, userID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
			return err
		})
	case "unsuspend":
		return act(ctx, pool, userID, "unsuspend", nil, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE users SET suspended_at = NULL WHERE id = $1`, userID)
			return err
		})
	case "delete-account":
		// The same flow as §7.11, without reauthentication.
		rc, err := worker.NewInsertOnlyClient(pool)
		if err != nil {
			return err
		}
		requestID, _ := uuid.NewV7()
		return act(ctx, pool, userID, "delete_account", map[string]any{"deletion_id": requestID}, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE users SET deletion_requested_at = now() WHERE id = $1 AND deletion_requested_at IS NULL`, userID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return errors.New("account is already being deleted")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO account_deletion_requests (id, original_user_id, phase) VALUES ($1, $2, 'accepted')`, requestID, userID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE device_cleanup_receipts SET deletion_request_id = $2 WHERE original_user_id = $1`, userID, requestID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
				return err
			}
			if err := events.Hint(ctx, tx, userID, "account_deleted", map[string]any{}); err != nil {
				return err
			}
			_, err = rc.InsertTx(ctx, tx, jobs.AccountDeletion{RequestID: requestID, UserID: userID}, &river.InsertOpts{MaxAttempts: 100})
			return err
		})
	}
	return usage()
}

func lookup(ctx context.Context, pool *pgxpool.Pool, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, fields.NormalizeEmail(email)).Scan(&id)
	if db.IsNoRows(err) {
		return id, errors.New("no account with that email")
	}
	return id, err
}

func act(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, action string, args map[string]any, fn func(pgx.Tx) error) error {
	if args == nil {
		args = map[string]any{}
	}
	raw, _ := json.Marshal(args)
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO admin_actions (operator, action, user_id, arguments) VALUES ($1, $2, $3, $4)`, operator(), action, userID, raw)
		return err
	})
	if err == nil {
		fmt.Printf("%s: done\n", action)
	}
	return err
}

func showUsage(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	var used, reserved, quota int64
	var cloud bool
	var suspended, deleting *time.Time
	var songs, pending int64
	err := pool.QueryRow(ctx, `
		SELECT storage_used_bytes, storage_reserved_bytes, storage_quota_bytes, cloud_sync_enabled, suspended_at, deletion_requested_at,
		       (SELECT count(*) FROM tracks WHERE user_id = u.id AND state = 'ready'),
		       (SELECT count(*) FROM tracks WHERE user_id = u.id AND state = 'pending_upload')
		FROM users u WHERE id = $1`, userID).Scan(&used, &reserved, &quota, &cloud, &suspended, &deleting, &songs, &pending)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintf(w, "account\t%s\n", userID)
	fmt.Fprintf(w, "cloud sync\t%v\n", cloud)
	fmt.Fprintf(w, "synced songs\t%d (%d syncing)\n", songs, pending)
	fmt.Fprintf(w, "storage used\t%d bytes\n", used)
	fmt.Fprintf(w, "storage reserved\t%d bytes\n", reserved)
	fmt.Fprintf(w, "quota\t%d bytes\n", quota)
	fmt.Fprintf(w, "suspended\t%v\n", suspended != nil)
	fmt.Fprintf(w, "being deleted\t%v\n", deleting != nil)
	return w.Flush()
}

func stalled(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, kind, state, phase, COALESCE(last_progress_at, created_at)
		FROM track_sync_operations
		WHERE (state = 'active' AND COALESCE(last_progress_at, created_at) < $1) OR (state = 'waiting' AND created_at < now() - interval '1 hour')
		ORDER BY created_at LIMIT 100`, time.Now().Add(-cfg.StallTimeout))
	if err != nil {
		return err
	}
	defer rows.Close()
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "OPERATION\tACCOUNT\tKIND\tSTATE\tPHASE\tLAST PROGRESS")
	for rows.Next() {
		var id, user uuid.UUID
		var kind, state, phase string
		var last time.Time
		if err := rows.Scan(&id, &user, &kind, &state, &phase, &last); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", id, user, kind, state, phase, last.Format(time.RFC3339))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	var failed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE state = 'discarded' AND kind IN ('cleanup_objects', 'account_deletion')`).Scan(&failed); err != nil {
		return err
	}
	fmt.Printf("\nfailed cleanup/deletion jobs: %d\n", failed)
	return rows.Err()
}
