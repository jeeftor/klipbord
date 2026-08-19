package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// linkURL returns the single shareable URL for every item type. It uses the
// short form (/{id}) by default. When name is provided and non-empty the
// filename is appended so that clients (curl, wget, browsers) save the file
// with a sensible filename instead of the raw item ID.
func linkURL(id string, name ...string) string {
	base := strings.TrimRight(baseURL, "/") + "/" + id
	if len(name) == 0 || name[0] == "" {
		return base
	}
	safe := sanitizeFilename(name[0])
	if safe == "" {
		return base
	}
	return base + "/" + safe
}

// sanitizeFilename strips path separators and other characters that are unsafe
// in a single URL path segment.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

// directLinkHandler serves text as plain text and files with their stored MIME
// type. It accepts both /link/{id} and /link/{id}/{filename}; the optional
// filename segment lets download tools infer the correct save-as name. A
// ?download=1 query parameter forces an attachment disposition so browsers
// prompt a download using the stored filename.
//
// The /link/ prefix is kept for backwards compatibility, but the canonical
// short form /{id} (handled by rootHandler) is preferred for sharing.
func directLinkHandler(w http.ResponseWriter, r *http.Request) {
	serveDirectLink(w, r, strings.TrimPrefix(r.URL.Path, "/link/"))
}

// serveDirectLink handles the actual item lookup and serving given the path
// remainder after the route prefix (either "/link/" or "/"). The remainder may
// be just the id, or "{id}/{filename}".
//
// When the URL has no filename segment (bare /{id}), the handler redirects to
// /{id}/{filename} so that the filename appears in the URL for download tools
// and AI agents. This redirect can be bypassed with ?direct=1, which serves
// the content inline without a round trip.
func serveDirectLink(w http.ResponseWriter, r *http.Request, rest string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	// Split into id and (optional) filename segment.
	id := rest
	hasFilename := false
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		id = rest[:idx]
		hasFilename = true
	}
	if id == "" {
		http.NotFound(w, r)
		return
	}
	item, ok := findItem(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// If the URL has no filename segment and the item has a name, redirect to
	// the named form so the filename is visible in the URL. Skip the redirect
	// when ?direct=1 is present (lets curl -O work without -L).
	if !hasFilename && r.URL.Query().Get("direct") != "1" {
		safe := sanitizeFilename(item.Name)
		if safe != "" {
			namedURL := "/" + id + "/" + safe
			// Preserve query string (e.g. ?download=1) but drop direct=1
			// since the named URL doesn't need it.
			query := r.URL.Query()
			query.Del("direct")
			if encoded := query.Encode(); encoded != "" {
				namedURL += "?" + encoded
			}
			http.Redirect(w, r, namedURL, http.StatusFound)
			return
		}
	}

	forceDownload := r.URL.Query().Has("download")
	disposition := "inline"
	if forceDownload {
		disposition = "attachment"
	}
	if item.Type == "text" {
		data, err := os.ReadFile(filepath.Join(dataDir, textDir, id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, item.Name))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
		logDownload(r, item, disposition)
		return
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, item.Name))
	logDownload(r, item, disposition)
	http.ServeFile(w, r, filepath.Join(dataDir, fileDir, id))
}
