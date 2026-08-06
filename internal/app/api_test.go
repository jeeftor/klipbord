package app

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- API test helpers ---

func setupTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := setupTestDir(t)
	baseURL = "http://test.local"
	visionEnabled = false // disable vision for API tests unless explicitly enabled

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/api/version", versionHandler)
	mux.HandleFunc("/api/health", healthHandler)
	mux.HandleFunc("/api/files", apiFilesHandler)
	mux.HandleFunc("/api/files/", apiFileHandler)
	mux.HandleFunc("/api/analyze/", apiAnalyzeHandler)
	mux.HandleFunc("/api/prompts", apiPromptsHandler)
	mux.HandleFunc("/api/prompts/", apiPromptHandler)
	mux.HandleFunc("/api/text", apiTextHandler)
	mux.HandleFunc("/api/text/", apiTextItemHandler)
	mux.HandleFunc("/link/", directLinkHandler)
	mux.HandleFunc("/api/upload", apiUploadHandler)
	mux.HandleFunc("/api/upload/init", apiUploadInitHandler)
	mux.HandleFunc("/api/upload/chunk/", apiUploadChunkHandler)
	mux.HandleFunc("/api/upload/complete", apiUploadCompleteHandler)
	mux.HandleFunc("/mcp", mcpHandler)

	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		server.Close()
		teardownTestDir(t, dir)
	})
	return server, dir
}

func doRequest(t *testing.T, server *httptest.Server, method, path string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// doRequestNoRedirect is like doRequest but does not follow redirects, so the
// caller can inspect 3xx responses directly.
func doRequestNoRedirect(t *testing.T, server *httptest.Server, method, path string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

func parseJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}
	return result
}

// --- Tests ---

func TestVersionEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)
	resp := doRequest(t, server, "GET", "/api/version", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("version endpoint status = %d, expected 200", resp.StatusCode)
	}
	body := parseJSON(t, resp)
	if body["version"] != version {
		t.Errorf("version = %v, expected %q", body["version"], version)
	}
}

func TestHealthEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)
	resp := doRequest(t, server, "GET", "/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health endpoint status = %d, expected 200", resp.StatusCode)
	}
	body := parseJSON(t, resp)
	if body["status"] != "ok" {
		t.Errorf("status = %v, expected 'ok'", body["status"])
	}
}

