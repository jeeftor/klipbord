package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item is the subset of Klipbord item metadata presented by the CLI.
type Item struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	Created    time.Time `json:"created"`
	Persistent bool      `json:"persistent"`
	URL        string    `json:"url"`
}

// CreatedItem is returned after creating text or file items.
type CreatedItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Client calls Klipbord's existing REST API with a selected profile.
type Client struct {
	credentials Credentials
	httpClient  *http.Client
	name        string
	profile     Profile
	store       *ConfigStore
}

// NewClient loads a profile and creates an API client.
func NewClient(store *ConfigStore, profileName string, httpClient *http.Client) (*Client, error) {
	name, profile, credentials, err := store.Profile(profileName)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{credentials: credentials, httpClient: httpClient, name: name, profile: profile, store: store}, nil
}

// CreateText creates a text snippet from standard input.
func (client *Client) CreateText(ctx context.Context, content, name, ttl string, persistent bool) (CreatedItem, error) {
	body, err := json.Marshal(map[string]string{"content": content, "name": name, "ttl": ttl})
	if err != nil {
		return CreatedItem{}, fmt.Errorf("encode text: %w", err)
	}
	var created CreatedItem
	if err := client.doJSON(ctx, http.MethodPost, "/api/text", bytes.NewReader(body), "application/json", &created); err != nil {
		return CreatedItem{}, err
	}
	if persistent {
		if err := client.SetPersistent(ctx, created.ID, true); err != nil {
			return CreatedItem{}, err
		}
	}
	return created, nil
}

// UploadFile uploads one local file through the multipart API.
func (client *Client) UploadFile(ctx context.Context, path, name, ttl string, persistent bool) (CreatedItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return CreatedItem{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()
	if name == "" {
		name = filepath.Base(path)
	}
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		defer writer.Close()
		part, err := form.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, file)
		}
		if err == nil {
			err = form.WriteField("ttl", ttl)
		}
		if err == nil {
			err = form.Close()
		}
		if err != nil {
			_ = writer.CloseWithError(fmt.Errorf("stream upload %q: %w", path, err))
		}
	}()
	var created CreatedItem
	if err := client.doJSON(ctx, http.MethodPost, "/api/upload", reader, form.FormDataContentType(), &created); err != nil {
		return CreatedItem{}, err
	}
	if persistent {
		if err := client.SetPersistent(ctx, created.ID, true); err != nil {
			return CreatedItem{}, err
		}
	}
	return created, nil
}

// List returns items, optionally limited to persistent items.
func (client *Client) List(ctx context.Context, persistent *bool) ([]Item, error) {
	path := "/api/files"
	if persistent != nil {
		path += "?persistent=" + fmt.Sprint(*persistent)
	}
	var response struct {
		Items []Item `json:"items"`
	}
	if err := client.doJSON(ctx, http.MethodGet, path, nil, "", &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// Get downloads text to stdout or a binary file to outputPath.
func (client *Client) Get(ctx context.Context, id, outputPath string, stdout io.Writer) error {
	items, err := client.List(ctx, nil)
	if err != nil {
		return err
	}
	var item Item
	found := false
	for _, candidate := range items {
		if candidate.ID == id {
			item = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("item %q not found", id)
	}
	path := "/api/files/" + url.PathEscape(id)
	if item.Type == "text" {
		path = "/api/text/" + url.PathEscape(id)
	}
	response, err := client.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if outputPath == "-" || item.Type == "text" && outputPath == "" {
		_, err := io.Copy(stdout, response.Body)
		return err
	}
	if outputPath == "" {
		outputPath = item.Name
	}
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("refusing to overwrite %q; use --output", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check output path: %w", err)
	}
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create %q: %w", outputPath, err)
	}
	defer output.Close()
	_, err = io.Copy(output, response.Body)
	return err
}

// SetPersistent pins or unpins an item.
func (client *Client) SetPersistent(ctx context.Context, id string, persistent bool) error {
	body, err := json.Marshal(map[string]bool{"persistent": persistent})
	if err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPatch, "/api/files/"+url.PathEscape(id), bytes.NewReader(body), "application/json", nil)
}

// Delete removes an item.
func (client *Client) Delete(ctx context.Context, id string) error {
	return client.doJSON(ctx, http.MethodDelete, "/api/files/"+url.PathEscape(id), nil, "", nil)
}

func (client *Client) doJSON(ctx context.Context, method, path string, body io.Reader, contentType string, target any) error {
	response, err := client.do(ctx, method, path, body, contentType)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (client *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	if err := client.ensureToken(ctx); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(client.profile.URL, "/")+path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for name, value := range client.credentials.Headers {
		request.Header.Set(name, value)
	}
	if client.profile.Method == "bearer" || client.profile.Method == "oidc" {
		request.Header.Set("Authorization", "Bearer "+client.credentials.Token)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Klipbord returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return response, nil
}
