package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/camradeling/migration_queue/backend/internal/api"
	"github.com/camradeling/migration_queue/backend/internal/config"
	"github.com/camradeling/migration_queue/backend/internal/db"
	"github.com/camradeling/migration_queue/backend/internal/queue"
	"github.com/camradeling/migration_queue/backend/internal/sms"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	dbx, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer dbx.Close()

	if err := db.Migrate(dbx, cfg.MigrationsPath); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	if err := db.SeedAdmin(dbx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("admin seed failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var sender sms.Sender = sms.ConsoleSender{}
	worker := &sms.Worker{DB: dbx, Sender: sender}
	go worker.Run(ctx)

	qsvc := queue.New(dbx)
	router := api.NewRouter(cfg, dbx, qsvc)

	slog.Info("starting server", "port", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