func TestCreateAndGetText(t *testing.T) {
	server, _ := setupTestServer(t)

	// Create text item
	body := strings.NewReader(`{"content":"hello world","name":"test.txt","ttl":"7d"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create text status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing or invalid id: %v", result["id"])
	}

	// Get the text item via API (returns raw text, not JSON)
	resp = doRequest(t, server, "GET", "/api/text/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get text status = %d", resp.StatusCode)
	}
	textData, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(textData) != "hello world" {
		t.Errorf("content = %q, expected 'hello world'", string(textData))
	}
}

func TestDirectLinkServesTextAndFiles(t *testing.T) {
	server, dir := setupTestServer(t)

	textBody := strings.NewReader(`{"content":"linked text","name":"linked.txt","ttl":"7d"}`)
	resp := doRequest(t, server, "POST", "/api/text", textBody)
	textID := parseJSON(t, resp)["id"].(string)

	// Use ?direct=1 to skip the filename redirect and serve inline
	resp = doRequest(t, server, "GET", "/link/"+textID+"?direct=1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("text link status = %d", resp.StatusCode)
	}
	text, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read text link: %v", err)
	}
	if got, want := string(text), "linked text"; got != want {
		t.Errorf("text link body = %q, expected %q", got, want)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("text link Content-Type = %q", got)
	}

	fileID := "file01"
	fileContent := []byte("linked file")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "linked.bin", Type: "file", MimeType: "application/octet-stream", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	resp = doRequest(t, server, "GET", "/link/"+fileID+"/linked.bin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file link status = %d", resp.StatusCode)
	}
	file, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read file link: %v", err)
	}
	if got := string(file); got != string(fileContent) {
		t.Errorf("file link body = %q, expected %q", got, fileContent)
	}

	for _, path := range []string{"/f/" + textID, "/t/" + textID} {
		resp = doRequest(t, server, "GET", path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("retired route %s status = %d, expected 404", path, resp.StatusCode)
		}
	}
}

func TestListFiles(t *testing.T) {
	server, _ := setupTestServer(t)

	// Create a few text items
	for i, content := range []string{"item1", "item2", "item3"} {
		body := strings.NewReader(`{"content":"` + content + `","name":"file` + string(rune('0'+i)) + `.txt"}`)
		doRequest(t, server, "POST", "/api/text", body)
	}

	// List files
	resp := doRequest(t, server, "GET", "/api/files", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("list files status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("items is not an array: %T", result["items"])
	}
	if len(items) != 3 {
		t.Errorf("items count = %d, expected 3", len(items))
	}
}

func TestListFilesAnalysisSummaryAndDetail(t *testing.T) {
	server, _ := setupTestServer(t)

	resp := doRequest(t, server, "POST", "/api/text", strings.NewReader(`{"content":"summary test","name":"summary.txt"}`))
	itemID := parseJSON(t, resp)["id"].(string)
	if !updateItem(itemID, func(item *Item) bool {
		item.Analyses = map[string]*ItemAnalysis{
			"default": {
				Status:      "complete",
				Text:        "full extracted text",
				Description: "full description",
				Backend:     "vision-model",
				DurationMs:  123,
			},
		}
		return true
	}) {
		t.Fatal("could not add test analysis")
	}

	resp = doRequest(t, server, "GET", "/api/files?analysis=summary", nil)
	items := parseJSON(t, resp)["items"].([]interface{})
	entry := items[0].(map[string]interface{})
	analysis := entry["analyses"].(map[string]interface{})["default"].(map[string]interface{})
	if _, ok := analysis["text"]; ok {
		t.Error("summary response included full extracted text")
	}
	if analysis["status"] != "complete" {
		t.Errorf("summary status = %v, expected complete", analysis["status"])
	}

	resp = doRequest(t, server, "GET", "/api/files/"+itemID+"/analysis", nil)
	detail := parseJSON(t, resp)["analyses"].(map[string]interface{})["default"].(map[string]interface{})
	if detail["text"] != "full extracted text" {
		t.Errorf("detail text = %v, expected full extracted text", detail["text"])
	}
}

func TestListFilesPersistentFilter(t *testing.T) {
	server, _ := setupTestServer(t)

	// Create a non-persistent item
	body := strings.NewReader(`{"content":"regular","name":"reg.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	regResult := parseJSON(t, resp)
	regID := regResult["id"].(string)

	// Create and pin another item
	body = strings.NewReader(`{"content":"pinned","name":"pin.txt"}`)
	resp = doRequest(t, server, "POST", "/api/text", body)
	pinResult := parseJSON(t, resp)
	pinID := pinResult["id"].(string)

	// Pin it
	patchBody := strings.NewReader(`{"persistent":true}`)
	doRequest(t, server, "PATCH", "/api/files/"+pinID, patchBody)

	// List non-persistent
	resp = doRequest(t, server, "GET", "/api/files?persistent=false", nil)
	result := parseJSON(t, resp)
	items := result["items"].([]interface{})
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["id"] == pinID {
			t.Error("pinned item appeared in non-persistent list")
		}
		if m["id"] == regID {
			// expected
		}
	}

	// List persistent
	resp = doRequest(t, server, "GET", "/api/files?persistent=true", nil)
	result = parseJSON(t, resp)
	items = result["items"].([]interface{})
	found := false
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["id"] == pinID {
			found = true
		}
	}
	if !found {
		t.Error("pinned item not found in persistent list")
	}
}

