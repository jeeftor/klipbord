package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed testdata/sample_terminal.png
var sampleTerminalImage []byte

// apiVisionTestHandler runs the full vision pipeline on the embedded sample image
func apiVisionTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if !visionEnabled {
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": "Vision processing is disabled",
		})
		return
	}

	preset := getActiveVisionPreset()

	// Write the sample image to the data dir so analyzeImage can read it
	tmpID := fmt.Sprintf("vision_test_%d", time.Now().UnixNano())
	tmpPath := filepath.Join(dataDir, fileDir, tmpID)
	os.MkdirAll(filepath.Dir(tmpPath), 0755)
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, sampleTerminalImage, 0644); err != nil {
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to write temp image: %v", err),
		})
		return
	}

	// Get the default prompt
	prompt, ok := getPrompt("default")
	if !ok {
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": "default prompt not found",
		})
		return
	}

	// Run the analysis (synchronous, not async)
	start := time.Now()
	result, err := analyzeImage(tmpID, prompt.Prompt)
	latency := time.Since(start)

	if err != nil {
		writeJSON(w, map[string]interface{}{
			"success":  false,
			"message":  fmt.Sprintf("Analysis failed: %v", err),
			"latency":  latency.Round(time.Millisecond).String(),
			"preset":   preset.Name,
			"model":    preset.Model,
			"endpoint": preset.Endpoint,
		})
		return
	}

	writeJSON(w, map[string]interface{}{
		"success":     true,
		"message":     "Vision analysis completed",
		"latency":     latency.Round(time.Millisecond).String(),
		"preset":      preset.Name,
		"model":       preset.Model,
		"endpoint":    preset.Endpoint,
		"image_type":  result.ImageType,
		"text":        result.Text,
		"description": result.Description,
		"image_b64":   base64.StdEncoding.EncodeToString(sampleTerminalImage),
	})
}

// mcpVisionTest runs the full vision pipeline on the embedded sample image via MCP
func mcpVisionTest() (interface{}, *MCPError) {
	if !visionEnabled {
		return nil, &MCPError{Code: -32603, Message: "vision processing is disabled"}
	}

	preset := getActiveVisionPreset()

	tmpID := fmt.Sprintf("vision_test_%d", time.Now().UnixNano())
	tmpPath := filepath.Join(dataDir, fileDir, tmpID)
	os.MkdirAll(filepath.Dir(tmpPath), 0755)
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, sampleTerminalImage, 0644); err != nil {
		return nil, &MCPError{Code: -32603, Message: "failed to write temp image: " + err.Error()}
	}

	prompt, ok := getPrompt("default")
	if !ok {
		return nil, &MCPError{Code: -32603, Message: "default prompt not found"}
	}

	start := time.Now()
	result, err := analyzeImage(tmpID, prompt.Prompt)
	latency := time.Since(start).Round(time.Millisecond)

	if err != nil {
		return MCPToolResult{
			Content: []MCPContent{{Type: "text", Text: fmt.Sprintf("Vision test FAILED (%s): %v", latency, err)}},
		}, nil
	}

	text := fmt.Sprintf("Vision test OK (%s) using preset %q [%s]:\n", latency, preset.Name, preset.Model)
	text += fmt.Sprintf("Image type: %s\n", result.ImageType)
	text += fmt.Sprintf("Description: %s\n", result.Description)
	text += fmt.Sprintf("Extracted text:\n%s", result.Text)

	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: text}},
	}, nil
}

// Ensure io is imported (used by analyzeImage internally)
var _ = io.ReadAll
var _ = json.Marshal
