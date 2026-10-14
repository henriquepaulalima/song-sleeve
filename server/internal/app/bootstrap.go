package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/henriquepaulalima/song-sleeve/server/internal/config"
	"github.com/henriquepaulalima/song-sleeve/server/internal/crypt"
	"github.com/henriquepaulalima/song-sleeve/server/internal/db"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/ratelimit"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

// Bootstrap loads the config and opens the database and bucket. River is attached
// by the caller (insert-only for the API, full client for the worker).
func Bootstrap(ctx context.Context, migrate bool) (*Deps, error) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, 20)
	if err != nil {
		return nil, err
	}
	if migrate {
		if err := db.Migrate(ctx, pool); err != nil {
			return nil, err
		}
	}
	store, err := storage.NewS3(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("object storage: %w", err)
	}
	sealer, err := crypt.NewSealer(cfg.DataKey)
	if err != nil {
		return nil, err
	}
	return &Deps{
		Cfg: cfg, Pool: pool, Store: store, Sealer: sealer,
		Limiter: ratelimit.New(pool), Hub: events.NewHub(pool),
	}, nil
}
