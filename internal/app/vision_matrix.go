package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MatrixCell is the result of one (preset × image × prompt) combination.
type MatrixCell struct {
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	Preview    string `json:"preview,omitempty"` // first 200 chars of extracted text
	Error      string `json:"error,omitempty"`
}

// MatrixPresetResult holds all cells for one preset.
type MatrixPresetResult struct {
	Preset    string                            `json:"preset"`
	Model     string                            `json:"model"`
	Endpoint  string                            `json:"endpoint"`
	Cells     map[string]map[string]*MatrixCell `json:"cells"` // [image_type][prompt_name]
	TotalRuns int                               `json:"total_runs"`
	Successes int                               `json:"successes"`
}

// MatrixResult is the final summary sent in the "done" SSE event.
type MatrixResult struct {
	Presets    []string             `json:"presets"`
	ImageTypes []string             `json:"image_types"`
	Prompts    []string             `json:"prompts"`
	Results    []MatrixPresetResult `json:"results"`
	TotalCells int                  `json:"total_cells"`
	Successes  int                  `json:"successes"`
}

// matrixConcurrency is the max number of parallel (image × prompt) calls per preset.
// Keeps the server from being overwhelmed during model load/swap.
const matrixConcurrency = 3

// modelWarmupTimeout is how long we wait for a model to load on first request.
const modelWarmupTimeout = 120 * time.Second

var allSampleImageTypes = []string{"terminal", "code", "document", "diagram", "screenshot"}

// apiVisionTestMatrixHandler streams matrix progress as Server-Sent Events.
// GET /api/vision/test-matrix
func apiVisionTestMatrixHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	sendEvent := func(eventType string, data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		flusher.Flush()
	}

	if !visionEnabled {
		sendEvent("error", map[string]string{"message": "vision processing is disabled"})
		return
	}

	presets := listVisionPresets()
	prompts := listPromptsSorted()

	if len(presets) == 0 {
		sendEvent("error", map[string]string{"message": "no presets configured"})
		return
	}
	if len(prompts) == 0 {
		sendEvent("error", map[string]string{"message": "no prompts configured"})
		return
	}

	imageTypes := allSampleImageTypes
	promptNames := make([]string, len(prompts))
	for i, p := range prompts {
		promptNames[i] = p.Name
	}
	presetNames := make([]string, len(presets))
	for i, p := range presets {
		presetNames[i] = p.Name
	}

	totalCells := len(presets) * len(imageTypes) * len(prompts)
	log.Printf("vision matrix: %d presets × %d images × %d prompts = %d cells",
		len(presets), len(imageTypes), len(prompts), totalCells)

	// Send the initial metadata so the UI can render the skeleton.
	sendEvent("start", map[string]interface{}{
		"total_cells": totalCells,
		"presets":     presetNames,
		"image_types": imageTypes,
		"prompts":     promptNames,
	})

	// Write all sample images to temp files once (shared across presets).
	type tempImg struct {
		id   string
		path string
	}
	tempImgs := make(map[string]tempImg, len(imageTypes))
	for _, imgType := range imageTypes {
		data, _ := getSampleImage(imgType)
		id := fmt.Sprintf("matrix_%s_%d", imgType, time.Now().UnixNano())
		path := filepath.Join(dataDir, fileDir, id)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, data, 0644); err != nil {
			log.Printf("vision matrix: failed to write temp image %s: %v", imgType, err)
			continue
		}
		tempImgs[imgType] = tempImg{id: id, path: path}
	}
	defer func() {
		for _, t := range tempImgs {
			os.Remove(t.path)
		}
	}()

	var allResults []MatrixPresetResult
	totalSuccesses := 0

	for pi, preset := range presets {
		log.Printf("vision matrix: running preset %q (%s)", preset.Name, preset.Model)

		sendEvent("preset_start", map[string]interface{}{
			"preset": preset.Name,
			"model":  preset.Model,
			"index":  pi,
			"total":  len(presets),
		})

		// Warmup: wait for the model to load before firing parallel cells.
		modelReady := warmupModel(preset, func(attempt, maxAttempts int, err string) {
			sendEvent("model_wait", map[string]interface{}{
				"preset":       preset.Name,
				"attempt":      attempt,
				"max_attempts": maxAttempts,
				"error":        err,
			})
		})

		if !modelReady {
			log.Printf("vision matrix: preset %q failed to warm up, skipping", preset.Name)
			sendEvent("preset_failed", map[string]interface{}{
				"preset":  preset.Name,
				"message": "model did not become ready within timeout",
			})
			// Still add a result so the UI shows 0/N for this preset.
			pr := MatrixPresetResult{
				Preset:   preset.Name,
				Model:    preset.Model,
				Endpoint: preset.Endpoint,
				Cells:    make(map[string]map[string]*MatrixCell),
			}
			for _, imgType := range imageTypes {
				for _, p := range prompts {
					if pr.Cells[imgType] == nil {
						pr.Cells[imgType] = make(map[string]*MatrixCell)
					}
					pr.Cells[imgType][p.Name] = &MatrixCell{Error: "model warmup failed"}
					pr.TotalRuns++
					sendEvent("cell_complete", map[string]interface{}{
						"preset": preset.Name, "image": imgType, "prompt": p.Name,
						"success": false, "duration_ms": int64(0), "error": "model warmup failed",
					})
				}
			}
			allResults = append(allResults, pr)
			continue
		}

		sendEvent("model_ready", map[string]interface{}{
			"preset": preset.Name,
			"model":  preset.Model,
		})

		pr := MatrixPresetResult{
			Preset:   preset.Name,
			Model:    preset.Model,
			Endpoint: preset.Endpoint,
			Cells:    make(map[string]map[string]*MatrixCell),
		}

		// Semaphore for concurrency limit within this preset.
		sem := make(chan struct{}, matrixConcurrency)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, imgType := range imageTypes {
			for _, prompt := range prompts {
				t, ok := tempImgs[imgType]
				if !ok {
					continue
				}
				wg.Add(1)
				go func(imgT string, p *VisionPrompt, tmpID string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					cell := runMatrixCell(tmpID, preset, p)

					mu.Lock()
					if pr.Cells[imgT] == nil {
						pr.Cells[imgT] = make(map[string]*MatrixCell)
					}
					pr.Cells[imgT][p.Name] = cell
					pr.TotalRuns++
					if cell.Success {
						pr.Successes++
					}
					mu.Unlock()

					// Stream cell result immediately.
					sendEvent("cell_complete", map[string]interface{}{
						"preset":      preset.Name,
						"image":       imgT,
						"prompt":      p.Name,
						"success":     cell.Success,
						"duration_ms": cell.DurationMs,
						"error":       cell.Error,
					})
				}(imgType, prompt, t.id)
			}
		}
		wg.Wait()

		allResults = append(allResults, pr)
		totalSuccesses += pr.Successes

		sendEvent("preset_complete", map[string]interface{}{
			"preset":    preset.Name,
			"successes": pr.Successes,
			"total":     pr.TotalRuns,
		})

		// Attempt to unload the model; give it a moment to settle first.
		time.Sleep(2 * time.Second)
		go tryUnloadModel(preset)
	}

	sendEvent("done", MatrixResult{
		Presets:    presetNames,
		ImageTypes: imageTypes,
		Prompts:    promptNames,
		Results:    allResults,
		TotalCells: totalCells,
		Successes:  totalSuccesses,
	})
}

