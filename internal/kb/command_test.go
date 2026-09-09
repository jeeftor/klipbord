package kb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "kb.example.com", want: "https://kb.example.com"},
		{input: " https://kb.example.com/ ", want: "https://kb.example.com"},
		{input: "http://localhost:8080", want: "http://localhost:8080"},
	}
	for _, test := range tests {
		got, err := normalizeServerURL(test.input)
		if err != nil {
			t.Fatalf("normalize %q: %v", test.input, err)
		}
		if got != test.want {
			t.Errorf("normalize %q = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := normalizeServerURL("ftp://kb.example.com"); err == nil {
		t.Fatal("expected non-HTTP URL to fail")
	}
}

func TestAllowsHTTPFallbackOnlyForLocalTargets(t *testing.T) {
	for _, serverURL := range []string{"https://localhost:8080", "https://klipbord.local", "https://192.168.1.10"} {
		if !allowsHTTPFallback(serverURL) {
			t.Errorf("expected HTTP fallback for %s", serverURL)
		}
	}
	if allowsHTTPFallback("https://kb.example.com") {
		t.Fatal("public hosts must not fall back to HTTP")
	}
}

func TestProfileAllowsPrivateNetworkHTTPAfterFallback(t *testing.T) {
	for _, serverURL := range []string{"http://klipbord.local", "http://192.168.1.10"} {
		if err := validateProfile(Profile{URL: serverURL, Method: "none"}); err != nil {
			t.Errorf("validateProfile(%q) = %v, want local HTTP profile to be accepted", serverURL, err)
		}
	}
	if err := validateProfile(Profile{URL: "http://klipbord.example.com", Method: "none"}); err == nil {
		t.Fatal("public HTTP profile must be rejected")
	}
}

func TestDebugLoggerOnlyWritesAtDebugLevel(t *testing.T) {
	var quiet, debug strings.Builder
	newDebugLogger("info", &quiet)("request: %s", "hidden")
	newDebugLogger("debug", &debug)("request: %s", "safe")
	if quiet.Len() != 0 {
		t.Fatalf("non-debug logger wrote %q", quiet.String())
	}
	if got, want := debug.String(), "[debug] request: safe\n"; got != want {
		t.Fatalf("debug logger = %q, want %q", got, want)
	}
}

func TestUniquePathNoCollision(t *testing.T) {
	dir := t.TempDir()
	got := uniquePath(dir, "photo.png")
	want := filepath.Join(dir, "photo.png")
	if got != want {
		t.Fatalf("uniquePath = %q, want %q", got, want)
	}
}

func TestUniquePathHandlesEmptyName(t *testing.T) {
	dir := t.TempDir()
	got := uniquePath(dir, "")
	want := filepath.Join(dir, "unnamed")
	if got != want {
		t.Fatalf("uniquePath = %q, want %q", got, want)
	}
}

func TestUniquePathAppendsSuffixOnCollision(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc-1.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	got := uniquePath(dir, "doc.txt")
	want := filepath.Join(dir, "doc-2.txt")
	if got != want {
		t.Fatalf("uniquePath = %q, want %q", got, want)
	}
}

func TestUniquePathPreservesExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.png"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	got := uniquePath(dir, "photo.png")
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("uniquePath = %q, want .png suffix", got)
	}
}

