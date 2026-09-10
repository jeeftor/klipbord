package kb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSubcommandsHonorConfigAndProfileFlags(t *testing.T) {
	keyring.MockInit()
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Item{{ID: "selected-profile"}}})
	}))
	defer server.Close()
	if err := store.Save(Config{ActiveProfile: "missing", Profiles: map[string]Profile{
		"chosen": {URL: server.URL, Method: "none"},
	}}); err != nil {
		t.Fatal(err)
	}
	root := NewRootCommand("test")
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"list", "--config", store.path, "--profile", "chosen", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "selected-profile") {
		t.Fatalf("selected profile was not used: %s", output.String())
	}
}

func TestLoginDebugFlagAccepted(t *testing.T) {
	for _, args := range [][]string{
		{"login", "--debug", "--url", "://invalid"},
		{"--debug", "login", "--url", "://invalid"},
		{"login", "--log-level", "debug", "--url", "://invalid"},
	} {
		root := NewRootCommand("test")
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "absolute http or https URL") {
			t.Fatalf("args %v: expected URL validation, got %v", args, err)
		}
	}
}

func TestConnectionFailureDoesNotEchoCredentials(t *testing.T) {
	store, _ := newTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, r.Header.Get("Authorization")+" secret-password")
	}))
	defer server.Close()
	if err := store.SaveProfile("test", Profile{URL: server.URL, Method: "bearer"}, Credentials{Token: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(store, "test", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.List(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-") {
		t.Fatalf("error echoed response credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "GET "+server.URL+"/api/files") {
		t.Fatalf("error is missing request context: %v", err)
	}
}

func TestLoginConnectionDiagnostics(t *testing.T) {
	keyring.MockInit()
	for _, test := range []struct {
		name, method, body, contentType, hint string
		status                                int
	}{
		{"empty unauthorized", "authentik-app-password", "", "", "App Password", 401},
		{"no credentials", "none", "", "", "sends no credentials", 401},
		{"forbidden", "bearer", "secret-token", "text/plain", "access policy", 403},
		{"not found", "none", "", "", "base URL", 404},
		{"upstream error", "none", "", "", "proxy logs", 502},
		{"redirect", "headers", "", "", "redirected", 302},
		{"browser login", "none", "<html>login</html>", "text/html", "HTML instead of API JSON", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KB_PASSWORD", "secret-password")
			t.Setenv("KB_TOKEN", "secret-token")
			t.Setenv("KB_HEADER_X_API_KEY", "secret-header")
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/api/files" {
					t.Errorf("login followed redirect to %s", r.URL.Path)
				}
				if r.UserAgent() != "klipbord-cli/test" {
					t.Errorf("unexpected User-Agent: %s", r.UserAgent())
				}
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Set-Cookie", "session=secret-cookie")
				w.Header().Set("WWW-Authenticate", "Bearer secret-challenge")
				if test.status == 302 {
					w.Header().Set("Location", "/browser-login?token=secret-redirect#secret-fragment")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			root := NewRootCommand("test")
			var stdout, stderr strings.Builder
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			root.SetArgs([]string{"login", "--debug", "--url", server.URL, "--method", test.method, "--username", "test-user", "--header", "X-API-Key", "--name", "diagnostic-test", "--config", configPath})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), test.hint) || !strings.Contains(err.Error(), "saved profile") {
				t.Fatalf("missing diagnostic hint %q: %v", test.hint, err)
			}
			if requests != 1 {
				t.Errorf("made %d requests, want exactly one", requests)
			}
			if stdout.Len() != 0 {
				t.Errorf("failure wrote success output: %s", stdout.String())
			}
			for _, expected := range []string{"[debug]", "API request: GET", "API response:", "method=" + test.method} {
				if !strings.Contains(stderr.String(), expected) {
					t.Errorf("missing %q in debug output: %s", expected, stderr.String())
				}
			}
			if strings.Contains(stderr.String()+err.Error(), "secret-") {
				t.Errorf("diagnostics leaked secrets: %s\n%v", stderr.String(), err)
			}
			store, storeErr := NewConfigStore(configPath, nil)
			if storeErr != nil {
				t.Fatal(storeErr)
			}
			_, profile, _, storeErr := store.Profile("diagnostic-test")
			if storeErr != nil || profile.URL != server.URL {
				t.Fatalf("saved profile unavailable: %+v %v", profile, storeErr)
			}
		})
	}
}

func TestDiagnosticURL(t *testing.T) {
	got := diagnosticURL("https://user:secret-password@example.com/api/files?token=secret-token#secret-fragment")
	if got != "https://example.com/api/files" {
		t.Fatalf("unsafe diagnostic URL: %s", got)
	}
}

func TestLoginSuccessAndDebugOptIn(t *testing.T) {
	keyring.MockInit()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Item{}})
	}))
	defer server.Close()
	for _, flags := range [][]string{nil, {"--debug"}, {"--log-level", "debug"}} {
		root := NewRootCommand("test")
		var stdout, stderr strings.Builder
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		args := []string{"login", "--url", server.URL, "--method", "none", "--name", "success-test", "--config", filepath.Join(t.TempDir(), "config.yaml")}
		root.SetArgs(append(args, flags...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "is ready") {
			t.Fatalf("missing success message: %s", stdout.String())
		}
		if got, want := strings.Contains(stderr.String(), "API response: 200 OK"), len(flags) > 0; got != want {
			t.Fatalf("flags %v: debug enabled=%t, want %t: %s", flags, got, want, stderr.String())
		}
	}
}
