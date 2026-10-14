// Package app bundles the shared dependencies handed to every module.
package app

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/henriquepaulalima/song-sleeve/server/internal/config"
	"github.com/henriquepaulalima/song-sleeve/server/internal/crypt"
	"github.com/henriquepaulalima/song-sleeve/server/internal/events"
	"github.com/henriquepaulalima/song-sleeve/server/internal/ratelimit"
	"github.com/henriquepaulalima/song-sleeve/server/internal/storage"
)

type Deps struct {
	Cfg     *config.Config
	Pool    *pgxpool.Pool
	River   *river.Client[pgx.Tx]
	Store   storage.ObjectStore
	Sealer  *crypt.Sealer
	Limiter *ratelimit.Limiter
	Hub     *events.Hub
}
