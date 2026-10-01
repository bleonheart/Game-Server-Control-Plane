package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/example/game-server-control-plane/internal/servers"
)

type createServerRequest struct {
	Name                 string             `json:"name"`
	Game                 string             `json:"game"`
	Map                  string             `json:"map"`
	Gamemode             string             `json:"gamemode"`
	MaxPlayers           int                `json:"max_players"`
	Hostname             string             `json:"hostname"`
	WorkshopCollection   string             `json:"workshop_collection"`
	RCONPassword         string             `json:"rcon_password"`
	ServerPassword       string             `json:"server_password"`
	Environment          map[string]string  `json:"environment"`
	ExtraArgs            []string           `json:"extra_args"`
	CPUQuota             int64              `json:"cpu_quota"`
	MemoryBytes          int64              `json:"memory_bytes"`
	AutoRecover          *bool              `json:"auto_recover"`
	MaxRestartAttempts   int                `json:"max_restart_attempts"`
	BackupRetentionCount int                `json:"backup_retention_count"`
	Ports                []model.ServerPort `json:"ports"`
}

func (h *Handler) createServer(w http.ResponseWriter, r *http.Request) {
	var input createServerRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	autoRecover := true
	if input.AutoRecover != nil {
		autoRecover = *input.AutoRecover
	}
	server, err := h.servers.Create(r.Context(), servers.CreateInput{
		Name: input.Name, Game: input.Game, Map: input.Map, Gamemode: input.Gamemode, MaxPlayers: input.MaxPlayers,
		Hostname: input.Hostname, WorkshopCollection: input.WorkshopCollection, RCONPassword: input.RCONPassword,
		ServerPassword: input.ServerPassword, Environment: input.Environment, ExtraArgs: input.ExtraArgs,
		CPUQuota: input.CPUQuota, MemoryBytes: input.MemoryBytes, AutoRecover: autoRecover,
		MaxRestartAttempts: input.MaxRestartAttempts, BackupRetentionCount: input.BackupRetentionCount, Ports: input.Ports,
	})
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err != nil {
		_ = h.audit.Record(r.Context(), principal.UserID, "server.create", "server:"+input.Name, "failed", requestID(r.Context()), map[string]any{"error": err.Error()})
		h.publicServiceError(w, r, err)
		return
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "server.create", "server:"+server.ID, "success", requestID(r.Context()), nil)
	writeJSON(w, http.StatusCreated, server)
}

func (h *Handler) listServers(w http.ResponseWriter, r *http.Request) {
	items, err := h.servers.List(r.Context(), queryInt(r, "limit", 50), queryInt(r, "offset", 0), r.URL.Query().Get("state"))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) getServer(w http.ResponseWriter, r *http.Request) {
	server, ports, err := h.servers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.publicServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": server, "ports": ports})
}

func (h *Handler) serverStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.servers.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		h.publicServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	h.serverAction(w, r, "server.delete", func() error { return h.servers.Delete(r.Context(), r.PathValue("id")) })
}

func (h *Handler) startServer(w http.ResponseWriter, r *http.Request) {
	h.serverAction(w, r, "server.start", func() error { return h.servers.Start(r.Context(), r.PathValue("id")) })
}

func (h *Handler) stopServer(w http.ResponseWriter, r *http.Request) {
	h.serverAction(w, r, "server.stop", func() error { return h.servers.Stop(r.Context(), r.PathValue("id"), false) })
}

func (h *Handler) forceStopServer(w http.ResponseWriter, r *http.Request) {
	h.serverAction(w, r, "server.force_stop", func() error { return h.servers.Stop(r.Context(), r.PathValue("id"), true) })
}

func (h *Handler) restartServer(w http.ResponseWriter, r *http.Request) {
	h.serverAction(w, r, "server.restart", func() error { return h.servers.Restart(r.Context(), r.PathValue("id")) })
}

type updateRequest struct {
	Validate bool `json:"validate"`
}

func (h *Handler) updateServer(w http.ResponseWriter, r *http.Request) {
	var input updateRequest
	if r.ContentLength > 0 && !decodeJSON(w, r, &input) {
		return
	}
	h.serverAction(w, r, "server.update", func() error { return h.servers.Update(r.Context(), r.PathValue("id"), input.Validate) })
}

func (h *Handler) validateServer(w http.ResponseWriter, r *http.Request) {
	output, err := h.servers.ValidateInstallation(r.Context(), r.PathValue("id"))
	if err != nil {
		h.publicServiceError(w, r, err)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	_ = h.audit.Record(r.Context(), principal.UserID, "server.validate", "server:"+r.PathValue("id"), "success", requestID(r.Context()), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "valid", "output": output})
}

func (h *Handler) serverAction(w http.ResponseWriter, r *http.Request, action string, run func() error) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	err := run()
	result := "success"
	metadata := map[string]any{}
	if err != nil {
		result = "failed"
		metadata["error"] = err.Error()
	}
	_ = h.audit.Record(r.Context(), principal.UserID, action, "server:"+r.PathValue("id"), result, requestID(r.Context()), metadata)
	if err != nil {
		h.publicServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) updateHistory(w http.ResponseWriter, r *http.Request) {
	items, err := h.servers.UpdateHistory(r.Context(), r.PathValue("id"), queryInt(r, "limit", 50))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) serverLogs(w http.ResponseWriter, r *http.Request) {
	tail := queryInt(r, "tail", 200)
	follow, _ := strconv.ParseBool(r.URL.Query().Get("follow"))
	reader, err := h.servers.Logs(r.Context(), r.PathValue("id"), tail, follow)
	if err != nil {
		h.publicServiceError(w, r, err)
		return
	}
	defer reader.Close()
	if !follow {
		body, err := io.ReadAll(io.LimitReader(reader, 4<<20))
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"logs": string(body)})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming_unavailable", "streaming unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		payload, _ := json.Marshal(map[string]string{"line": scanner.Text()})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
	}
}

func (h *Handler) publicServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, servers.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "server not found")
	case errors.Is(err, servers.ErrConflict):
		writeError(w, r, http.StatusConflict, "state_conflict", err.Error())
	default:
		if isValidationError(err) {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		h.internalError(w, r, err)
	}
}

func isValidationError(err error) bool {
	value := err.Error()
	prefixes := []string{"invalid ", "unsupported ", "name must", "host port", "max players", "rcon password", "too many", "garrysmod requires", "password must", "too many environment", "backup retention count", "extra argument conflicts", "cpu quota", "memory limit", "target port", "duplicate host port", "duplicate port name"}
	for _, prefix := range prefixes {
		if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
