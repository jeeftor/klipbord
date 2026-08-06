package app

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// AuthConfig is the response returned by /api/auth/config.
// It tells the kb CLI how to authenticate without requiring the user
// to know the OIDC issuer or client ID upfront.
type AuthConfig struct {
	Method   string   `json:"method"`    // "none", "oidc"
	Issuer   string   `json:"issuer"`    // OIDC issuer URL (empty if method != oidc)
	ClientID string   `json:"client_id"` // OIDC public client ID (empty if method != oidc)
	Scopes   []string `json:"scopes"`    // OIDC scopes to request
}

func authConfigHandler(w http.ResponseWriter, _ *http.Request) {
	config := AuthConfig{
		Method: "none",
	}

	// If OIDC_ISSUER is set, advertise OIDC login
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		config.Method = "oidc"
		config.Issuer = issuer
		config.ClientID = os.Getenv("OIDC_CLIENT_ID")
		if config.ClientID == "" {
			config.ClientID = "klipbord"
		}
		config.Scopes = []string{"openid", "profile", "offline_access"}
		if scopes := os.Getenv("OIDC_SCOPES"); scopes != "" {
			config.Scopes = splitScopes(scopes)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(config)
}

func splitScopes(s string) []string {
	var scopes []string
	for _, scope := range strings.Split(s, " ") {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return []string{"openid", "profile", "offline_access"}
	}
	return scopes
}