func TestPatchPersistent(t *testing.T) {
	server, _ := setupTestServer(t)

	// Create item
	body := strings.NewReader(`{"content":"test","name":"test.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Pin it
	patchBody := strings.NewReader(`{"persistent":true}`)
	resp = doRequest(t, server, "PATCH", "/api/files/"+id, patchBody)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("patch status = %d", resp.StatusCode)
	}
	result = parseJSON(t, resp)
	if result["persistent"] != true {
		t.Errorf("persistent = %v, expected true", result["persistent"])
	}
	if result["ttl"] != "never" {
		t.Errorf("ttl = %v, expected 'never'", result["ttl"])
	}

	// Unpin it
	patchBody = strings.NewReader(`{"persistent":false}`)
	resp = doRequest(t, server, "PATCH", "/api/files/"+id, patchBody)
	result = parseJSON(t, resp)
	if result["persistent"] != false {
		t.Errorf("persistent = %v, expected false", result["persistent"])
	}
}

func TestPatchMissingPersistentField(t *testing.T) {
	server, _ := setupTestServer(t)

	body := strings.NewReader(`{"content":"test","name":"test.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// PATCH without persistent field
	patchBody := strings.NewReader(`{}`)
	resp = doRequest(t, server, "PATCH", "/api/files/"+id, patchBody)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing persistent field, got %d", resp.StatusCode)
	}
}

func TestDeleteItemAPI(t *testing.T) {
	server, _ := setupTestServer(t)

	// Create item
	body := strings.NewReader(`{"content":"to delete","name":"del.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Delete it
	resp = doRequest(t, server, "DELETE", "/api/files/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("delete status = %d", resp.StatusCode)
	}

	// Verify it's gone
	_, ok := findItem(id)
	if ok {
		t.Error("item still exists after delete")
	}
}

func TestFileUpload(t *testing.T) {
	server, dir := setupTestServer(t)

	// Create a test file
	fileContent := []byte("test file content")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write(fileContent)
	writer.WriteField("ttl", "7d")
	writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Verify file exists on disk
	filePath := filepath.Join(dir, fileDir, id)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("File not found on disk: %v", err)
	}
	if string(data) != "test file content" {
		t.Errorf("file content = %q, expected 'test file content'", string(data))
	}

	// Verify it's in the list
	resp = doRequest(t, server, "GET", "/api/files", nil)
	result = parseJSON(t, resp)
	items := result["items"].([]interface{})
	found := false
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["id"] == id {
			found = true
			if m["type"] != "file" {
				t.Errorf("type = %v, expected 'file'", m["type"])
			}
		}
	}
	if !found {
		t.Error("uploaded file not found in list")
	}
}

func TestFileUploadWithImage(t *testing.T) {
	server, dir := setupTestServer(t)

	// Create a minimal PNG
	pngData := createMinimalPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// Create a form file with explicit image/png content type
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="test.png"`}
	h["Content-Type"] = []string{"image/png"}
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatalf("Failed to create form part: %v", err)
	}
	part.Write(pngData)
	writer.WriteField("ttl", "7d")
	writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Verify file exists on disk
	filePath := filepath.Join(dir, fileDir, id)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("File not found on disk: %v", err)
	}

	// Verify mime type is set correctly
	item, ok := findItem(id)
	if !ok {
		t.Fatal("item not found in metadata")
	}
	if !strings.HasPrefix(item.MimeType, "image/") {
		t.Errorf("mime_type = %q, expected image/*", item.MimeType)
	}
}

