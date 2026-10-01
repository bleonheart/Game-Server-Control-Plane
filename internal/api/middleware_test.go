package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/game-server-control-plane/internal/auth"
	"github.com/example/game-server-control-plane/internal/model"
)

func TestRequireOperator(t *testing.T) {
	handler := requireOperator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	viewerRequest := httptest.NewRequest(http.MethodPost, "/", nil)
	viewerRequest = viewerRequest.WithContext(auth.WithPrincipal(viewerRequest.Context(), model.Principal{UserID: "viewer", Roles: []model.Role{model.RoleViewer}}))
	viewerResponse := httptest.NewRecorder()
	handler.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", viewerResponse.Code)
	}

	operatorRequest := httptest.NewRequest(http.MethodPost, "/", nil)
	operatorRequest = operatorRequest.WithContext(auth.WithPrincipal(operatorRequest.Context(), model.Principal{UserID: "operator", Roles: []model.Role{model.RoleOperator}}))
	operatorResponse := httptest.NewRecorder()
	handler.ServeHTTP(operatorResponse, operatorRequest)
	if operatorResponse.Code != http.StatusNoContent {
		t.Fatalf("operator status=%d", operatorResponse.Code)
	}
}
