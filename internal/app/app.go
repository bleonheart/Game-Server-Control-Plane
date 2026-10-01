package app

import (
	"context"
	"fmt"

	"github.com/example/game-server-control-plane/internal/audit"
	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/backups"
	"github.com/example/game-server-control-plane/internal/config"
	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/games"
	"github.com/example/game-server-control-plane/internal/games/garrysmod"
	"github.com/example/game-server-control-plane/internal/observability"
	"github.com/example/game-server-control-plane/internal/runtime"
	"github.com/example/game-server-control-plane/internal/scheduler"
	"github.com/example/game-server-control-plane/internal/secrets"
	"github.com/example/game-server-control-plane/internal/servers"
	"github.com/example/game-server-control-plane/internal/steamcmd"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Services struct {
	DB        *pgxpool.Pool
	Auth      *auth.Service
	Servers   *servers.Service
	Backups   *backups.Service
	Scheduler *scheduler.Service
	Audit     *audit.Service
	Metrics   *observability.Metrics
}

func Build(ctx context.Context, cfg config.Config) (*Services, error) {
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	cipher, err := secrets.New(cfg.SecretKey)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("secrets: %w", err)
	}
	metrics := observability.NewMetrics()
	runtimeService := runtime.NewDocker(cfg.DockerBinary)
	steamService := steamcmd.New(cfg.SteamCMDPath)
	gameRegistry := games.NewRegistry(garrysmod.New())
	serverService := servers.NewService(db, runtimeService, steamService, gameRegistry, cipher, metrics, cfg.DataRoot, cfg.DockerDataRoot, cfg.GameImage, cfg.AllowedPortMin, cfg.AllowedPortMax)
	backupService := backups.NewService(db, backups.NewLocalStorage(cfg.BackupRoot), runtimeService)
	return &Services{
		DB:        db,
		Auth:      auth.NewService(db, cfg.SessionTTL),
		Servers:   serverService,
		Backups:   backupService,
		Scheduler: scheduler.NewService(db, serverService, backupService),
		Audit:     audit.New(db),
		Metrics:   metrics,
	}, nil
}
