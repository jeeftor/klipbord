package kb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]Credentials
}

func (store *memorySecrets) Delete(profile string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.values, profile)
	return nil
}

func (store *memorySecrets) Get(profile string) (Credentials, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	credentials, ok := store.values[profile]
	if !ok {
		return Credentials{}, os.ErrNotExist
	}
	return credentials, nil
}

func (store *memorySecrets) Set(profile string, credentials Credentials) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values[profile] = credentials
	return nil
}

func newTestStore(t *testing.T) (*ConfigStore, *memorySecrets) {
	t.Helper()
	secrets := &memorySecrets{values: map[string]Credentials{}}
	store, err := NewConfigStore(filepath.Join(t.TempDir(), "config.yaml"), secrets)
	if err != nil {
		t.Fatal(err)
	}
	return store, secrets
}

func TestClientUsesBearerAndExistingItemEndpoints(t *testing.T) {
	store, _ := newTestStore(t)
	var seenAuth string
	var pinned bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seenAuth = request.Header.Get("Authorization")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/text":
			_, _ = io.ReadAll(request.Body)
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "text1", Name: "note.txt", URL: "https://kb.test/text1/note.txt"})
		case request.Method == http.MethodPost && request.URL.Path == "/api/upload":
			if err := request.ParseMultipartForm(1024 * 1024); err != nil {
				t.Fatal(err)
			}
			file, header, err := request.FormFile("file")
			if err != nil || header.Filename != "report.txt" {
				t.Fatalf("unexpected upload: %v %v", header, err)
			}
			defer file.Close()
			_ = json.NewEncoder(writer).Encode(CreatedItem{ID: "file1", Name: header.Filename, URL: "https://kb.test/file1/report.txt"})
		case request.Method == http.MethodGet && request.URL.Path == "/api/files":
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{{ID: "text1", Name: "note.txt", Type: "text"}, {ID: "file1", Name: "report.txt", Type: "file"}}})
		case request.Method == http.MethodGet && request.URL.Path == "/api/text/text1":
			_, _ = io.WriteString(writer, "hello from Klipbord")
		case request.Method == http.MethodPatch && request.URL.Path == "/api/files/text1":
			pinned = true
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "text1"})
		case request.Method == http.MethodDelete && request.URL.Path == "/api/files/text1":
			_ = json.NewEncoder(writer).Encode(map[string]string{"status": "deleted"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "bearer"}, Credentials{Token: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateText(context.Background(), "hello", "note.txt", "7d", true)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "text1" || !pinned {
		t.Fatalf("created=%+v pinned=%t", created, pinned)
	}
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadFile(context.Background(), path, "", "7d", false); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := client.Get(context.Background(), "text1", "", &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hello from Klipbord" {
		t.Fatalf("text output = %q", output.String())
	}
	if err := client.Delete(context.Background(), "text1"); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q", seenAuth)
	}
}

func TestConfigLeavesCredentialsOutOfFile(t *testing.T) {
	store, secrets := newTestStore(t)
	credentials := Credentials{Token: "do-not-write", Headers: map[string]string{"X-Secret": "also-secret"}}
	if err := store.SaveProfile("work", Profile{URL: "https://kb.example.test", Method: "headers"}, credentials); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "secret") {
		t.Fatalf("config contains secret: %s", contents)
	}
	if got, err := secrets.Get("work"); err != nil || got.Token != credentials.Token {
		t.Fatalf("keychain credentials = %+v, %v", got, err)
	}
}

func TestSetActiveProfile(t *testing.T) {
	store, _ := newTestStore(t)
	for _, name := range []string{"home", "work"} {
		if err := store.SaveProfile(name, Profile{URL: "https://" + name + ".example.test", Method: "bearer"}, Credentials{Token: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetActiveProfile("work"); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.ActiveProfile != "work" {
		t.Fatalf("active profile = %q", config.ActiveProfile)
	}
}

func TestOIDCRefreshesExpiredToken(t *testing.T) {
	store, secrets := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/issuer/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{"token_endpoint": serverURL(request) + "/token"})
		case "/token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("grant_type") != "refresh_token" {
				t.Fatalf("grant type = %q", request.Form.Get("grant_type"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "new-token", "refresh_token": "new-refresh", "expires_in": 3600})
		case "/api/files":
			if request.Header.Get("Authorization") != "Bearer new-token" {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []Item{}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	issuer := server.URL + "/issuer"
	if err := store.SaveProfile("default", Profile{URL: server.URL, Method: "oidc", Issuer: issuer, ClientID: "kb-client"}, Credentials{Token: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	credentials, err := secrets.Get("default")
	if err != nil || credentials.Token != "new-token" || credentials.RefreshToken != "new-refresh" {
		t.Fatalf("refreshed credentials = %+v, %v", credentials, err)
	}
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}
