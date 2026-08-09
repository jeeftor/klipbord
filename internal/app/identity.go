package app

import (
	"net/http"
	"strings"
)

// Identity is the Authentik identity forwarded by a trusted reverse proxy.
// Klipbord only displays this information; it does not enforce access control.
type Identity struct {
	Authenticated bool     `json:"authenticated"`
	UserID        string   `json:"user_id,omitempty"`
	Username      string   `json:"username,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

func identityHandler(w http.ResponseWriter, r *http.Request) {
	identity := identityFromRequest(r)
	writeJSON(w, identity)
}

func identityFromRequest(r *http.Request) Identity {
	identity := Identity{
		UserID:   r.Header.Get("X-Authentik-Uid"),
		Username: r.Header.Get("X-Authentik-Username"),
	}
	for _, group := range strings.Split(r.Header.Get("X-Authentik-Groups"), "|") {
		if group = strings.TrimSpace(group); group != "" {
			identity.Groups = append(identity.Groups, group)
		}
	}
	identity.Authenticated = identity.UserID != "" || identity.Username != ""
	return identity
}
