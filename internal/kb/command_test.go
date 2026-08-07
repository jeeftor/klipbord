package kb

import (
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
