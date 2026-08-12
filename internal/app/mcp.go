package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MCP JSON-RPC types
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type MCPToolResult struct {
	Content []MCPContent `json:"content"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var mcpTools = []MCPTool{
	{
		Name:        "list_files",
		Description: "List all pasted files and text snippets with metadata (id, name, type, size, created, expires, persistent, url). Use persistent=true to list only persistent items.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"persistent": map[string]any{
					"type":        "boolean",
					"description": "If true, list only persistent items. If false, list only non-persistent items. If omitted, list all.",
				},
			},
		},
	},
	{
		Name:        "get_file",
		Description: "Get the content of a file or text snippet by ID. Returns base64-encoded content for files, plain text for text snippets.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the item",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "get_file_url",
		Description: "Get the public share URL for a file or text snippet by ID. Anyone with this URL can access the item.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the item",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "upload_file",
		Description: "Upload a named file with base64-encoded content. Use this for artifacts that must retain their filename and MIME type, including YAML, JSON, scripts, and configuration files. Returns the item ID and public URL.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{
					"type":        "string",
					"description": "Filename to preserve and use in the share URL and downloads, for example config.yaml",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Base64-encoded file content",
				},
				"mime_type": map[string]any{
					"type":        "string",
					"description": "Optional MIME type. Provide application/yaml for YAML when precise content serving matters; otherwise it is detected from the filename and content.",
				},
				"ttl": map[string]any{
					"type":        "string",
					"description": "Time to live: 1h, 1d, 7d, 30d, or never (default: 7d)",
				},
				"persistent": map[string]any{
					"type":        "boolean",
					"description": "If true, item will never expire (default: false)",
				},
			},
			"required": []string{"filename", "content"},
		},
	},
	{
		Name:        "create_text",
		Description: "Create a plain-text note or short snippet, always served as text/plain. Do not use this for named artifacts such as YAML, JSON, scripts, or configuration files; use upload_file instead. Returns the item ID and public URL.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "The text content",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Optional display and download name only; it does not change the text/plain content type",
				},
				"ttl": map[string]any{
					"type":        "string",
					"description": "Time to live: 1h, 1d, 7d, 30d, or never (default: 7d)",
				},
				"persistent": map[string]any{
					"type":        "boolean",
					"description": "If true, item will never expire (default: false)",
				},
			},
			"required": []string{"content"},
		},
	},
	{
		Name:        "get_text",
		Description: "Get the raw text content of a text snippet by ID.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the text snippet",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "delete_file",
		Description: "Permanently delete a file or text snippet by ID. This removes its stored content and cannot be undone.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the item to delete",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "persist_file",
		Description: "Pin or unpin an item to keep it forever (persistent items never expire).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the item",
				},
				"persistent": map[string]any{
					"type":        "boolean",
					"description": "true to pin (keep forever), false to unpin (restore default TTL)",
				},
			},
			"required": []string{"id", "persistent"},
		},
	},
	{
		Name:        "describe_image",
		Description: "Get the vision analysis (extracted text and description) for an image item. Returns the stored analysis if available. Optionally specify a prompt name to get a specific analysis.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the image item",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Optional: prompt name (e.g. 'default', 'terminal', 'code', 'document', 'diagram', or a custom prompt). If omitted, returns all analyses.",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "analyze_image",
		Description: "Run vision analysis for an image item using the configured vision provider. This can incur provider cost and saves or replaces the analysis for the selected prompt. Extracts text and generates a description; optionally specify a prompt name.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the image item",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Optional: prompt name (e.g. 'default', 'terminal', 'code', 'document', 'diagram', or a custom prompt). Defaults to 'default'.",
				},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "inspect_image",
		Description: "Ask a focused visual question about an image using the configured vision provider. This can incur provider cost and saves a new inspection analysis. Returns structured visible evidence using ui, ocr, code, document, diagram, or describe mode.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The image item ID",
				},
				"mode": map[string]any{
					"type":        "string",
					"description": "Inspection mode: ui, ocr, code, document, diagram, or describe",
				},
				"question": map[string]any{
					"type":        "string",
					"description": "The specific visual question to answer from visible evidence",
				},
			},
			"required": []string{"id", "mode", "question"},
		},
	},
	{
		Name:        "list_prompts",
		Description: "List all available vision prompt templates (built-in and custom).",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "create_prompt",
		Description: "Create a persistent custom vision prompt template. The prompt text instructs the vision model how to analyze images.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Unique name for the prompt",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Short description of what the prompt does",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "The prompt text sent to the vision model",
				},
			},
			"required": []string{"name", "prompt"},
		},
	},
	{
		Name:        "update_prompt",
		Description: "Persistently update an existing vision prompt template (built-in or custom).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name of the prompt to update",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "New description (optional)",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "New prompt text (optional)",
				},
			},
			"required": []string{"name"},
		},
	},
	{
		Name:        "delete_prompt",
		Description: "Delete a custom vision prompt template. Built-in prompts cannot be deleted.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name of the prompt to delete",
				},
			},
			"required": []string{"name"},
		},
	},
	{
		Name:        "list_vision_presets",
		Description: "List all configured vision LLM presets (endpoints, models, active selection).",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "set_vision_preset",
		Description: "Persistently switch the server-wide active vision LLM preset. Fails if environment variables override configuration.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"preset": map[string]any{
					"type":        "string",
					"description": "Name of the preset to activate",
				},
			},
			"required": []string{"preset"},
		},
	},
	{
		Name:        "test_vision_preset",
		Description: "Send a minimal request to test connectivity to a vision LLM preset. This invokes the configured provider and can incur provider cost. Returns success, message, and latency.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"preset": map[string]any{
					"type":        "string",
					"description": "Name of the preset to test (omit to test the active preset)",
				},
			},
		},
	},
	{
		Name:        "test_vision",
		Description: "Run the full vision pipeline on a built-in sample image. This invokes the configured provider and can incur provider cost; it tests image upload, model analysis, and text extraction. Optionally specify image_type to match terminal, code, document, or diagram. Returns extracted text, image type, and latency.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"image_type": map[string]any{
					"type":        "string",
					"description": "Sample image type: terminal (default), code, document, or diagram. Uses the matching prompt template.",
				},
			},
		},
	},
	{
		Name:        "compare_vision",
		Description: "Run an image through every configured vision preset, compare results, and rank quality. This can make multiple provider calls and incur substantial cost; pairwise LLM judging can add another call. Returns ranked results with scores and rationale.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"image_type": map[string]any{
					"type":        "string",
					"description": "Sample image type: terminal (default), code, document, or diagram. Ignored if item_id is provided.",
				},
				"item_id": map[string]any{
					"type":        "string",
					"description": "Existing image item ID to compare. If omitted, uses a sample image.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Prompt name to use for analysis (default: 'default')",
				},
			},
		},
	},
	{
		Name:        "compare_prompts",
		Description: "Run one image through every available vision prompt and rank quality. This can make multiple provider calls and incur substantial cost. Returns ranked results with scores, character counts, and extracted text.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"image_type": map[string]any{
					"type":        "string",
					"description": "Sample image type: terminal (default), code, document, diagram, or screenshot. Ignored if item_id is provided.",
				},
				"item_id": map[string]any{
					"type":        "string",
					"description": "Existing image item ID to test. If omitted, uses a sample image.",
				},
			},
		},
	},
}

func mcpHandler(w http.ResponseWriter, r *http.Request) {
	// Handle SSE/streaming for MCP HTTP transport
	if r.Method == http.MethodGet {
		// Return server capabilities for SSE connection
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		// Send endpoint event
		fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
		flusher.Flush()
		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case "initialize":
		writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "klipbord",
				"version": "1.0.0",
			},
		})

	case "tools/list":
		writeMCPResult(w, req.ID, map[string]any{
			"tools": mcpTools,
		})

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeMCPError(w, req.ID, -32602, "Invalid params")
			return
		}
		result, mcpErr := handleMCPToolCall(params.Name, params.Arguments)
		if mcpErr != nil {
			writeMCPError(w, req.ID, mcpErr.Code, mcpErr.Message)
			return
		}
		writeMCPResult(w, req.ID, result)

	case "ping":
		writeMCPResult(w, req.ID, map[string]any{})

	default:
		writeMCPError(w, req.ID, -32601, "Method not found: "+req.Method)
	}
}

func handleMCPToolCall(name string, args map[string]any) (interface{}, *MCPError) {
	switch name {
	case "list_files":
		persistent, hasPersistent := args["persistent"].(bool)
		if hasPersistent {
			return mcpListFilesFiltered(persistent)
		}
		return mcpListFiles()

	case "get_file":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		return mcpGetFile(id)

	case "get_file_url":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		return mcpGetFileURL(id)

	case "upload_file":
		filename, _ := args["filename"].(string)
		content, _ := args["content"].(string)
		mimeType, _ := args["mime_type"].(string)
		ttl, _ := args["ttl"].(string)
		persistent, _ := args["persistent"].(bool)
		if filename == "" || content == "" {
			return nil, &MCPError{Code: -32602, Message: "filename and content are required"}
		}
		return mcpUploadFile(filename, content, mimeType, ttl, persistent)

	case "create_text":
		content, _ := args["content"].(string)
		itemName, _ := args["name"].(string)
		ttl, _ := args["ttl"].(string)
		persistent, _ := args["persistent"].(bool)
		if content == "" {
			return nil, &MCPError{Code: -32602, Message: "content is required"}
		}
		return mcpCreateText(content, itemName, ttl, persistent)

	case "get_text":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		return mcpGetText(id)

	case "delete_file":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		return mcpDeleteFile(id)

	case "persist_file":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		persistent, _ := args["persistent"].(bool)
		return mcpPersistFile(id, persistent)

	case "describe_image":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		promptName, _ := args["prompt"].(string)
		return mcpDescribeImage(id, promptName)

	case "analyze_image":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return nil, &MCPError{Code: -32602, Message: "id is required"}
		}
		promptName, _ := args["prompt"].(string)
		if promptName == "" {
			promptName = "default"
		}
		return mcpAnalyzeImage(id, promptName)

	case "inspect_image":
		id, _ := args["id"].(string)
		mode, _ := args["mode"].(string)
		question, _ := args["question"].(string)
		if id == "" || mode == "" || question == "" {
			return nil, &MCPError{Code: -32602, Message: "id, mode, and question are required"}
		}
		return mcpInspectImage(id, mode, question)

	case "list_prompts":
		return mcpListPrompts()

	case "create_prompt":
		name, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		promptText, _ := args["prompt"].(string)
		if name == "" || promptText == "" {
			return nil, &MCPError{Code: -32602, Message: "name and prompt are required"}
		}
		return mcpCreatePrompt(name, desc, promptText)

	case "update_prompt":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, &MCPError{Code: -32602, Message: "name is required"}
		}
		desc, _ := args["description"].(string)
		promptText, _ := args["prompt"].(string)
		return mcpUpdatePrompt(name, desc, promptText)

	case "delete_prompt":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, &MCPError{Code: -32602, Message: "name is required"}
		}
		return mcpDeletePrompt(name)

	case "list_vision_presets":
		return mcpListVisionPresets()

	case "set_vision_preset":
		preset, _ := args["preset"].(string)
		if preset == "" {
			return nil, &MCPError{Code: -32602, Message: "preset is required"}
		}
		return mcpSetVisionPreset(preset)

	case "test_vision_preset":
		preset, _ := args["preset"].(string)
		return mcpTestVisionPreset(preset)

	case "test_vision":
		imageType, _ := args["image_type"].(string)
		return mcpVisionTest(imageType)

	case "compare_vision":
		imageType, _ := args["image_type"].(string)
		itemID, _ := args["item_id"].(string)
		promptName, _ := args["prompt"].(string)
		return mcpCompareVision(imageType, itemID, promptName)

	case "compare_prompts":
		imageType, _ := args["image_type"].(string)
		itemID, _ := args["item_id"].(string)
		return mcpComparePrompts(imageType, itemID)

	default:
		return nil, &MCPError{Code: -32601, Message: "Unknown tool: " + name}
	}
}

func mcpListFiles() (interface{}, *MCPError) {
	items := listItems()
	return formatItemList(items)
}

func mcpListFilesFiltered(persistent bool) (interface{}, *MCPError) {
	all := listItems()
	var items []Item
	for _, item := range all {
		if item.Persistent == persistent {
			items = append(items, item)
		}
	}
	return formatItemList(items)
}

func formatItemList(items []Item) (interface{}, *MCPError) {
	var lines []string
	lines = append(lines, fmt.Sprintf("Found %d items:", len(items)))
	for _, item := range items {
		url := linkURL(item.ID, item.Name)
		expiry := "never"
		if !item.Expires.IsZero() {
			expiry = item.Expires.Format("2006-01-02 15:04:05")
		}
		pinTag := ""
		if item.Persistent {
			pinTag = " [persistent]"
		}
		lines = append(lines, fmt.Sprintf("  [%s] %s (%s, %d bytes, expires: %s%s) — %s",
			item.ID, item.Name, item.Type, item.Size, expiry, pinTag, url))
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: strings.Join(lines, "\n")}},
	}, nil
}

func mcpGetFile(id string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	if item.Type == "text" {
		data, err := os.ReadFile(filepath.Join(dataDir, textDir, id))
		if err != nil {
			return nil, &MCPError{Code: -32603, Message: "file not found on disk"}
		}
		return MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: string(data)}},
		}, nil
	}
	// For files, return base64
	data, err := os.ReadFile(filepath.Join(dataDir, fileDir, id))
	if err != nil {
		return nil, &MCPError{Code: -32603, Message: "file not found on disk"}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("File: %s\nMIME: %s\nSize: %d bytes\nURL: %s\nBase64 content:\n%s",
				item.Name, item.MimeType, item.Size, linkURL(id, item.Name), encoded),
		}},
	}, nil
}

func mcpGetFileURL(id string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	url := linkURL(item.ID, item.Name)
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: url}},
	}, nil
}

func mcpUploadFile(filename, content, mimeType, ttlStr string, persistent bool) (interface{}, *MCPError) {
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, &MCPError{Code: -32602, Message: "invalid base64 content"}
	}
	if int64(len(data)) > maxUploadBytes {
		return nil, &MCPError{Code: -32602, Message: fmt.Sprintf("file too large (max %d MB)", maxUploadMB)}
	}

	ttl, err := parseTTL(ttlStr)
	if err != nil {
		ttl = defaultTTL
	}

	id, err := genID()
	if err != nil {
		return nil, &MCPError{Code: -32603, Message: "failed to generate item ID"}
	}
	fpath := filepath.Join(dataDir, fileDir, id)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		return nil, &MCPError{Code: -32603, Message: "failed to write file"}
	}

	if mimeType == "" || isGenericMimeType(mimeType) {
		mimeType = detectMimeType(mimeType, filename, fpath)
	}

	var expires time.Time
	if ttl > 0 && !persistent {
		expires = time.Now().Add(ttl)
	}

	item := Item{
		ID:         id,
		Name:       filename,
		Type:       "file",
		MimeType:   mimeType,
		Size:       int64(len(data)),
		Created:    time.Now(),
		Expires:    expires,
		TTL:        ttlString(ttl),
		Persistent: persistent,
	}
	if persistent {
		item.TTL = "never"
	}
	addItem(item)

	url := linkURL(id, filename)
	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("Uploaded %s (%d bytes). ID: %s, URL: %s", filename, len(data), id, url),
		}},
	}, nil
}

func mcpCreateText(content, name, ttlStr string, persistent bool) (interface{}, *MCPError) {
	ttl, err := parseTTL(ttlStr)
	if err != nil {
		ttl = defaultTTL
	}

	id, err := genID()
	if err != nil {
		return nil, &MCPError{Code: -32603, Message: "failed to generate item ID"}
	}
	if name == "" {
		name = fmt.Sprintf("snippet_%s.txt", time.Now().Format("20060102_150405"))
	}

	if err := os.WriteFile(filepath.Join(dataDir, textDir, id), []byte(content), 0644); err != nil {
		return nil, &MCPError{Code: -32603, Message: "failed to save text"}
	}

	var expires time.Time
	if ttl > 0 && !persistent {
		expires = time.Now().Add(ttl)
	}

	item := Item{
		ID:         id,
		Name:       name,
		Type:       "text",
		Size:       int64(len(content)),
		Created:    time.Now(),
		Expires:    expires,
		TTL:        ttlString(ttl),
		Persistent: persistent,
	}
	if persistent {
		item.TTL = "never"
	}
	addItem(item)

	url := linkURL(id, name)
	return MCPToolResult{
		Content: []MCPContent{{
			Type: "text",
			Text: fmt.Sprintf("Created text snippet '%s' (%d chars). ID: %s, URL: %s", name, len(content), id, url),
		}},
	}, nil
}

func mcpGetText(id string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	if item.Type != "text" {
		return nil, &MCPError{Code: -32602, Message: "item is not a text snippet"}
	}
	data, err := os.ReadFile(filepath.Join(dataDir, textDir, id))
	if err != nil {
		return nil, &MCPError{Code: -32603, Message: "file not found on disk"}
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: string(data)}},
	}, nil
}

func mcpDeleteFile(id string) (interface{}, *MCPError) {
	if _, ok := findItem(id); !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	deleteItem(id)
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Deleted item %s", id)}},
	}, nil
}

func mcpPersistFile(id string, persistent bool) (interface{}, *MCPError) {
	if _, ok := findItem(id); !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	updateItem(id, func(item *Item) bool {
		item.Persistent = persistent
		if persistent {
			item.Expires = time.Time{}
			item.TTL = "never"
		} else {
			item.Expires = time.Now().Add(defaultTTL)
			item.TTL = ttlString(defaultTTL)
		}
		return true
	})
	action := "pinned"
	if !persistent {
		action = "unpinned"
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("%s item %s", action, id)}},
	}, nil
}

func mcpDescribeImage(id, promptName string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	if item.Type != "file" || !strings.HasPrefix(item.MimeType, "image/") {
		return nil, &MCPError{Code: -32602, Message: "item is not an image"}
	}
	if len(item.Analyses) == 0 {
		return MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("No vision analyses available for image %s. Use analyze_image to trigger analysis.", id)}},
		}, nil
	}

	// If a specific prompt is requested, return just that analysis
	if promptName != "" {
		a, exists := item.Analyses[promptName]
		if !exists {
			return MCPToolResult{
				Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("No analysis with prompt %q for image %s. Available: %s", promptName, id, strings.Join(analysisKeys(item.Analyses), ", "))}},
			}, nil
		}
		return MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: formatAnalysis(id, item.Name, promptName, a)}},
		}, nil
	}

	// Return all analyses
	var lines []string
	lines = append(lines, fmt.Sprintf("Vision analyses for %s (%s):", id, item.Name))
	for _, pn := range analysisKeysSorted(item.Analyses) {
		a := item.Analyses[pn]
		lines = append(lines, "")
		lines = append(lines, formatAnalysis(id, item.Name, pn, a))
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: strings.Join(lines, "\n")}},
	}, nil
}

func mcpAnalyzeImage(id, promptName string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	if item.Type != "file" || !strings.HasPrefix(item.MimeType, "image/") {
		return nil, &MCPError{Code: -32602, Message: "item is not an image"}
	}
	if !visionEnabled {
		return nil, &MCPError{Code: -32603, Message: "vision processing is disabled"}
	}
	prompt, ok := getPrompt(promptName)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: fmt.Sprintf("prompt %q not found. Use list_prompts to see available prompts.", promptName)}
	}
	result, err := analyzeImage(id, prompt.Prompt)
	if err != nil {
		return nil, &MCPError{Code: -32603, Message: "analysis failed: " + err.Error()}
	}
	analysis := &ItemAnalysis{
		Status:      "complete",
		Text:        result.Text,
		Description: result.Description,
		Evidence:    result.Evidence,
		Backend:     visionModel,
		PromptName:  promptName,
		ProcessedAt: time.Now(),
	}
	updateItem(id, func(it *Item) bool {
		if it.Analyses == nil {
			it.Analyses = make(map[string]*ItemAnalysis)
		}
		it.Analyses[promptName] = analysis
		return true
	})
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: formatAnalysis(id, item.Name, promptName, analysis)}},
	}, nil
}

func mcpInspectImage(id, mode, question string) (interface{}, *MCPError) {
	item, ok := findItem(id)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "item not found"}
	}
	if item.Type != "file" || !strings.HasPrefix(item.MimeType, "image/") {
		return nil, &MCPError{Code: -32602, Message: "item is not an image"}
	}
	if !visionEnabled {
		return nil, &MCPError{Code: -32603, Message: "vision processing is disabled"}
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	promptName, ok := inspectionPromptName(mode)
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "mode must be one of: ui, ocr, code, document, diagram, describe"}
	}
	prompt, ok := getPrompt(promptName)
	if !ok {
		return nil, &MCPError{Code: -32603, Message: fmt.Sprintf("built-in prompt %q is unavailable", promptName)}
	}

	question = strings.TrimSpace(question)
	promptText := fmt.Sprintf("%s\n\nSpecific question from the text-only agent:\n---\n%s\n---\nAnswer the question only from visible evidence while preserving the required JSON schema. /no_think", prompt.Prompt, question)
	start := time.Now()
	result, err := analyzeImage(id, promptText)
	elapsed := time.Since(start).Round(time.Millisecond)
	sum := sha256.Sum256([]byte(question))
	analysisName := fmt.Sprintf("inspect:%s:%x", mode, sum[:4])
	if err != nil {
		updateItem(id, func(it *Item) bool {
			if it.Analyses == nil {
				it.Analyses = make(map[string]*ItemAnalysis)
			}
			it.Analyses[analysisName] = &ItemAnalysis{Status: "failed", Backend: visionModel, PromptName: promptName, Question: question, DurationMs: elapsed.Milliseconds(), Error: err.Error()}
			return true
		})
		return nil, &MCPError{Code: -32603, Message: "inspection failed: " + err.Error()}
	}

	analysis := &ItemAnalysis{Status: "complete", Text: result.Text, Description: result.Description, Evidence: result.Evidence, Backend: visionModel, PromptName: promptName, Question: question, ProcessedAt: time.Now(), DurationMs: elapsed.Milliseconds()}
	updateItem(id, func(it *Item) bool {
		if it.Analyses == nil {
			it.Analyses = make(map[string]*ItemAnalysis)
		}
		it.Analyses[analysisName] = analysis
		return true
	})
	return MCPToolResult{Content: []MCPContent{{Type: "text", Text: formatAnalysis(id, item.Name, analysisName, analysis)}}}, nil
}

func inspectionPromptName(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ui":
		return "ui", true
	case "ocr", "describe":
		return "default", true
	case "code", "document", "diagram":
		return strings.ToLower(strings.TrimSpace(mode)), true
	default:
		return "", false
	}
}

func mcpListPrompts() (interface{}, *MCPError) {
	list := listPromptsSorted()
	var lines []string
	lines = append(lines, fmt.Sprintf("Available vision prompts (%d):", len(list)))
	for _, p := range list {
		tag := ""
		if p.BuiltIn {
			tag = " [built-in]"
		}
		lines = append(lines, fmt.Sprintf("  %s%s: %s", p.Name, tag, p.Description))
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: strings.Join(lines, "\n")}},
	}, nil
}

// formatAnalysis formats a single analysis for MCP text output
func formatAnalysis(id, name, promptName string, a *ItemAnalysis) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("[%s] %s (%s):", promptName, id, name))
	lines = append(lines, fmt.Sprintf("  Status: %s", a.Status))
	lines = append(lines, fmt.Sprintf("  Backend: %s", a.Backend))
	if a.Description != "" {
		lines = append(lines, fmt.Sprintf("  Description: %s", a.Description))
	}
	if a.Question != "" {
		lines = append(lines, fmt.Sprintf("  Question: %s", a.Question))
	}
	if a.Text != "" {
		lines = append(lines, "  Extracted text:")
		lines = append(lines, a.Text)
	}
	if a.Evidence != nil {
		evidence, err := json.Marshal(a.Evidence)
		if err == nil {
			lines = append(lines, "  Visible evidence:")
			lines = append(lines, string(evidence))
		}
	}
	if a.Error != "" {
		lines = append(lines, fmt.Sprintf("  Error: %s", a.Error))
	}
	return strings.Join(lines, "\n")
}

// analysisKeys returns unsorted keys from an analyses map
func analysisKeys(m map[string]*ItemAnalysis) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// analysisKeysSorted returns sorted keys from an analyses map
func analysisKeysSorted(m map[string]*ItemAnalysis) []string {
	keys := analysisKeys(m)
	sort.Strings(keys)
	return keys
}

func writeMCPResult(w http.ResponseWriter, id interface{}, result interface{}) {
	json.NewEncoder(w).Encode(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeMCPError(w http.ResponseWriter, id interface{}, code int, msg string) {
	json.NewEncoder(w).Encode(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: code, Message: msg},
	})
}

// --- Vision prompt MCP tools ---

func mcpCreatePrompt(name, description, promptText string) (interface{}, *MCPError) {
	promptsMu.Lock()
	defer promptsMu.Unlock()
	if _, exists := prompts[name]; exists {
		return nil, &MCPError{Code: -32602, Message: "prompt already exists"}
	}
	p := &VisionPrompt{
		Name:        name,
		Description: description,
		Prompt:      promptText,
		BuiltIn:     false,
	}
	prompts[name] = p
	savePrompts()
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Created prompt %q", name)}},
	}, nil
}

func mcpUpdatePrompt(name, description, promptText string) (interface{}, *MCPError) {
	promptsMu.Lock()
	defer promptsMu.Unlock()
	p, ok := prompts[name]
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "prompt not found"}
	}
	if description != "" {
		p.Description = description
	}
	if promptText != "" {
		p.Prompt = promptText
	}
	savePrompts()
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Updated prompt %q", name)}},
	}, nil
}

func mcpDeletePrompt(name string) (interface{}, *MCPError) {
	promptsMu.Lock()
	defer promptsMu.Unlock()
	p, ok := prompts[name]
	if !ok {
		return nil, &MCPError{Code: -32602, Message: "prompt not found"}
	}
	if p.BuiltIn {
		return nil, &MCPError{Code: -32602, Message: "cannot delete built-in prompt"}
	}
	delete(prompts, name)
	savePrompts()
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Deleted prompt %q", name)}},
	}, nil
}

// --- Vision preset MCP tools ---

func mcpListVisionPresets() (interface{}, *MCPError) {
	presets := listVisionPresets()
	var lines []string
	lines = append(lines, fmt.Sprintf("Vision enabled: %v", visionEnabled))
	lines = append(lines, fmt.Sprintf("Active preset: %s", visionConfig.ActivePreset))
	if visionEnvOverridden {
		lines = append(lines, "Note: env vars (VISION_ENDPOINT/VISION_MODEL) are overriding UI config")
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Presets (%d):", len(presets)))
	for _, p := range presets {
		active := ""
		if p.Name == visionConfig.ActivePreset {
			active = " *"
		}
		desc := ""
		if p.Description != "" {
			desc = fmt.Sprintf(" — %s", p.Description)
		}
		lines = append(lines, fmt.Sprintf("  %s%s: %s @ %s%s", p.Name, active, p.Model, p.Endpoint, desc))
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: strings.Join(lines, "\n")}},
	}, nil
}

func mcpSetVisionPreset(preset string) (interface{}, *MCPError) {
	if visionEnvOverridden {
		return nil, &MCPError{Code: -32603, Message: "cannot change active preset — env vars are overriding config"}
	}
	if !setActiveVisionPreset(preset) {
		return nil, &MCPError{Code: -32602, Message: fmt.Sprintf("preset %q not found", preset)}
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Switched active preset to %q", preset)}},
	}, nil
}

func mcpTestVisionPreset(presetName string) (interface{}, *MCPError) {
	var preset *VisionPreset
	if presetName == "" {
		preset = getActiveVisionPreset()
	} else {
		p, ok := getVisionPreset(presetName)
		if !ok {
			return nil, &MCPError{Code: -32602, Message: fmt.Sprintf("preset %q not found", presetName)}
		}
		preset = p
	}
	result := testVisionPreset(preset)
	var lines []string
	status := "FAILED"
	if result.Success {
		status = "OK"
	}
	lines = append(lines, fmt.Sprintf("Test %s for preset %q: %s", status, preset.Name, result.Message))
	if result.Latency != "" {
		lines = append(lines, fmt.Sprintf("Latency: %s", result.Latency))
	}
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: strings.Join(lines, "\n")}},
	}, nil
}
