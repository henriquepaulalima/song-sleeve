// Command api runs the Song Sleeve HTTP API. With -with-worker it also runs the
// job workers in the same process, which is convenient for local development.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/henriquepaulalima/song-sleeve/server/internal/app"
	"github.com/henriquepaulalima/song-sleeve/server/internal/server"
	"github.com/henriquepaulalima/song-sleeve/server/internal/worker"
)

func main() {
	withWorker := flag.Bool("with-worker", false, "also run the job workers in this process")
	migrate := flag.Bool("migrate", false, "apply database migrations at startup")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := app.Bootstrap(ctx, *migrate)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer d.Pool.Close()

	if *withWorker {
		d.River, err = worker.NewClient(d)
		if err == nil {
			err = d.River.Start(ctx)
		}
	} else {
		d.River, err = worker.NewInsertOnlyClient(d.Pool)
	}
	if err != nil {
		slog.Error("river failed", "error", err)
		os.Exit(1)
	}

	go d.Hub.Run(ctx)

	srv := &http.Server{
		Addr:              "0.0.0.0:" + d.Cfg.Port,
		Handler:           server.New(d),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("api listening", "addr", srv.Addr, "env", d.Cfg.Env, "worker", *withWorker)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	if *withWorker {
		_ = d.River.Stop(shutdown)
	}
}
