package app

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	chunkSize      = 5 * 1024 * 1024
	chunkStaleTime = time.Hour
)

func apiUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, `{"error":"upload too large or invalid"}`, http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file field required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	ttl, err := parseTTL(r.FormValue("ttl"))
	if err != nil {
		ttl = defaultTTL
	}
	id, err := genID()
	if err != nil {
		http.Error(w, `{"error":"failed to generate item ID"}`, http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dataDir, fileDir, id)
	destination, err := os.Create(path)
	if err != nil {
		http.Error(w, `{"error":"failed to create file"}`, http.StatusInternalServerError)
		return
	}
	defer destination.Close()
	written, err := io.Copy(destination, file)
	if err != nil {
		_ = os.Remove(path)
		http.Error(w, `{"error":"failed to write file"}`, http.StatusInternalServerError)
		return
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	addUploadedFile(id, header.Filename, mimeType, written, ttl)
	writeJSON(w, map[string]interface{}{"id": id, "name": header.Filename, "url": linkURL(id)})
}

func addUploadedFile(id, name, mimeType string, size int64, ttl time.Duration) {
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	addItem(Item{
		ID: id, Name: name, Type: "file", MimeType: mimeType, Size: size,
		Created: time.Now(), Expires: expires, TTL: ttlString(ttl),
	})
	if visionEnabled && strings.HasPrefix(mimeType, "image/") {
		go analyzeImageAsync(id)
	}
}

type chunkUploadMeta struct {
	Filename    string    `json:"filename"`
	MimeType    string    `json:"mime_type"`
	Size        int64     `json:"size"`
	TTL         string    `json:"ttl"`
	TotalChunks int       `json:"total_chunks"`
	Created     time.Time `json:"created"`
}

func chunkUploadDir(uploadID string) string {
	return filepath.Join(dataDir, chunkDir, uploadID)
}

func chunkMetaPath(uploadID string) string {
	return filepath.Join(chunkUploadDir(uploadID), ".meta")
}

func loadChunkMeta(uploadID string) (chunkUploadMeta, bool) {
	if !validChunkID(uploadID) {
		return chunkUploadMeta{}, false
	}
	data, err := os.ReadFile(chunkMetaPath(uploadID))
	if err != nil {
		return chunkUploadMeta{}, false
	}
	var metadata chunkUploadMeta
	if err := json.Unmarshal(data, &metadata); err != nil {
		return chunkUploadMeta{}, false
	}
	return metadata, true
}

func saveChunkMeta(uploadID string, metadata chunkUploadMeta) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal chunk metadata: %w", err)
	}
	return os.WriteFile(chunkMetaPath(uploadID), data, 0644)
}

func apiUploadInitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		TTL      string `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if request.Filename == "" {
		http.Error(w, `{"error":"filename required"}`, http.StatusBadRequest)
		return
	}
	if request.Size <= 0 {
		http.Error(w, `{"error":"size required"}`, http.StatusBadRequest)
		return
	}
	if request.Size > maxUploadBytes {
		http.Error(w, fmt.Sprintf(`{"error":"file too large (max %d MB)"}`, maxUploadMB), http.StatusBadRequest)
		return
	}

	totalChunks := int((request.Size + chunkSize - 1) / chunkSize)
	uploadID, err := genChunkID()
	if err != nil {
		http.Error(w, `{"error":"failed to generate upload ID"}`, http.StatusInternalServerError)
		return
	}
	directory := chunkUploadDir(uploadID)
	if err := os.MkdirAll(directory, 0755); err != nil {
		http.Error(w, `{"error":"failed to create upload dir"}`, http.StatusInternalServerError)
		return
	}
	metadata := chunkUploadMeta{
		Filename: request.Filename, MimeType: request.MimeType, Size: request.Size, TTL: request.TTL,
		TotalChunks: totalChunks, Created: time.Now(),
	}
	if err := saveChunkMeta(uploadID, metadata); err != nil {
		_ = os.RemoveAll(directory)
		http.Error(w, `{"error":"failed to save upload metadata"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"upload_id": uploadID, "chunk_size": chunkSize, "total_chunks": totalChunks})
}

func apiUploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, chunkSize+1024*1024)
	if err := r.ParseMultipartForm(chunkSize + 1024*1024); err != nil {
		http.Error(w, `{"error":"chunk too large or invalid"}`, http.StatusBadRequest)
		return
	}
	uploadID := r.FormValue("upload_id")
	metadata, ok := loadChunkMeta(uploadID)
	if !ok {
		http.Error(w, `{"error":"upload not found or expired"}`, http.StatusNotFound)
		return
	}
	index, err := strconv.Atoi(r.FormValue("chunk_index"))
	if err != nil || index < 0 || index >= metadata.TotalChunks {
		http.Error(w, `{"error":"invalid chunk_index"}`, http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, `{"error":"chunk data required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()
	destination, err := os.Create(filepath.Join(chunkUploadDir(uploadID), strconv.Itoa(index)))
	if err != nil {
		http.Error(w, `{"error":"failed to write chunk"}`, http.StatusInternalServerError)
		return
	}
	defer destination.Close()
	if _, err := io.Copy(destination, file); err != nil {
		http.Error(w, `{"error":"failed to write chunk"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "chunk_index": index, "upload_id": uploadID})
}

func apiUploadStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	uploadID := strings.TrimPrefix(r.URL.Path, "/api/upload/status/")
	metadata, ok := loadChunkMeta(uploadID)
	if !ok {
		http.Error(w, `{"error":"upload not found or expired"}`, http.StatusNotFound)
		return
	}
	entries, err := os.ReadDir(chunkUploadDir(uploadID))
	if err != nil {
		http.Error(w, `{"error":"failed to read upload dir"}`, http.StatusInternalServerError)
		return
	}
	received := make([]int, 0, len(entries))
	for _, entry := range entries {
		if index, err := strconv.Atoi(entry.Name()); err == nil {
			received = append(received, index)
		}
	}
	writeJSON(w, map[string]interface{}{
		"upload_id": uploadID, "filename": metadata.Filename, "size": metadata.Size,
		"total_chunks": metadata.TotalChunks, "received": received,
		"complete": len(received) == metadata.TotalChunks,
	})
}

func apiUploadCompleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	metadata, ok := loadChunkMeta(request.UploadID)
	if !ok {
		http.Error(w, `{"error":"upload not found or expired"}`, http.StatusNotFound)
		return
	}
	directory := chunkUploadDir(request.UploadID)
	for index := 0; index < metadata.TotalChunks; index++ {
		if _, err := os.Stat(filepath.Join(directory, strconv.Itoa(index))); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"missing chunk %d"}`, index), http.StatusBadRequest)
			return
		}
	}

	id, err := genID()
	if err != nil {
		http.Error(w, `{"error":"failed to generate item ID"}`, http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dataDir, fileDir, id)
	destination, err := os.Create(path)
	if err != nil {
		http.Error(w, `{"error":"failed to create file"}`, http.StatusInternalServerError)
		return
	}
	defer destination.Close()
	var written int64
	for index := 0; index < metadata.TotalChunks; index++ {
		chunk, err := os.Open(filepath.Join(directory, strconv.Itoa(index)))
		if err != nil {
			_ = os.Remove(path)
			http.Error(w, `{"error":"failed to read chunk"}`, http.StatusInternalServerError)
			return
		}
		count, copyErr := io.Copy(destination, chunk)
		_ = chunk.Close()
		if copyErr != nil {
			_ = os.Remove(path)
			http.Error(w, `{"error":"failed to write file"}`, http.StatusInternalServerError)
			return
		}
		written += count
	}
	_ = os.RemoveAll(directory)
	mimeType := metadata.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	ttl, err := parseTTL(metadata.TTL)
	if err != nil {
		ttl = defaultTTL
	}
	addUploadedFile(id, metadata.Filename, mimeType, written, ttl)
	writeJSON(w, map[string]interface{}{"id": id, "name": metadata.Filename, "url": linkURL(id)})
}

func genChunkID() (string, error) {
	for {
		id, err := genID()
		if err != nil {
			return "", fmt.Errorf("generate chunk upload ID: %w", err)
		}
		if _, err := os.Stat(chunkUploadDir(id)); os.IsNotExist(err) {
			return id, nil
		}
	}
}

func validChunkID(id string) bool {
	return len(id) == idLen && !strings.ContainsAny(id, `/\\`)
}

func chunkSweeper() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		root := filepath.Join(dataDir, chunkDir)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metadata, ok := loadChunkMeta(entry.Name())
			if !ok || time.Since(metadata.Created) > chunkStaleTime {
				_ = os.RemoveAll(filepath.Join(root, entry.Name()))
				if ok {
					log.Printf("Cleaned up stale chunk upload %s (%s)", entry.Name(), metadata.Filename)
				}
			}
		}
	}
}
