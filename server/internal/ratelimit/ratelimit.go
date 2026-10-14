// Package ratelimit implements fixed-window counters in PostgreSQL so every API
// replica shares them (§4.4).
package ratelimit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
}

var (
	Login          = Rule{"login", 10, 15 * time.Minute}
	Register       = Rule{"register", 10, time.Hour}
	ForgotIP       = Rule{"forgot_ip", 10, time.Hour}
	ForgotEmail    = Rule{"forgot_email", 3, time.Hour}
	PasswordChange = Rule{"password_change", 10, 15 * time.Minute}
	Reauth         = Rule{"reauth", 10, 15 * time.Minute}
	ResetSubmit    = Rule{"reset_submit", 20, time.Hour}
	Grants         = Rule{"grants", 600, time.Minute}
)

type Limiter struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Limiter { return &Limiter{pool: pool} }

// Allow counts one hit for key under rule and returns rate_limited past the limit.
func (l *Limiter) Allow(ctx context.Context, rule Rule, key string) error {
	now := time.Now().UTC()
	window := now.Truncate(rule.Window)
	var count int
	err := l.pool.QueryRow(ctx, `
		INSERT INTO rate_limit_buckets (key, window_start, count) VALUES ($1, $2, 1)
		ON CONFLICT (key, window_start) DO UPDATE SET count = rate_limit_buckets.count + 1
		RETURNING count`, rule.Name+":"+key, window).Scan(&count)
	if err != nil {
		return err
	}
	if count > rule.Limit {
		retry := int(window.Add(rule.Window).Sub(now).Seconds()) + 1
		return apperr.WithDetails(apperr.RateLimited, "Too many attempts. Try again later.", map[string]any{"retry_after_seconds": retry})
	}
	return nil
}

// Purge removes expired windows; run periodically by the worker.
func Purge(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `DELETE FROM rate_limit_buckets WHERE window_start < now() - interval '1 day'`)
	return err
}
