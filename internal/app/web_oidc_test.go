package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	homelabauth "github.com/jeeftor/homelab-auth"
)

func TestWebOIDCStatusDisabled(t *testing.T) {
	previous := webOIDC
	webOIDC = nil
	t.Cleanup(func() { webOIDC = previous })

	recorder := httptest.NewRecorder()
	NewHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/session status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var status webLoginStatus
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Enabled || status.Authenticated {
		t.Fatalf("status = %#v, want disabled unauthenticated login", status)
	}
}

func TestWebOIDCStatusEnabledWithoutSession(t *testing.T) {
	previous := webOIDC
	webOIDC = &homelabauth.Provider{}
	t.Cleanup(func() { webOIDC = previous })

	recorder := httptest.NewRecorder()
	NewHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	var status webLoginStatus
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Enabled || status.Authenticated {
		t.Fatalf("status = %#v, want enabled unauthenticated login", status)
	}
}
