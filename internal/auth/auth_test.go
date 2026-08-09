package auth

import (
	"net/http"
	"testing"
)

func TestPublicEndpoint(t *testing.T) {
	if !publicEndpoint(http.MethodGet, "/api/build-info") {
		t.Fatal("build-info endpoint must be public")
	}
	if publicEndpoint(http.MethodPost, "/api/build-info") {
		t.Fatal("build-info endpoint must not allow POST without authentication")
	}
}
