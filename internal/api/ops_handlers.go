package api

import (
	"errors"
	"net/http"

	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/backups"
	"github.com/example/game-server-control-plane/internal/servers"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) listBackups(w http.ResponseWriter, r *http.Request) {
	items, err := h.backups.List(r.Context(), r.PathValue("id"), queryInt(r, "limit", 50))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) createBackup(w http.ResponseWriter, r *http.Request) {
	backup, err := h.backups.Create(r.Context(), r.PathValue("id"))
	principal, _ := auth.PrincipalFromContext(r.Context())
	result := "success"
	metadata := map[string]any{}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "backup.create", "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "server not found")
			return
		}
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, backup)
}

func (h *Handler) validateBackup(w http.ResponseWriter, r *http.Request) {
	err := h.backups.Validate(r.Context(), r.PathValue("id"), r.PathValue("backupID"))
	principal, _ := auth.PrincipalFromContext(r.Context())
	result := "success"
	metadata := map[string]any{"backup_id": r.PathValue("backupID")}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "backup.validate", "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		if errors.Is(err, backups.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "backup not found")
			return
		}
		if errors.Is(err, backups.ErrChecksumMismatch) {
			writeError(w, r, http.StatusUnprocessableEntity, "backup_invalid", "backup checksum mismatch")
			return
		}
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "valid"})
}

func (h *Handler) restoreBackup(w http.ResponseWriter, r *http.Request) {
	err := h.backups.Restore(r.Context(), r.PathValue("id"), r.PathValue("backupID"))
	principal, _ := auth.PrincipalFromContext(r.Context())
	result := "success"
	metadata := map[string]any{"backup_id": r.PathValue("backupID")}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "backup.restore", "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, backups.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "backup not found")
			return
		}
		if err.Error() == "server must be stopped before restore" {
			writeError(w, r, http.StatusConflict, "state_conflict", err.Error())
			return
		}
		if errors.Is(err, backups.ErrChecksumMismatch) {
			writeError(w, r, http.StatusUnprocessableEntity, "backup_invalid", "backup checksum mismatch")
			return
		}
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

type createScheduleRequest struct {
	Kind            string `json:"kind"`
	IntervalSeconds int    `json:"interval_seconds"`
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var input createScheduleRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := h.scheduler.Create(r.Context(), r.PathValue("id"), input.Kind, input.IntervalSeconds)
	principal, _ := auth.PrincipalFromContext(r.Context())
	result := "success"
	metadata := map[string]any{"kind": input.Kind, "interval_seconds": input.IntervalSeconds}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	} else {
		metadata["schedule_id"] = item.ID
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "schedule.create", "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "server not found")
			return
		}
		if err.Error() == "invalid schedule kind" || err.Error() == "minimum interval is 60 seconds" {
			writeError(w, r, http.StatusBadRequest, "invalid_schedule", err.Error())
			return
		}
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := h.scheduler.List(r.Context(), r.PathValue("id"))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	err := h.scheduler.Delete(r.Context(), r.PathValue("id"), r.PathValue("scheduleID"))
	principal, _ := auth.PrincipalFromContext(r.Context())
	result := "success"
	metadata := map[string]any{"schedule_id": r.PathValue("scheduleID")}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "schedule.delete", "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "schedule not found")
			return
		}
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	items, err := h.audit.List(r.Context(), queryInt(r, "limit", 50), queryInt(r, "offset", 0))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) prometheus(w http.ResponseWriter, r *http.Request) {
	if !h.metricsEnabled {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	h.metrics.WritePrometheus(w)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("request_failed", "request_id", requestID(r.Context()), "error", err)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
}
