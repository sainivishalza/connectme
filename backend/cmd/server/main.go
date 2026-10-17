package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectme/internal/server"
	"connectme/migrations"
	"connectme/pkg/config"
	"connectme/pkg/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pg, err := database.NewPostgres(ctx, cfg.PostgresURL)
	if err != nil {
		return err
	}
	defer pg.Close()
	if err := database.Migrate(ctx, pg, migrations.FS); err != nil {
		return err
	}
	rdb, err := database.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()
	mg, err := database.NewMongo(ctx, cfg.MongoURL)
	if err != nil {
		return err
	}
	defer mg.Close(context.Background())

	server.StartJobs(ctx, server.Deps{Cfg: cfg, PG: pg, Redis: rdb, Mongo: mg})
	if cfg.MetricsAddr != "" {
		server.ServeMetrics(ctx, cfg.MetricsAddr)
	}
	srv := server.New(server.Deps{Cfg: cfg, PG: pg, Redis: rdb, Mongo: mg})
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("listening", "addr", srv.Addr, "env", cfg.Env)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
