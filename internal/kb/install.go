package kb

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

// pipeToShell streams the given input into "sh", forwarding stdout/stderr to
// the provided writers. It mirrors `curl ... | sh`.
func pipeToShell(ctx context.Context, input io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh")
	cmd.Stdin = input
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run install.sh: %w", err)
	}
	return nil
}
