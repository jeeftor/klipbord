package kb

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// diagnosticURL excludes URL credentials, query parameters, and fragments from logs.
func diagnosticURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, parsed.String())
}

// apiError keeps request context without echoing untrusted response bodies.
type apiError struct {
	method     string
	endpoint   string
	statusCode int
	emptyBody  bool
}

func (err *apiError) Error() string {
	body := "response body omitted to protect credentials"
	if err.emptyBody {
		body = "empty response body"
	}
	return fmt.Sprintf("%s %s: Klipbord returned %d %s (%s)", err.method, err.endpoint, err.statusCode, http.StatusText(err.statusCode), body)
}

// loginConnectionError pairs the failed probe with recovery steps for the selected method.
func loginConnectionError(name string, profile Profile, err error) error {
	hint := "Check your server URL, network connection, and proxy configuration."
	var response *apiError
	if errors.As(err, &response) {
		switch response.statusCode {
		case http.StatusUnauthorized:
			switch profile.Method {
			case "authentik-app-password":
				hint = "Check your Authentik username and App Password (not an API token), your application's access policies, and your proxy provider's Intercept header authentication setting. KB_PASSWORD takes precedence over AUTHENTIK_APP_PASSWORD and APP_PASSWORD."
			case "none":
				hint = "Your server or proxy requires authentication, but this profile sends no credentials. Run login again with the appropriate --method."
			case "oidc":
				hint = "Your server or proxy rejected the OIDC access token. Check your issuer, client ID, token audience, and proxy bearer authentication configuration."
			case "bearer":
				hint = "Check that your bearer token is valid, has not expired, and is accepted by your server or proxy."
			case "cloudflare":
				hint = "Check your Cloudflare Access service token and the application's Service Auth policy."
			case "headers":
				hint = "Check your configured header names and credentials, and whether your proxy forwards and accepts them."
			}
		case http.StatusForbidden:
			hint = "Your server or proxy denied access. Check your account permissions and access policy; a 403 does not prove that your credentials were accepted."
		case http.StatusNotFound:
			hint = "Your server URL did not resolve to the Klipbord API. Check your base URL and reverse-proxy route."
		case http.StatusTooManyRequests:
			hint = "Your server or proxy is rate limiting requests. Wait before retrying."
		default:
			if response.statusCode >= 300 && response.statusCode < 400 {
				hint = "Your API request was redirected. Check your canonical server URL and configure your proxy for CLI authentication if it redirects to a browser login page."
			} else if response.statusCode >= 500 {
				hint = "Your server or proxy reported a server error. Check your Klipbord service and proxy logs."
			}
		}
	}
	return fmt.Errorf("saved profile %q (method=%s), but connection test failed: %w\n%s\nYour profile remains saved; saving it does not confirm authentication. Run login again with --debug for request details", name, profile.Method, err, hint)
}