func TestAnalyzeNonImage(t *testing.T) {
	server, _ := setupTestServer(t)
	visionEnabled = true

	// Create a text item
	body := strings.NewReader(`{"content":"hello","name":"test.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Try to analyze it (should fail)
	resp = doRequest(t, server, "POST", "/api/analyze/"+id, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("analyze text item status = %d, expected 400", resp.StatusCode)
	}
}

func TestAnalyzeNonExistent(t *testing.T) {
	server, _ := setupTestServer(t)
	visionEnabled = true

	resp := doRequest(t, server, "POST", "/api/analyze/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("analyze non-existent status = %d, expected 404", resp.StatusCode)
	}
}

func TestAnalyzeVisionDisabled(t *testing.T) {
	server, _ := setupTestServer(t)
	visionEnabled = false

	// Upload an image with proper content type
	pngData := createMinimalPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="test.png"`}
	h["Content-Type"] = []string{"image/png"}
	part, _ := writer.CreatePart(h)
	part.Write(pngData)
	writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, _ := http.DefaultClient.Do(req)
	result := parseJSON(t, resp)
	id := result["id"].(string)

	// Try to analyze (should fail - vision disabled)
	resp = doRequest(t, server, "POST", "/api/analyze/"+id, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("analyze with vision disabled status = %d, expected 503", resp.StatusCode)
	}
}

func TestRootHandlerNotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	resp := doRequest(t, server, "GET", "/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("nonexistent path status = %d, expected 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestShortLinkServesItem(t *testing.T) {
	server, dir := setupTestServer(t)

	// Use a 12-char lowercase alphanumeric ID to match the ID format.
	fileID := "shortlink001"
	fileContent := []byte("short link content")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "generated_64x64_30fps.gif", Type: "file", MimeType: "image/gif", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	// /{id}/{filename} should serve the item directly
	resp := doRequest(t, server, "GET", "/"+fileID+"/generated_64x64_30fps.gif", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("short link with filename status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(fileContent) {
		t.Errorf("short link body = %q, expected %q", body, fileContent)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "generated_64x64_30fps.gif") {
		t.Errorf("Content-Disposition = %q, expected filename", got)
	}

	// /{id}?direct=1 should serve inline (skip redirect)
	resp = doRequest(t, server, "GET", "/"+fileID+"?direct=1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("short link direct status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// /{id}/{filename}?download=1 should force attachment
	resp = doRequest(t, server, "GET", "/"+fileID+"/generated_64x64_30fps.gif?download=1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("short link download status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, expected attachment", got)
	}
}

func TestShortLinkRedirectsToFilename(t *testing.T) {
	server, dir := setupTestServer(t)

	fileID := "redirtest001"
	fileContent := []byte("redirect test")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "report.docx", Type: "file", MimeType: "application/octet-stream", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	// Bare /{id} should 302 redirect to /{id}/{filename}
	resp := doRequestNoRedirect(t, server, "GET", "/"+fileID, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("bare short link status = %d, expected 302", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location != "/"+fileID+"/report.docx" {
		t.Errorf("redirect Location = %q, expected /%s/report.docx", location, fileID)
	}
	resp.Body.Close()

	// /{id}?download=1 should redirect to /{id}/{filename}?download=1
	resp = doRequestNoRedirect(t, server, "GET", "/"+fileID+"?download=1", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("bare short link with download status = %d, expected 302", resp.StatusCode)
	}
	location = resp.Header.Get("Location")
	if location != "/"+fileID+"/report.docx?download=1" {
		t.Errorf("redirect Location = %q, expected /%s/report.docx?download=1", location, fileID)
	}
	resp.Body.Close()

	// /{id}?direct=1 should NOT redirect — should serve directly
	resp = doRequest(t, server, "GET", "/"+fileID+"?direct=1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct bypass status = %d, expected 200", resp.StatusCode)
	}
	resp.Body.Close()

	// /link/{id} should also redirect (backwards compat)
	resp = doRequestNoRedirect(t, server, "GET", "/link/"+fileID, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("legacy bare link status = %d, expected 302", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestShortLinkNotFoundForInvalidID(t *testing.T) {
	server, _ := setupTestServer(t)
	// 12-char ID that doesn't exist in the store
	resp := doRequest(t, server, "GET", "/aaaaaaaaaaaa", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("nonexistent short link status = %d, expected 404", resp.StatusCode)
	}
	resp.Body.Close()
	// Non-ID-shaped paths should still 404
	resp = doRequest(t, server, "GET", "/random-path", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("random path status = %d, expected 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHeadRequestRevealsFilename(t *testing.T) {
	server, dir := setupTestServer(t)

	fileID := "headreq00001"
	fileContent := []byte("head request test")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "Quarterly_Report.docx", Type: "file", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	// HEAD request on named URL should return headers (including filename) without body
	resp := doRequest(t, server, "HEAD", "/"+fileID+"/Quarterly_Report.docx", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "Quarterly_Report.docx") {
		t.Errorf("HEAD Content-Disposition = %q, expected filename", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "wordprocessingml") {
		t.Errorf("HEAD Content-Type = %q, expected docx mime", got)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 0 {
		t.Errorf("HEAD body = %d bytes, expected empty", len(body))
	}

	// HEAD via /link/ prefix should also work
	resp = doRequest(t, server, "HEAD", "/link/"+fileID+"/Quarterly_Report.docx", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD via /link/ status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDirectLinkMethodNotAllowed(t *testing.T) {
	server, dir := setupTestServer(t)
	fileID := "methodtest01"
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	addItem(Item{ID: fileID, Name: "test.bin", Type: "file", MimeType: "application/octet-stream", Size: 1, Created: time.Now(), TTL: "never"})

	// POST to /{id}/{filename} should be 405
	resp := doRequest(t, server, "POST", "/"+fileID+"/test.bin", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST short link status = %d, expected 405", resp.StatusCode)
	}
	resp.Body.Close()

	// POST to /link/{id}/{filename} should be 405
	resp = doRequest(t, server, "POST", "/link/"+fileID+"/test.bin", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /link/ status = %d, expected 405", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDirectLinkTextHeadRequest(t *testing.T) {
	server, _ := setupTestServer(t)

	body := strings.NewReader(`{"content":"head text test","name":"head.txt"}`)
	resp := doRequest(t, server, "POST", "/api/text", body)
	textID := parseJSON(t, resp)["id"].(string)

	// HEAD on a text item via named URL should return headers without body
	resp = doRequest(t, server, "HEAD", "/"+textID+"/head.txt", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD text status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "head.txt") {
		t.Errorf("HEAD text Content-Disposition = %q, expected filename", got)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(bodyBytes) != 0 {
		t.Errorf("HEAD text body = %d bytes, expected empty", len(bodyBytes))
	}
}

func TestDirectLinkTextNotFoundOnDisk(t *testing.T) {
	server, _ := setupTestServer(t)

	// Add a text item but don't write the file to disk
	addItem(Item{ID: "ghosttext001", Name: "ghost.txt", Type: "text", Size: 5, Created: time.Now(), TTL: "never"})

	resp := doRequest(t, server, "GET", "/ghosttext001/ghost.txt", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing text file status = %d, expected 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPatchMimeType(t *testing.T) {
	server, dir := setupTestServer(t)

	fileID := "filemime01"
	fileContent := []byte("mime test")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "photo.gif", Type: "file", MimeType: "application/octet-stream", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	// Patch the MIME type
	patchBody := strings.NewReader(`{"mime_type":"image/gif"}`)
	resp := doRequest(t, server, "PATCH", "/api/files/"+fileID, patchBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch mime status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	if got := result["mime_type"].(string); got != "image/gif" {
		t.Errorf("patched mime_type = %q, expected image/gif", got)
	}

	// Verify it persisted
	item, ok := findItem(fileID)
	if !ok {
		t.Fatal("item not found after patch")
	}
	if item.MimeType != "image/gif" {
		t.Errorf("stored mime_type = %q, expected image/gif", item.MimeType)
	}

	// Patching with no fields should error
	resp = doRequest(t, server, "PATCH", "/api/files/"+fileID, strings.NewReader(`{}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty patch status = %d, expected 400", resp.StatusCode)
	}
}

func TestDirectLinkWithFilenameSegment(t *testing.T) {
	server, dir := setupTestServer(t)

	fileID := "filefn01"
	fileContent := []byte("named file content")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "generated_64x64_30fps.gif", Type: "file", MimeType: "image/gif", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	// /link/{id}/{filename} should serve the same content as /link/{id}
	resp := doRequest(t, server, "GET", "/link/"+fileID+"/generated_64x64_30fps.gif", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("named link status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(fileContent) {
		t.Errorf("named link body = %q, expected %q", body, fileContent)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "generated_64x64_30fps.gif") {
		t.Errorf("Content-Disposition = %q, expected filename", got)
	}
}

func TestDirectLinkDownloadQuery(t *testing.T) {
	server, dir := setupTestServer(t)

	fileID := "filedl01"
	fileContent := []byte("download me")
	if err := os.WriteFile(filepath.Join(dir, fileDir, fileID), fileContent, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	addItem(Item{ID: fileID, Name: "report.pdf", Type: "file", MimeType: "application/pdf", Size: int64(len(fileContent)), Created: time.Now(), TTL: "never"})

	resp := doRequest(t, server, "GET", "/link/"+fileID+"/report.pdf?download=1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download link status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, expected attachment", got)
	}
}

func TestLinkURLIncludesFilename(t *testing.T) {
	baseURL = "https://klipbord.example.com"
	if got, want := linkURL("abc123"), "https://klipbord.example.com/abc123"; got != want {
		t.Errorf("linkURL without name = %q, expected %q", got, want)
	}
	if got, want := linkURL("abc123", "generated_64x64_30fps.gif"), "https://klipbord.example.com/abc123/generated_64x64_30fps.gif"; got != want {
		t.Errorf("linkURL with name = %q, expected %q", got, want)
	}
	// Path separators in the name should be stripped.
	if got, want := linkURL("abc123", "sub/dir/file.txt"), "https://klipbord.example.com/abc123/file.txt"; got != want {
		t.Errorf("linkURL with path name = %q, expected %q", got, want)
	}
}

func TestUploadDetectsMimeFromExtension(t *testing.T) {
	server, _ := setupTestServer(t)

	// Upload a .gif with a generic octet-stream content type (as curl does
	// when no ;type= is specified). The server should detect image/gif from
	// the filename extension so the item is treated as an image.
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="generated_64x64_30fps.gif"`}
	h["Content-Type"] = []string{"application/octet-stream"}
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatalf("create form part: %v", err)
	}
	part.Write([]byte("GIF89a fake gif data"))
	writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	result := parseJSON(t, resp)
	id := result["id"].(string)

	item, ok := findItem(id)
	if !ok {
		t.Fatal("uploaded item not found")
	}
	if item.MimeType != "image/gif" {
		t.Errorf("mime_type = %q, expected image/gif", item.MimeType)
	}
	// The returned URL should include the filename.
	if got := result["url"].(string); !strings.Contains(got, "/generated_64x64_30fps.gif") {
		t.Errorf("url = %q, expected to contain filename", got)
	}
}

// --- Test helpers for images ---

func createMinimalPNG(t *testing.T) []byte {
	t.Helper()
	// Minimal valid 1x1 PNG
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, // IHDR length
		0x49, 0x48, 0x44, 0x52, // "IHDR"
		0x00, 0x00, 0x00, 0x01, // width = 1
		0x00, 0x00, 0x00, 0x01, // height = 1
		0x08, 0x02, // bit depth = 8, color type = 2 (RGB)
		0x00, 0x00, 0x00, // compression, filter, interlace
		0x90, 0x77, 0x53, 0xDE, // CRC
		0x00, 0x00, 0x00, 0x0C, // IDAT length
		0x49, 0x44, 0x41, 0x54, // "IDAT"
		0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, 0x00, // compressed data
		0x01, 0x01, 0x00, 0x05, // more data
		0xFF, 0x01, 0x0E, 0xA0, // CRC
		0x00, 0x00, 0x00, 0x00, // IEND length
		0x49, 0x45, 0x4E, 0x44, // "IEND"
		0xAE, 0x42, 0x60, 0x82, // CRC
	}
}

// Ensure time is imported
var _ = time.Now
