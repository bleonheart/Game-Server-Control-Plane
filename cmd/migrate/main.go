package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/example/game-server-control-plane/internal/config"
	"github.com/example/game-server-control-plane/internal/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration_failed", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		slog.Error("migration_failed", "error", err)
		os.Exit(1)
	}
}
