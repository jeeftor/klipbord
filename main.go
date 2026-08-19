package main

import (
	"os"

	"github.com/jeeftor/klipbord/internal/app"
	"github.com/jeeftor/klipbord/internal/webassets"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newServerCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

// newServerCommand creates the command-line interface for the Klipbord server.
func newServerCommand() *cobra.Command {
	var port string
	var dataDir string
	var baseURL string
	var maxUploadMB string
	var visionEnabled bool

	command := &cobra.Command{
		Use:          "kb-server",
		Short:        "Run the Klipbord server",
		Long:         "Run the Klipbord server with embedded web assets and file-backed storage.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Version:      version,
		Run: func(cmd *cobra.Command, _ []string) {
			setFlagEnvironment(cmd, "port", "PORT", port)
			setFlagEnvironment(cmd, "data-dir", "DATA_DIR", dataDir)
			setFlagEnvironment(cmd, "base-url", "BASE_URL", baseURL)
			setFlagEnvironment(cmd, "max-upload-mb", "MAX_UPLOAD_MB", maxUploadMB)
			if cmd.Flags().Changed("vision-enabled") {
				if visionEnabled {
					_ = os.Setenv("VISION_ENABLED", "true")
				} else {
					_ = os.Setenv("VISION_ENABLED", "false")
				}
			}
			app.Run(version, webassets.Embedded())
		},
	}

	command.Flags().StringVar(&port, "port", "", "HTTP port (default: 8080; overrides PORT)")
	command.Flags().StringVar(&dataDir, "data-dir", "", "Storage directory (default: ./data; overrides DATA_DIR)")
	command.Flags().StringVar(&baseURL, "base-url", "", "Public URL for generated links (overrides BASE_URL)")
	command.Flags().StringVar(&maxUploadMB, "max-upload-mb", "", "Maximum upload size in MB (overrides MAX_UPLOAD_MB)")
	command.Flags().BoolVar(&visionEnabled, "vision-enabled", true, "Enable automatic image analysis (overrides VISION_ENABLED)")

	return command
}

// setFlagEnvironment applies an explicitly provided command flag to the runtime environment.
func setFlagEnvironment(command *cobra.Command, flagName, environmentName, value string) {
	if command.Flags().Changed(flagName) {
		_ = os.Setenv(environmentName, value)
	}
}
