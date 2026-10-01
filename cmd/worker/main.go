package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := services.Scheduler.Run(ctx, cfg.SchedulerInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("scheduler_failed", "error", err)
			stop()
		}
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if err := services.Servers.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("reconcile_failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := services.Auth.CleanupExpired(ctx); err != nil {
					logger.Error("session_cleanup_failed", "error", err)
				}
			}
		}
	}()
	logger.Info("worker_started")
	wg.Wait()
}
