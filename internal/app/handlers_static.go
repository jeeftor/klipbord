package app

import (
	"fmt"
	"net/http"
	"strings"
)

// rootHandler is the catch-all handler registered at "/". It serves three
// purposes:
//   - "/" redirects to the clipboard UI
//   - "/{id}" or "/{id}/{filename}" serves a stored item directly (the short
//     shareable link form, equivalent to /link/{id}[/{filename}])
//   - anything else returns 404
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/clip", http.StatusFound)
		return
	}
	// Strip the leading "/" and try to serve it as a direct link.
	rest := strings.TrimPrefix(r.URL.Path, "/")
	if looksLikeItemID(rest) || looksLikeIDWithFilename(rest) {
		serveDirectLink(w, r, rest)
		return
	}
	http.NotFound(w, r)
}

// looksLikeItemID reports whether s is a plausible item ID (12 lowercase
// alphanumeric characters). This is a cheap pre-filter so we don't hit the
// metadata store for every random 404.
func looksLikeItemID(s string) bool {
	if len(s) != idLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// looksLikeIDWithFilename reports whether s starts with a valid-looking item
// ID followed by "/" and a filename.
func looksLikeIDWithFilename(s string) bool {
	idx := strings.IndexByte(s, '/')
	if idx <= 0 {
		return false
	}
	return looksLikeItemID(s[:idx])
}

func webUIHandler(w http.ResponseWriter, _ *http.Request) {
	html := strings.Replace(string(assets.IndexHTML), "{{VERSION}}", version, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func manifestHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write(assets.ManifestJSON)
}

func swHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(assets.ServiceWorker)
}

func iconHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write(assets.IconSVG)
}

func versionHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(fmt.Sprintf(`{"version":"%s"}`, version)))
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
