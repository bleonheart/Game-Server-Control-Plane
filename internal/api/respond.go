package api

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
	Meta  any       `json:"meta,omitempty"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: value})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: &apiError{Code: code, Message: message, RequestID: requestID(r.Context())}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request body")
		return false
	}
	return true
}
