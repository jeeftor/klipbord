package kb

import (
	"strings"
	"testing"
)

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
