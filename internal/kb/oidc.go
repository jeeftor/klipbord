package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type oidcDiscovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
}

// DeviceLogin completes an OIDC device authorization flow and returns refreshable credentials.
func DeviceLogin(ctx context.Context, httpClient *http.Client, profile Profile, notify func(string)) (Credentials, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	discovery, err := discoverOIDC(ctx, httpClient, profile.Issuer)
	if err != nil {
		return Credentials{}, err
	}
	if discovery.DeviceAuthorizationEndpoint == "" {
		return Credentials{}, errors.New("OIDC issuer does not advertise device authorization; configure a device-code flow or use bearer login")
	}
	scopes := profile.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "offline_access"}
	}
	values := url.Values{"client_id": {profile.ClientID}, "scope": {strings.Join(scopes, " ")}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.DeviceAuthorizationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return Credentials{}, fmt.Errorf("create device authorization request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := httpClient.Do(request)
	if err != nil {
		return Credentials{}, fmt.Errorf("start device authorization: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Credentials{}, oidcHTTPError(response)
	}
	var device deviceAuthorizationResponse
	if err := json.NewDecoder(response.Body).Decode(&device); err != nil {
		return Credentials{}, fmt.Errorf("decode device authorization response: %w", err)
	}
	if device.DeviceCode == "" || device.VerificationURI == "" {
		return Credentials{}, errors.New("OIDC device authorization response is incomplete")
	}
	verificationURL := device.VerificationURIComplete
	if verificationURL == "" {
		verificationURL = device.VerificationURI
	}
	notify(fmt.Sprintf("Open %s and complete login using code %s", verificationURL, device.UserCode))
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		case <-time.After(interval):
		}
		credentials, pending, err := exchangeDeviceCode(ctx, httpClient, discovery.TokenEndpoint, profile.ClientID, device.DeviceCode)
		if err != nil {
			return Credentials{}, err
		}
		if pending {
			continue
		}
		return credentials, nil
	}
	return Credentials{}, errors.New("OIDC device authorization expired before login completed")
}

func (client *Client) ensureToken(ctx context.Context) error {
	if client.profile.Method != "oidc" || client.credentials.ExpiresAt == 0 || time.Now().Unix() < client.credentials.ExpiresAt-30 {
		return nil
	}
	if client.credentials.RefreshToken == "" {
		return errors.New("OIDC access token expired; run kb login again")
	}
	discovery, err := discoverOIDC(ctx, client.httpClient, client.profile.Issuer)
	if err != nil {
		return err
	}
	values := url.Values{
		"client_id":     {client.profile.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {client.credentials.RefreshToken},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("create OIDC refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("refresh OIDC token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return oidcHTTPError(response)
	}
	var token tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode refreshed OIDC token: %w", err)
	}
	if token.AccessToken == "" {
		return errors.New("OIDC refresh response did not include an access token")
	}
	client.credentials.Token = token.AccessToken
	if token.RefreshToken != "" {
		client.credentials.RefreshToken = token.RefreshToken
	}
	client.credentials.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
	return client.store.secrets.Set(client.name, client.credentials)
}

func discoverOIDC(ctx context.Context, httpClient *http.Client, issuer string) (oidcDiscovery, error) {
	if issuer == "" {
		return oidcDiscovery{}, errors.New("OIDC issuer is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscovery{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return oidcDiscovery{}, fmt.Errorf("fetch OIDC discovery document: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return oidcDiscovery{}, oidcHTTPError(response)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(response.Body).Decode(&discovery); err != nil {
		return oidcDiscovery{}, fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if discovery.TokenEndpoint == "" {
		return oidcDiscovery{}, errors.New("OIDC discovery document does not include a token endpoint")
	}
	return discovery, nil
}

func exchangeDeviceCode(ctx context.Context, httpClient *http.Client, endpoint, clientID, deviceCode string) (Credentials, bool, error) {
	values := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return Credentials{}, false, fmt.Errorf("create device token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := httpClient.Do(request)
	if err != nil {
		return Credentials{}, false, fmt.Errorf("poll device authorization: %w", err)
	}
	defer response.Body.Close()
	var token tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return Credentials{}, false, fmt.Errorf("decode device token response: %w", err)
	}
	if token.Error == "authorization_pending" {
		return Credentials{}, true, nil
	}
	if response.StatusCode != http.StatusOK || token.Error != "" {
		return Credentials{}, false, fmt.Errorf("OIDC device authorization failed: %s", token.Error)
	}
	if token.AccessToken == "" {
		return Credentials{}, false, errors.New("OIDC device authorization did not include an access token")
	}
	return Credentials{
		Token:        token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix(),
	}, false, nil
}

func oidcHTTPError(response *http.Response) error {
	var body tokenResponse
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body.Error != "" {
		return fmt.Errorf("OIDC request failed: %s", body.Error)
	}
	return fmt.Errorf("OIDC request returned %s", response.Status)
}
