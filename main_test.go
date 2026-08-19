package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestServerCommandHelp(t *testing.T) {
	command := newServerCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--help"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}

	for _, flag := range []string{"--base-url", "--data-dir", "--port", "--version"} {
		if !strings.Contains(output.String(), flag) {
			t.Errorf("help output does not contain %q", flag)
		}
	}
}