func TestPullPollDownloadsNewItemsOnly(t *testing.T) {
	store, _ := newTestStore(t)
	var listCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/files":
			listCalls++
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{
				{ID: "old", Name: "old.txt", Type: "text"},
				{ID: "new1", Name: "new1.txt", Type: "text"},
				{ID: "new2", Name: "new2.txt", Type: "text"},
			}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/old":
			_, _ = io.WriteString(writer, "old content")
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/new1":
			_, _ = io.WriteString(writer, "new1 content")
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/new2":
			_, _ = io.WriteString(writer, "new2 content")
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seen := map[string]bool{"old": true}
	var stdout, stderr strings.Builder
	if err := pullPoll(context.Background(), client, dir, seen, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"new1.txt", "new2.txt"} {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		want := strings.TrimSuffix(name, ".txt") + " content"
		if string(content) != want {
			t.Fatalf("%s content = %q, want %q", name, content, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("old item should not have been downloaded")
	}
	if !seen["new1"] || !seen["new2"] {
		t.Fatalf("seen = %v, want new1 and new2 marked seen", seen)
	}
}

func TestPullPollSkipsAlreadySeen(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/files":
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{
				{ID: "seen", Name: "seen.txt", Type: "text"},
			}})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seen := map[string]bool{"seen": true}
	var stdout, stderr strings.Builder
	if err := pullPoll(context.Background(), client, dir, seen, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestPullPollContinuesAfterDownloadError(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/files":
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{
				{ID: "bad", Name: "bad.txt", Type: "text"},
				{ID: "good", Name: "good.txt", Type: "text"},
			}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/bad":
			http.Error(writer, "server error", http.StatusInternalServerError)
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/good":
			_, _ = io.WriteString(writer, "good content")
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seen := map[string]bool{}
	var stdout, stderr strings.Builder
	if err := pullPoll(context.Background(), client, dir, seen, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "good.txt")); err != nil {
		t.Fatalf("good.txt should have been downloaded: %v", err)
	}
	if !strings.Contains(stderr.String(), "Failed to download bad") {
		t.Fatalf("stderr = %q, want download failure message", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Pulled good") {
		t.Fatalf("stdout = %q, want pull success message", stdout.String())
	}
}

func TestPushPollUploadsNewFilesOnly(t *testing.T) {
	store, _ := newTestStore(t)
	var uploadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/upload":
			uploadCalls++
			if err := request.ParseMultipartForm(1024 * 1024); err != nil {
				t.Fatal(err)
			}
			file, header, err := request.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "id-" + header.Filename, Name: header.Filename, URL: "https://kb.test/" + header.Filename})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta"), 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := map[string]bool{}
	var stdout, stderr strings.Builder
	if err := pushPoll(context.Background(), client, dir, uploaded, "7d", false, false, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if uploadCalls != 2 {
		t.Fatalf("uploadCalls = %d, want 2", uploadCalls)
	}
	if !uploaded[filepath.Join(dir, "a.txt")] || !uploaded[filepath.Join(dir, "b.txt")] {
		t.Fatalf("uploaded = %v, want both files marked", uploaded)
	}
	if !strings.Contains(stdout.String(), "Pushed") {
		t.Fatalf("stdout = %q, want push messages", stdout.String())
	}
}

func TestPushPollSkipsAlreadyUploaded(t *testing.T) {
	store, _ := newTestStore(t)
	var uploadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/upload":
			uploadCalls++
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "x", Name: "x", URL: "https://kb.test/x"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "done.txt")
	if err := os.WriteFile(path, []byte("done"), 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := map[string]bool{path: true}
	var stdout, stderr strings.Builder
	if err := pushPoll(context.Background(), client, dir, uploaded, "7d", false, false, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if uploadCalls != 0 {
		t.Fatalf("uploadCalls = %d, want 0", uploadCalls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestPushPollRemovesAfterUpload(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/upload":
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "x", Name: "x", URL: "https://kb.test/x"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(path, []byte("bye"), 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := map[string]bool{}
	var stdout, stderr strings.Builder
	if err := pushPoll(context.Background(), client, dir, uploaded, "7d", false, true, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should have been removed after upload")
	}
}

func TestPushPollSkipsSubdirectories(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	uploaded := map[string]bool{}
	var stdout, stderr strings.Builder
	if err := pushPoll(context.Background(), client, dir, uploaded, "7d", false, false, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
}

func TestSyncPollPullsAndPushes(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/files":
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{
				{ID: "srv1", Name: "from-server.txt", Type: "text"},
			}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/srv1":
			_, _ = io.WriteString(writer, "from server")
		case request.Method == http.MethodPost && request.URL.Path == "/api/upload":
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "upl1", Name: "to-server.txt", URL: "https://kb.test/upl1"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "none"}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "to-server.txt"), []byte("from local"), 0600); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	uploaded := map[string]bool{}
	var stdout, stderr strings.Builder
	if err := syncPoll(context.Background(), client, dir, seen, uploaded, "7d", false, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	// Server item should have been pulled.
	if _, err := os.ReadFile(filepath.Join(dir, "from-server.txt")); err != nil {
		t.Fatalf("from-server.txt should have been pulled: %v", err)
	}
	// Local file should have been uploaded.
	if !uploaded[filepath.Join(dir, "to-server.txt")] {
		t.Fatal("to-server.txt should have been pushed")
	}
}

func TestAuthentikAppPasswordCredentialsUseBasicAuthentication(t *testing.T) {
	t.Setenv("AUTHENTIK_USERNAME", "alex")
	t.Setenv("AUTHENTIK_APP_PASSWORD", "app-password")
	credentials, err := authentikAppPasswordCredentials("", &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	value := credentials.Headers["Authorization"]
	if !strings.HasPrefix(value, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic authentication", value)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "Basic "))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "alex:app-password" {
		t.Fatalf("decoded authorization = %q", decoded)
	}
}

func TestRootCommandShowsHelpWithoutArgumentsAtTerminal(t *testing.T) {
	original := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = original })

	command := NewRootCommand("test")
	var output strings.Builder
	command.SetOut(&output)
	command.SetArgs(nil)

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !strings.Contains(output.String(), "Upload and manage items in Klipbord") {
		t.Fatalf("expected help output, got %q", output.String())
	}
}
