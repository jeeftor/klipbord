package kb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// latestReleaseURL is the GitHub "latest release" page that redirects to the
// tagged release (e.g. https://github.com/jeeftor/klipbord/releases/tag/v1.2.3).
const latestReleaseURL = "https://github.com/jeeftor/klipbord/releases/latest"

// installScriptURL points at the install.sh asset attached to the latest
// release, suitable for piping directly into a shell.
const installScriptURL = "https://github.com/jeeftor/klipbord/releases/latest/download/install.sh"

// checkLatestVersion resolves the latest release tag name by following the
// /releases/latest redirect. The returned tag includes its leading "v" (for
// example "v1.2.3"), matching the behaviour of install.sh.
func checkLatestVersion(ctx context.Context, httpClient *http.Client, userAgent string) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("create version check request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	// The final request URL ends with .../releases/tag/<tag-name>.
	final := resp.Request.URL.String()
	if idx := strings.LastIndex(final, "/releases/tag/"); idx >= 0 {
		return final[idx+len("/releases/tag/"):], nil
	}
	return "", fmt.Errorf("could not determine latest release tag from %s", final)
}

// compareVersions reports whether latest is newer than current. Both values
// may include an optional leading "v". A non-semver current version (such as
// "dev") is always considered older than any real release.
func compareVersions(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")
	if current == "" || current == "dev" {
		return latest != "" && latest != "dev"
	}
	if latest == "" || latest == "dev" {
		return false
	}
	var cMajor, cMinor, cPatch, lMajor, lMinor, lPatch int
	if _, err := fmt.Sscanf(current, "%d.%d.%d", &cMajor, &cMinor, &cPatch); err != nil {
		return true
	}
	if _, err := fmt.Sscanf(latest, "%d.%d.%d", &lMajor, &lMinor, &lPatch); err != nil {
		return false
	}
	if lMajor != cMajor {
		return lMajor > cMajor
	}
	if lMinor != cMinor {
		return lMinor > cMinor
	}
	return lPatch > cPatch
}

// runInstallScript downloads install.sh and pipes it into sh, mirroring the
// documented one-liner: curl -fsSL <url> | sh
func runInstallScript(ctx context.Context, stdout, stderr io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, installScriptURL, nil)
	if err != nil {
		return fmt.Errorf("create install request: %w", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download install.sh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("install.sh returned %s", resp.Status)
	}
	return pipeToShell(ctx, resp.Body, stdout, stderr)
}
