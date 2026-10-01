package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/jackc/pgx/v5"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	token, principal, err := h.auth.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			_ = h.audit.Record(r.Context(), "", "user.login", "auth", "denied", requestID(r.Context()), map[string]any{"email": input.Email})
			writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		h.internalError(w, r, err)
		return
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "user.login", "auth", "success", requestID(r.Context()), nil)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "token_type": "Bearer", "user": principal})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := h.auth.Logout(r.Context(), bearerToken(r)); err != nil {
		h.internalError(w, r, err)
		return
	}
	_ = h.audit.Record(r.Context(), principal.UserID, "user.logout", "auth", "success", requestID(r.Context()), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	writeJSON(w, http.StatusOK, principal)
}

type createUserRequest struct {
	Email    string       `json:"email"`
	Password string       `json:"password"`
	Roles    []model.Role `json:"roles"`
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var input createUserRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	user, err := h.auth.CreateUser(r.Context(), input.Email, input.Password, input.Roles)
	if err != nil {
		if isAuthValidationError(err) {
			writeError(w, r, http.StatusBadRequest, "invalid_user", err.Error())
			return
		}
		h.internalError(w, r, err)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	_ = h.audit.Record(r.Context(), principal.UserID, "user.create", "user:"+user.ID, "success", requestID(r.Context()), map[string]any{"roles": user.Roles})
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	users, err := h.auth.ListUsers(r.Context(), limit, offset)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

type setRolesRequest struct {
	Roles []model.Role `json:"roles"`
}

func (h *Handler) setUserRoles(w http.ResponseWriter, r *http.Request) {
	var input setRolesRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.auth.SetRoles(r.Context(), r.PathValue("id"), input.Roles); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "not_found", "user not found")
			return
		}
		if isAuthValidationError(err) {
			writeError(w, r, http.StatusBadRequest, "invalid_roles", err.Error())
			return
		}
		h.internalError(w, r, err)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	_ = h.audit.Record(r.Context(), principal.UserID, "user.roles.change", "user:"+r.PathValue("id"), "success", requestID(r.Context()), map[string]any{"roles": input.Roles})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func queryInt(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func isAuthValidationError(err error) bool {
	value := err.Error()
	return value == "invalid email" || strings.HasPrefix(value, "password must") || strings.HasPrefix(value, "invalid role") || value == "at least one role is required"
}
