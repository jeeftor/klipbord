package main

import (
	"os"

	"github.com/jeeftor/klipbord/internal/kb"
)

var version = "dev"

func main() {
	if err := kb.NewRootCommand(version).Execute(); err != nil {
		os.Exit(1)
	}
}
