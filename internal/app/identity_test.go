package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityHandlerReturnsForwardedAuthentikIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/identity", nil)
	request.Header.Set("X-Authentik-Uid", "stable-user-id")
	request.Header.Set("X-Authentik-Username", "alex")
	request.Header.Set("X-Authentik-Groups", "family | klipbord-admin")
	response := httptest.NewRecorder()

	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/identity status = %d, want %d", response.Code, http.StatusOK)
	}
	var identity Identity
	if err := json.NewDecoder(response.Body).Decode(&identity); err != nil {
		t.Fatalf("decode identity response: %v", err)
	}
	if !identity.Authenticated || identity.UserID != "stable-user-id" || identity.Username != "alex" {
		t.Fatalf("identity = %#v, want forwarded Authentik identity", identity)
	}
	if len(identity.Groups) != 2 || identity.Groups[0] != "family" || identity.Groups[1] != "klipbord-admin" {
		t.Errorf("groups = %#v, want [family klipbord-admin]", identity.Groups)
	}
}

func TestIdentityHandlerReportsNoForwardedIdentity(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/identity", nil))

	var identity Identity
	if err := json.NewDecoder(response.Body).Decode(&identity); err != nil {
		t.Fatalf("decode identity response: %v", err)
	}
	if identity.Authenticated {
		t.Errorf("authenticated = true without forwarded identity")
	}
}
