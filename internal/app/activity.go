package app

import (
	"log/slog"
	"net"
	"net/http"
)

// logUpload records a completed upload without logging request contents.
func logUpload(request *http.Request, id, name string, size int64, mode string) {
	slog.Info("upload complete",
		"id", id,
		"name", name,
		"size_bytes", size,
		"mode", mode,
		"client", requestClient(request),
	)
}

// logDownload records a successful item download without logging query values.
func logDownload(request *http.Request, item Item, disposition string) {
	slog.Info("download served",
		"id", item.ID,
		"name", item.Name,
		"size_bytes", item.Size,
		"disposition", disposition,
		"client", requestClient(request),
	)
}

func requestClient(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}
