// Command worker runs the River job workers: email, upload verification and
// publication, cleanup, account deletion and periodic sweeps (§4.1).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := app.Bootstrap(ctx, false)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer d.Pool.Close()

	d.River, err = worker.NewClient(d)
	if err != nil {
		slog.Error("river failed", "error", err)
		os.Exit(1)
	}
	if err := d.River.Start(ctx); err != nil {
		slog.Error("river start failed", "error", err)
		os.Exit(1)
	}
	slog.Info("worker running")
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = d.River.Stop(shutdown)
}