// warmupModel sends a lightweight request to the preset to ensure the model
// is loaded before we fire parallel cells. Returns true if the model responded
// successfully within the timeout.
func warmupModel(preset *VisionPreset, onWait func(attempt, max int, errMsg string)) bool {
	const maxAttempts = 8
	const retryDelay = 5 * time.Second

	reqBody := map[string]interface{}{
		"model":      preset.Model,
		"messages":   []map[string]string{{"role": "user", "content": "Ready?"}},
		"max_tokens": 5,
	}
	bodyJSON, _ := json.Marshal(reqBody)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequest("POST", preset.Endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			onWait(attempt, maxAttempts, err.Error())
			time.Sleep(retryDelay)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if preset.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+preset.APIKey)
		}

		client := &http.Client{Timeout: modelWarmupTimeout}
		resp, err := client.Do(req)
		if err != nil {
			onWait(attempt, maxAttempts, err.Error())
			time.Sleep(retryDelay)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.Printf("vision matrix: preset %q ready (attempt %d)", preset.Name, attempt)
			return true
		}
		onWait(attempt, maxAttempts, fmt.Sprintf("HTTP %d", resp.StatusCode))
		time.Sleep(retryDelay)
	}
	return false
}

// runMatrixCell runs one (preset × image × prompt) and returns a MatrixCell.
func runMatrixCell(tmpID string, preset *VisionPreset, prompt *VisionPrompt) *MatrixCell {
	start := time.Now()
	var result PresetResult
	if prompt.Mode == "scan" {
		result = analyzeWithPresetScan(tmpID, preset, prompt.Prompt)
	} else {
		result = analyzeWithPreset(tmpID, preset, prompt.Prompt)
	}
	elapsed := time.Since(start).Milliseconds()

	cell := &MatrixCell{
		Success:    result.Success,
		DurationMs: elapsed,
	}
	if !result.Success {
		cell.Error = result.Error
	} else {
		text := result.Text
		if len(text) > 200 {
			text = text[:200]
		}
		cell.Preview = text
	}
	return cell
}

// tryUnloadModel sends an Ollama-compatible keep_alive=0 request to ask the
// server to evict the model from memory. Fails silently.
func tryUnloadModel(preset *VisionPreset) {
	body := map[string]interface{}{
		"model":      preset.Model,
		"messages":   []interface{}{},
		"keep_alive": 0,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", preset.Endpoint, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if preset.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+preset.APIKey)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Printf("vision matrix: unload signal sent to %q (HTTP %d)", preset.Name, resp.StatusCode)
}
