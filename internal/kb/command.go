package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type commandState struct {
	configPath string
	profile    string
	version    string
}

// NewRootCommand creates the kb command-line client.
func NewRootCommand(version string) *cobra.Command {
	state := commandState{version: version}
	root := &cobra.Command{
		Use:           "kb [FILE...]",
		Short:         "Upload and manage items in Klipbord",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, paths []string) error {
			return state.upload(command.Context(), command.OutOrStdout(), command.ErrOrStderr(), paths, command.Flags())
		},
	}
	root.PersistentFlags().StringVar(&state.configPath, "config", "", "config file path")
	root.PersistentFlags().StringVarP(&state.profile, "profile", "p", "", "connection profile")
	root.Flags().String("name", "", "item name")
	root.Flags().String("ttl", "7d", "expiration: 1h, 1d, 7d, 30d, or never")
	root.Flags().Bool("persistent", false, "make uploaded items persistent")
	root.Flags().Bool("json", false, "write JSON output")
	root.Version = version
	root.AddCommand(state.loginCommand(), state.logoutCommand(), state.profileCommand(), state.listCommand(), state.getCommand(), state.pinCommand(true), state.pinCommand(false), state.deleteCommand())
	return root
}

func (state commandState) store() (*ConfigStore, error) {
	return NewConfigStore(state.configPath, nil)
}

func (state commandState) client() (*Client, error) {
	store, err := state.store()
	if err != nil {
		return nil, err
	}
	return NewClient(store, state.profile, nil)
}

func (state commandState) upload(ctx context.Context, stdout, stderr io.Writer, paths []string, flags interface {
	GetBool(string) (bool, error)
	GetString(string) (string, error)
}) error {
	client, err := state.client()
	if err != nil {
		return err
	}
	ttl, _ := flags.GetString("ttl")
	name, _ := flags.GetString("name")
	persistent, _ := flags.GetBool("persistent")
	jsonOutput, _ := flags.GetBool("json")
	if len(paths) == 0 {
		if terminalInput() {
			return errors.New("provide a file path or pipe text to kb")
		}
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read standard input: %w", err)
		}
		if len(content) == 0 {
			return errors.New("standard input is empty")
		}
		created, err := client.CreateText(ctx, string(content), name, ttl, persistent)
		if err != nil {
			return err
		}
		return writeCreated(stdout, created, jsonOutput)
	}
	if name != "" && len(paths) != 1 {
		return errors.New("--name can only be used with one file")
	}
	created := make([]CreatedItem, 0, len(paths))
	for _, path := range paths {
		itemName := ""
		if len(paths) == 1 {
			itemName = name
		}
		item, err := client.UploadFile(ctx, path, itemName, ttl, persistent)
		if err != nil {
			return err
		}
		created = append(created, item)
	}
	if jsonOutput {
		return json.NewEncoder(stdout).Encode(created)
	}
	for _, item := range created {
		if _, err := fmt.Fprintln(stdout, item.URL); err != nil {
			return err
		}
	}
	return nil
}

