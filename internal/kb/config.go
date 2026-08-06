package kb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

const keyringService = "klipbord"

// Profile describes a non-secret Klipbord connection profile.
type Profile struct {
	URL      string   `mapstructure:"url" yaml:"url"`
	Method   string   `mapstructure:"method" yaml:"method"`
	Issuer   string   `mapstructure:"issuer,omitempty" yaml:"issuer,omitempty"`
	ClientID string   `mapstructure:"client_id,omitempty" yaml:"client_id,omitempty"`
	Scopes   []string `mapstructure:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// Config holds local, non-secret client settings.
type Config struct {
	ActiveProfile string             `mapstructure:"active_profile" yaml:"active_profile"`
	Profiles      map[string]Profile `mapstructure:"profiles" yaml:"profiles"`
}

// Credentials are held in the OS keychain, never in the config file.
type Credentials struct {
	Token        string            `json:"token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    int64             `json:"expires_at,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

// SecretStore isolates OS keychain access for tests.
type SecretStore interface {
	Delete(profile string) error
	Get(profile string) (Credentials, error)
	Set(profile string, credentials Credentials) error
}

type keyringStore struct{}

func (keyringStore) Delete(profile string) error {
	err := keyring.Delete(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (keyringStore) Get(profile string) (Credentials, error) {
	value, err := keyring.Get(keyringService, profile)
	if err != nil {
		return Credentials{}, err
	}
	var credentials Credentials
	if err := json.Unmarshal([]byte(value), &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode keychain credentials: %w", err)
	}
	return credentials, nil
}

func (keyringStore) Set(profile string, credentials Credentials) error {
	value, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode keychain credentials: %w", err)
	}
	return keyring.Set(keyringService, profile, string(value))
}

// ConfigStore manages profiles without mixing credentials into the config file.
type ConfigStore struct {
	path    string
	secrets SecretStore
}

// NewConfigStore creates a profile store at path. An empty path uses the user config directory.
func NewConfigStore(path string, secrets SecretStore) (*ConfigStore, error) {
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("find user config directory: %w", err)
		}
		path = filepath.Join(configDir, "klipbord", "config.yaml")
	}
	if secrets == nil {
		secrets = keyringStore{}
	}
	return &ConfigStore{path: path, secrets: secrets}, nil
}

// Load returns persisted profile metadata or an empty configuration.
func (store *ConfigStore) Load() (Config, error) {
	config := Config{Profiles: map[string]Profile{}}
	if _, err := os.Stat(store.path); errors.Is(err, os.ErrNotExist) {
		return config, nil
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}
	v := viper.New()
	v.SetConfigFile(store.path)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := v.Unmarshal(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]Profile{}
	}
	return config, nil
}

// Save persists profile metadata with owner-only access.
func (store *ConfigStore) Save(config Config) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	v := viper.New()
	v.Set("active_profile", config.ActiveProfile)
	v.Set("profiles", config.Profiles)
	if err := v.WriteConfigAs(store.path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(store.path, 0600)
}

// Profile returns the selected profile and its keychain credentials.
func (store *ConfigStore) Profile(name string) (string, Profile, Credentials, error) {
	config, err := store.Load()
	if err != nil {
		return "", Profile{}, Credentials{}, err
	}
	if name == "" {
		name = config.ActiveProfile
	}
	if name == "" {
		return "", Profile{}, Credentials{}, errors.New("no profile configured; run kb login")
	}
	profile, ok := config.Profiles[name]
	if !ok {
		return "", Profile{}, Credentials{}, fmt.Errorf("profile %q not found", name)
	}
	credentials, err := store.secrets.Get(name)
	if profile.Method == "none" {
		err = nil
	}
	if err != nil {
		return "", Profile{}, Credentials{}, fmt.Errorf("load credentials for %q: %w", name, err)
	}
	return name, profile, credentials, nil
}

// SaveProfile writes profile metadata and its matching secret record.
func (store *ConfigStore) SaveProfile(name string, profile Profile, credentials Credentials) error {
	if err := validateProfile(profile); err != nil {
		return err
	}
	config, err := store.Load()
	if err != nil {
		return err
	}
	config.Profiles[name] = profile
	if config.ActiveProfile == "" {
		config.ActiveProfile = name
	}
	if profile.Method == "none" {
		if err := store.secrets.Delete(name); err != nil {
			return fmt.Errorf("clear credentials from OS keychain: %w", err)
		}
	} else if err := store.secrets.Set(name, credentials); err != nil {
		return fmt.Errorf("save credentials in OS keychain: %w", err)
	}
	return store.Save(config)
}

// DeleteProfile removes both local metadata and its keychain record.
func (store *ConfigStore) DeleteProfile(name string) error {
	config, err := store.Load()
	if err != nil {
		return err
	}
	if _, ok := config.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	delete(config.Profiles, name)
	if config.ActiveProfile == name {
		config.ActiveProfile = ""
		for candidate := range config.Profiles {
			config.ActiveProfile = candidate
			break
		}
	}
	if err := store.secrets.Delete(name); err != nil {
		return fmt.Errorf("delete credentials from OS keychain: %w", err)
	}
	return store.Save(config)
}

// SetActiveProfile selects the default profile used when --profile is omitted.
func (store *ConfigStore) SetActiveProfile(name string) error {
	config, err := store.Load()
	if err != nil {
		return err
	}
	if _, ok := config.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	config.ActiveProfile = name
	return store.Save(config)
}

func validateProfile(profile Profile) error {
	parsed, err := url.Parse(profile.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("profile URL must be an absolute URL")
	}
	if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("profile URL must use HTTPS unless it is a loopback address")
	}
	switch profile.Method {
	case "none", "bearer", "cloudflare", "headers", "oidc":
	default:
		return fmt.Errorf("unsupported login method %q", profile.Method)
	}
	if profile.Method == "oidc" && (profile.Issuer == "" || profile.ClientID == "") {
		return errors.New("OIDC login requires an issuer and client ID")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
