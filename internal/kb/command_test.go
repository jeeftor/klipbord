package kb

import (
	"encoding/base64"
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
