package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// linkURL returns the single shareable URL for every item type.
func linkURL(id string) string {
	return strings.TrimRight(baseURL, "/") + "/link/" + id
}

// directLinkHandler serves text as plain text and files with their stored MIME type.
func directLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/link/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	item, ok := findItem(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if item.Type == "text" {
		data, err := os.ReadFile(filepath.Join(dataDir, textDir, id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", item.Name))
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", item.Name))
	http.ServeFile(w, r, filepath.Join(dataDir, fileDir, id))
}
