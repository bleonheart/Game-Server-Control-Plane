package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/model"
)

type requestIDKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func requestMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 128 {
			buf := make([]byte, 12)
			if _, err := rand.Read(buf); err == nil {
				id = hex.EncodeToString(buf)
			} else {
				id = "unknown"
			}
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		w.Header().Set("X-Request-ID", id)
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))
		logger.Info("http_request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func authMiddleware(service *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		principal, err := service.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or expired session")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func requireRole(role model.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok || !principal.HasRole(role) {
			writeError(w, r, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok || !principal.CanOperate() {
			writeError(w, r, http.StatusForbidden, "forbidden", "operator role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}
