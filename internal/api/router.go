package api

import (
	"log/slog"
	"net/http"

	"github.com/example/game-server-control-plane/internal/audit"
	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/backups"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/example/game-server-control-plane/internal/observability"
	"github.com/example/game-server-control-plane/internal/scheduler"
	"github.com/example/game-server-control-plane/internal/servers"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	auth           *auth.Service
	servers        *servers.Service
	backups        *backups.Service
	scheduler      *scheduler.Service
	audit          *audit.Service
	metrics        *observability.Metrics
	metricsEnabled bool
	db             *pgxpool.Pool
	logger         *slog.Logger
}

func NewRouter(authService *auth.Service, serverService *servers.Service, backupService *backups.Service, schedulerService *scheduler.Service, auditService *audit.Service, metrics *observability.Metrics, metricsEnabled bool, db *pgxpool.Pool, logger *slog.Logger) http.Handler {
	h := &Handler{auth: authService, servers: serverService, backups: backupService, scheduler: schedulerService, audit: auditService, metrics: metrics, metricsEnabled: metricsEnabled, db: db, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/auth/logout", authMiddleware(authService, http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/v1/auth/me", authMiddleware(authService, http.HandlerFunc(h.me)))
	mux.Handle("GET /api/v1/users", authMiddleware(authService, requireRole(model.RoleAdmin, http.HandlerFunc(h.listUsers))))
	mux.Handle("POST /api/v1/users", authMiddleware(authService, requireRole(model.RoleAdmin, http.HandlerFunc(h.createUser))))
	mux.Handle("PUT /api/v1/users/{id}/roles", authMiddleware(authService, requireRole(model.RoleAdmin, http.HandlerFunc(h.setUserRoles))))
	mux.Handle("GET /api/v1/servers", authMiddleware(authService, http.HandlerFunc(h.listServers)))
	mux.Handle("POST /api/v1/servers", authMiddleware(authService, requireOperator(http.HandlerFunc(h.createServer))))
	mux.Handle("GET /api/v1/servers/{id}", authMiddleware(authService, http.HandlerFunc(h.getServer)))
	mux.Handle("GET /api/v1/servers/{id}/status", authMiddleware(authService, http.HandlerFunc(h.serverStatus)))
	mux.Handle("DELETE /api/v1/servers/{id}", authMiddleware(authService, requireRole(model.RoleAdmin, http.HandlerFunc(h.deleteServer))))
	mux.Handle("POST /api/v1/servers/{id}/start", authMiddleware(authService, requireOperator(http.HandlerFunc(h.startServer))))
	mux.Handle("POST /api/v1/servers/{id}/stop", authMiddleware(authService, requireOperator(http.HandlerFunc(h.stopServer))))
	mux.Handle("POST /api/v1/servers/{id}/force-stop", authMiddleware(authService, requireOperator(http.HandlerFunc(h.forceStopServer))))
	mux.Handle("POST /api/v1/servers/{id}/restart", authMiddleware(authService, requireOperator(http.HandlerFunc(h.restartServer))))
	mux.Handle("POST /api/v1/servers/{id}/update", authMiddleware(authService, requireOperator(http.HandlerFunc(h.updateServer))))
	mux.Handle("POST /api/v1/servers/{id}/validate", authMiddleware(authService, requireOperator(http.HandlerFunc(h.validateServer))))
	mux.Handle("GET /api/v1/servers/{id}/updates", authMiddleware(authService, http.HandlerFunc(h.updateHistory)))
	mux.Handle("GET /api/v1/servers/{id}/logs", authMiddleware(authService, http.HandlerFunc(h.serverLogs)))
	mux.Handle("GET /api/v1/servers/{id}/backups", authMiddleware(authService, http.HandlerFunc(h.listBackups)))
	mux.Handle("POST /api/v1/servers/{id}/backups", authMiddleware(authService, requireOperator(http.HandlerFunc(h.createBackup))))
	mux.Handle("POST /api/v1/servers/{id}/backups/{backupID}/validate", authMiddleware(authService, requireOperator(http.HandlerFunc(h.validateBackup))))
	mux.Handle("POST /api/v1/servers/{id}/backups/{backupID}/restore", authMiddleware(authService, requireOperator(http.HandlerFunc(h.restoreBackup))))
	mux.Handle("GET /api/v1/servers/{id}/schedules", authMiddleware(authService, http.HandlerFunc(h.listSchedules)))
	mux.Handle("POST /api/v1/servers/{id}/schedules", authMiddleware(authService, requireOperator(http.HandlerFunc(h.createSchedule))))
	mux.Handle("DELETE /api/v1/servers/{id}/schedules/{scheduleID}", authMiddleware(authService, requireOperator(http.HandlerFunc(h.deleteSchedule))))
	mux.Handle("GET /api/v1/audit", authMiddleware(authService, requireRole(model.RoleAdmin, http.HandlerFunc(h.listAudit))))
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /ready", h.ready)
	mux.HandleFunc("GET /metrics", h.prometheus)
	return requestMiddleware(logger, mux)
}
