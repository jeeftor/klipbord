package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ffprobeResult maps the JSON output from ffprobe.
type ffprobeResult struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

// ffprobeAvailable checks if ffprobe is on PATH.
var ffprobeChecked bool
var ffprobeOK bool

func ffprobeAvailable() bool {
	if ffprobeChecked {
		return ffprobeOK
	}
	_, err := exec.LookPath("ffprobe")
	ffprobeOK = err == nil
	ffprobeChecked = true
	if !ffprobeOK {
		log.Println("ffprobe not found — media metadata extraction disabled")
	}
	return ffprobeOK
}

// probeMedia runs ffprobe on the file and returns extracted metadata.
// Returns nil if ffprobe is unavailable or the file is not a media file.
func probeMedia(path string) *MediaInfo {
	if !ffprobeAvailable() {
		return nil
	}
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var result ffprobeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil
	}

	info := &MediaInfo{}
	// Parse format-level duration/bitrate (most reliable)
	info.Duration = parseFloatSafe(result.Format.Duration)
	info.BitRate = parseIntSafe(result.Format.BitRate)

	for _, s := range result.Streams {
		switch s.CodecType {
		case "video":
			if info.Codec == "" {
				info.Codec = s.CodecName
			}
			info.Width = s.Width
			info.Height = s.Height
			if info.Duration == 0 {
				info.Duration = parseFloatSafe(s.Duration)
			}
			if info.BitRate == 0 {
				info.BitRate = parseIntSafe(s.BitRate)
			}
		case "audio":
			if info.Codec == "" {
				info.Codec = s.CodecName
			}
			info.SampleRate = parseIntSafe(s.SampleRate)
			info.Channels = s.Channels
			if info.Duration == 0 {
				info.Duration = parseFloatSafe(s.Duration)
			}
			if info.BitRate == 0 {
				info.BitRate = parseIntSafe(s.BitRate)
			}
		}
	}

	// Return nil if we got nothing useful
	if info.Duration == 0 && info.Codec == "" && info.BitRate == 0 {
		return nil
	}
	return info
}

// probeMediaForItem is the entry point called after a file is uploaded.
// It runs ffprobe asynchronously and updates the item in metadata.
func probeMediaForItem(id, mimeType string) {
	if !strings.HasPrefix(mimeType, "audio/") && !strings.HasPrefix(mimeType, "video/") {
		return
	}
	if !ffprobeAvailable() {
		return
	}
	path := filepath.Join(dataDir, fileDir, id)
	go func() {
		info := probeMedia(path)
		if info == nil {
			return
		}
		updateItemMediaInfo(id, info)
	}()
}

// updateItemMediaInfo updates the MediaInfo field on an item and saves.
func updateItemMediaInfo(id string, info *MediaInfo) {
	metaMu.Lock()
	for i := range meta.Items {
		if meta.Items[i].ID == id {
			meta.Items[i].MediaInfo = info
			break
		}
	}
	metaMu.Unlock()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal metadata after media probe: %v", err)
		return
	}
	_ = os.WriteFile(filepath.Join(dataDir, metaFile), data, 0644)
	log.Printf("Media probe: %s -> codec=%s duration=%.1f bitrate=%d", id, info.Codec, info.Duration, info.BitRate)
}

func parseFloatSafe(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func parseIntSafe(s string) int {
	var i int
	fmt.Sscanf(s, "%d", &i)
	return i
}
