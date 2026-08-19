package app

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityLogsUploadAndDownload(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	request := httptest.NewRequest("GET", "/item?token=not-logged", nil)
	request.RemoteAddr = "192.0.2.45:1234"

	logUpload(request, "upload123", "report.pdf", 42, "multipart")
	logDownload(request, Item{ID: "download123", Name: "report.pdf", Size: 42}, "attachment")

	logs := output.String()
	for _, expected := range []string{
		"msg=\"upload complete\"", "id=upload123", "name=report.pdf", "size_bytes=42", "mode=multipart", "client=192.0.2.45",
		"msg=\"download served\"", "id=download123", "disposition=attachment",
	} {
		if !strings.Contains(logs, expected) {
			t.Errorf("logs missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "not-logged") {
		t.Fatalf("logs must not include query values: %s", logs)
	}
}
