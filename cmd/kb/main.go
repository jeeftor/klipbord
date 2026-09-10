package main

import (
	"os"

	"github.com/jeeftor/klipbord/internal/kb"
)

var version = "dev"

func main() {
	command := kb.NewRootCommand(version)
	command.SilenceErrors = true
	if err := command.Execute(); err != nil {
		kb.PrintError(command.ErrOrStderr(), err)
		os.Exit(1)
	}
}