func (state commandState) loginCommand() *cobra.Command {
	var method, profileName, profileURL, issuer, clientID string
	var scopes, headerNames []string
	command := &cobra.Command{
		Use:   "login",
		Short: "Create or update a secure connection profile",
		RunE: func(command *cobra.Command, _ []string) error {
			if profileName == "" {
				profileName = "default"
			}
			profile := Profile{URL: profileURL, Method: method, Issuer: issuer, ClientID: clientID, Scopes: scopes}
			credentials, err := loginCredentials(command.Context(), command.ErrOrStderr(), profile, headerNames)
			if err != nil {
				return err
			}
			store, err := state.store()
			if err != nil {
				return err
			}
			if err := store.SaveProfile(profileName, profile, credentials); err != nil {
				return err
			}
			client, err := NewClient(store, profileName, nil)
			if err != nil {
				return err
			}
			if _, err := client.List(command.Context(), nil); err != nil {
				return fmt.Errorf("saved profile but connection test failed: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Profile %q is ready.\n", profileName)
			return err
		},
	}
	command.Flags().StringVar(&profileName, "name", "default", "profile name")
	command.Flags().StringVar(&profileURL, "url", "", "Klipbord base URL")
	command.Flags().StringVar(&method, "method", "", "login method: none, bearer, cloudflare, headers, or oidc")
	command.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL")
	command.Flags().StringVar(&clientID, "client-id", "", "OIDC public client ID")
	command.Flags().StringSliceVar(&scopes, "scope", []string{"openid", "profile", "offline_access"}, "OIDC scopes")
	command.Flags().StringSliceVar(&headerNames, "header", nil, "custom header name; repeat for each header")
	_ = command.MarkFlagRequired("url")
	_ = command.MarkFlagRequired("method")
	return command
}

func (state commandState) logoutCommand() *cobra.Command {
	var profileName string
	command := &cobra.Command{
		Use:   "logout",
		Short: "Remove a connection profile and its credentials",
		RunE: func(command *cobra.Command, _ []string) error {
			if profileName == "" {
				profileName = state.profile
			}
			if profileName == "" {
				profileName = "default"
			}
			store, err := state.store()
			if err != nil {
				return err
			}
			if err := store.DeleteProfile(profileName); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Logged out of %q.\n", profileName)
			return err
		},
	}
	command.Flags().StringVar(&profileName, "name", "", "profile name")
	return command
}

func (state commandState) profileCommand() *cobra.Command {
	command := &cobra.Command{Use: "profile", Short: "Manage connection profiles"}
	command.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List configured profiles",
			RunE: func(command *cobra.Command, _ []string) error {
				store, err := state.store()
				if err != nil {
					return err
				}
				config, err := store.Load()
				if err != nil {
					return err
				}
				names := make([]string, 0, len(config.Profiles))
				for name := range config.Profiles {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					profile := config.Profiles[name]
					marker := " "
					if name == config.ActiveProfile {
						marker = "*"
					}
					if _, err := fmt.Fprintf(command.OutOrStdout(), "%s %s\t%s\t%s\n", marker, name, profile.Method, profile.URL); err != nil {
						return err
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "use NAME",
			Short: "Select the default profile",
			Args:  cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				store, err := state.store()
				if err != nil {
					return err
				}
				return store.SetActiveProfile(args[0])
			},
		},
	)
	return command
}

func (state commandState) listCommand() *cobra.Command {
	var persistent, jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List Klipbord items",
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := state.client()
			if err != nil {
				return err
			}
			var filter *bool
			if command.Flags().Changed("persistent") {
				filter = &persistent
			}
			items, err := client.List(command.Context(), filter)
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(command.OutOrStdout()).Encode(items)
			}
			for _, item := range items {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\n", item.ID, item.Type, item.Name); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&persistent, "persistent", false, "show only persistent items")
	command.Flags().BoolVar(&jsonOutput, "json", false, "write JSON output")
	return command
}

func (state commandState) getCommand() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "get ID",
		Short: "Download an item",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			client, err := state.client()
			if err != nil {
				return err
			}
			return client.Get(command.Context(), args[0], output, command.OutOrStdout())
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "output path; use - for stdout")
	return command
}

func (state commandState) pinCommand(persistent bool) *cobra.Command {
	name := "pin"
	short := "Make an item persistent"
	if !persistent {
		name = "unpin"
		short = "Restore normal expiration for an item"
	}
	return &cobra.Command{
		Use:   name + " ID",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			client, err := state.client()
			if err != nil {
				return err
			}
			return client.SetPersistent(command.Context(), args[0], persistent)
		},
	}
}

func (state commandState) deleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rm ID",
		Aliases: []string{"delete"},
		Short:   "Delete an item",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			client, err := state.client()
			if err != nil {
				return err
			}
			return client.Delete(command.Context(), args[0])
		},
	}
}

func loginCredentials(ctx context.Context, stderr io.Writer, profile Profile, headerNames []string) (Credentials, error) {
	switch profile.Method {
	case "none":
		return Credentials{}, nil
	case "bearer":
		token, err := secret("KB_TOKEN", "Bearer token: ")
		return Credentials{Token: token}, err
	case "cloudflare":
		clientID, err := secret("CF_ACCESS_CLIENT_ID", "Cloudflare Access client ID: ")
		if err != nil {
			return Credentials{}, err
		}
		clientSecret, err := secret("CF_ACCESS_CLIENT_SECRET", "Cloudflare Access client secret: ")
		if err != nil {
			return Credentials{}, err
		}
		return Credentials{Headers: map[string]string{
			"CF-Access-Client-Id":     clientID,
			"CF-Access-Client-Secret": clientSecret,
		}}, nil
	case "headers":
		if len(headerNames) == 0 {
			return Credentials{}, errors.New("headers login requires at least one --header name")
		}
		headers := make(map[string]string, len(headerNames))
		for _, name := range headerNames {
			if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n:") {
				return Credentials{}, fmt.Errorf("invalid header name %q", name)
			}
			value, err := secret("KB_HEADER_"+headerEnvName(name), name+": ")
			if err != nil {
				return Credentials{}, err
			}
			headers[name] = value
		}
		return Credentials{Headers: headers}, nil
	case "oidc":
		return DeviceLogin(ctx, nil, profile, func(message string) { _, _ = fmt.Fprintln(stderr, message) })
	default:
		return Credentials{}, fmt.Errorf("unsupported login method %q", profile.Method)
	}
}

func secret(environment, prompt string) (string, error) {
	if value := os.Getenv(environment); value != "" {
		return value, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	if len(value) == 0 {
		return "", errors.New("secret cannot be empty")
	}
	return string(value), nil
}

func headerEnvName(name string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToUpper(character)
		}
		return '_'
	}, name)
}

func terminalInput() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func writeCreated(writer io.Writer, created CreatedItem, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(writer).Encode(created)
	}
	_, err := fmt.Fprintln(writer, created.URL)
	return err
}
