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

	"github.com/example/game-server-control-plane/internal/api"
	"github.com/example/game-server-control-plane/internal/app"
	"github.com/example/game-server-control-plane/internal/config"
	"github.com/example/game-server-control-plane/internal/observability"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration_failed", "error", err)
		os.Exit(1)
	}
	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	services, err := app.Build(ctx, cfg)
	if err != nil {
		logger.Error("startup_failed", "error", err)
		os.Exit(1)
	}
	defer services.DB.Close()
	if cfg.DevSeed {
		if err := services.Auth.SeedAdmin(ctx, cfg.DevAdminEmail, cfg.DevAdminPassword); err != nil {
			logger.Error("seed_admin_failed", "error", err)
			os.Exit(1)
		}
		if err := services.Servers.SeedExample(ctx, cfg.DevAdminPassword); err != nil {
			logger.Error("seed_server_failed", "error", err)
			os.Exit(1)
		}
	}
	handler := api.NewRouter(services.Auth, services.Servers, services.Backups, services.Scheduler, services.Audit, services.Metrics, cfg.MetricsEnabled, services.DB, logger)
	server := &http.Server{Addr: cfg.BindAddress, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("api_started", "address", cfg.BindAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("api_failed", "error", err)
		os.Exit(1)
	}
}
