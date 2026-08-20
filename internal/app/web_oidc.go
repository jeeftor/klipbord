package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	homelabauth "github.com/jeeftor/homelab-auth"
)

var webOIDC *homelabauth.Provider

// initWebOIDC enables native browser login only when all web OIDC settings are configured.
func initWebOIDC() error {
	values := map[string]string{
		"KLIPBORD_OIDC_ISSUER":         os.Getenv("KLIPBORD_OIDC_ISSUER"),
		"KLIPBORD_OIDC_CLIENT_ID":      os.Getenv("KLIPBORD_OIDC_CLIENT_ID"),
		"KLIPBORD_OIDC_CLIENT_SECRET":  os.Getenv("KLIPBORD_OIDC_CLIENT_SECRET"),
		"KLIPBORD_OIDC_REDIRECT_URL":   os.Getenv("KLIPBORD_OIDC_REDIRECT_URL"),
		"KLIPBORD_OIDC_SESSION_SECRET": os.Getenv("KLIPBORD_OIDC_SESSION_SECRET"),
	}
	configured := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			configured++
		}
	}
	if configured == 0 {
		webOIDC = nil
		return nil
	}
	if configured != len(values) {
		return errors.New("KLIPBORD_OIDC_ISSUER, KLIPBORD_OIDC_CLIENT_ID, KLIPBORD_OIDC_CLIENT_SECRET, KLIPBORD_OIDC_REDIRECT_URL, and KLIPBORD_OIDC_SESSION_SECRET must be set together")
	}

	insecureCookies, err := strconv.ParseBool(envOr("KLIPBORD_OIDC_INSECURE_COOKIES", "false"))
	if err != nil {
		return fmt.Errorf("parse KLIPBORD_OIDC_INSECURE_COOKIES: %w", err)
	}
	provider, err := homelabauth.New(context.Background(), homelabauth.Config{
		Issuer:          values["KLIPBORD_OIDC_ISSUER"],
		ClientID:        values["KLIPBORD_OIDC_CLIENT_ID"],
		ClientSecret:    values["KLIPBORD_OIDC_CLIENT_SECRET"],
		RedirectURL:     values["KLIPBORD_OIDC_REDIRECT_URL"],
		SessionSecret:   []byte(values["KLIPBORD_OIDC_SESSION_SECRET"]),
		InsecureCookies: insecureCookies,
		Logger:          slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("configure web OIDC: %w", err)
	}
	webOIDC = provider
	slog.Info("web OIDC login enabled")
	return nil
}

func webOIDCLoginHandler(w http.ResponseWriter, r *http.Request) {
	if webOIDC == nil {
		http.NotFound(w, r)
		return
	}
	webOIDC.LoginHandler(w, r)
}

func webOIDCCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if webOIDC == nil {
		http.NotFound(w, r)
		return
	}
	webOIDC.CallbackHandler(w, r)
}

func webOIDCLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if webOIDC == nil {
		http.NotFound(w, r)
		return
	}
	webOIDC.LogoutHandler(w, r)
}

func webOIDCStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := webLoginStatus{Enabled: webOIDC != nil}
	if webOIDC != nil {
		if identity, authenticated := webOIDC.IdentityFromRequest(r); authenticated {
			status.Authenticated = true
			status.Subject = identity.Subject
			status.Email = identity.Email
			status.Name = identity.Name
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status)
}

type webLoginStatus struct {
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	Subject       string `json:"subject,omitempty"`
	Email         string `json:"email,omitempty"`
	Name          string `json:"name,omitempty"`
}
