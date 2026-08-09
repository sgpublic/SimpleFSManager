package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesRootWithoutRedirect(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("unexpected redirect to %q", location)
	}
}
