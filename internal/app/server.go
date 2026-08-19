package app

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

var version = "dev"

// Assets contains the static files served by Klipbord.
type Assets struct {
	IndexHTML     []byte
	SwaggerHTML   []byte
	ManifestJSON  []byte
	ServiceWorker []byte
	IconSVG       []byte
}

var assets Assets

const (
	defaultPort        = "8080"
	defaultDataDir     = "/data"
	defaultBaseURL     = "http://localhost:8080"
	defaultMaxUploadMB = 2048
)

var (
	maxUploadMB    int
	maxUploadBytes int64
)

var (
	visionEnabled        bool
	visionEndpoint       string
	visionModel          string
	visionRequestTimeout = 120 * time.Second
	modelUnloadTimeout   = 120 * time.Second
)

// Run initializes Klipbord and starts its HTTP server.
func Run(appVersion string, staticAssets Assets) {
	configureLogCapture()
	version = appVersion
	assets = staticAssets
	dataDir = envOr("DATA_DIR", defaultDataDir)
	port := envOr("PORT", defaultPort)
	baseURL = envOr("BASE_URL", "")
	if baseURL == "" {
		baseURL = detectedBaseURL(port)
	}
	visionRequestTimeout = durationEnv("VISION_REQUEST_TIMEOUT", 120*time.Second)
	modelUnloadTimeout = durationEnv("VISION_UNLOAD_TIMEOUT", 120*time.Second)

	maxUploadMB = defaultMaxUploadMB
	if value := envOr("MAX_UPLOAD_MB", ""); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			maxUploadMB = parsed
		}
	}
	maxUploadBytes = int64(maxUploadMB) * 1024 * 1024

	initVisionConfig()
	for _, directory := range []string{textDir, fileDir, chunkDir} {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0755); err != nil {
			log.Fatalf("Failed to create dir %s: %v", directory, err)
		}
	}
	loadMetadata()
	initPrompts()
	cleanupOrphanedMetadata()
	go probeExistingMedia()
	go sweeper()
	go chunkSweeper()

	activePreset := getActiveVisionPreset()
	log.Printf("klipbord %s listening on :%s (data=%s, max_upload=%dMB, vision=%t, preset=%s)", version, port, dataDir, maxUploadMB, visionEnabled, activePreset.Name)
	if err := http.ListenAndServe(":"+port, NewHandler()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// detectedBaseURL chooses a reachable LAN address for links when BASE_URL is unset.
// Deployments behind a reverse proxy should set BASE_URL explicitly.
func detectedBaseURL(port string) string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return defaultBaseURL
	}

	addresses := make([]net.Addr, 0)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		interfaceAddresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		addresses = append(addresses, interfaceAddresses...)
	}
	return baseURLFromAddresses(port, addresses)
}

// baseURLFromAddresses derives a base URL from eligible interface addresses.
func baseURLFromAddresses(port string, addresses []net.Addr) string {
	candidates := make([]string, 0, len(addresses))
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		candidates = append(candidates, ip.String())
	}
	if len(candidates) == 0 {
		return "http://localhost:" + port
	}

	sort.Strings(candidates)
	return "http://" + candidates[0] + ":" + port
}

// NewHandler returns the HTTP handler for Klipbord's web UI and API.
func NewHandler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/clip", webUIHandler)
	mux.HandleFunc("/persist", webUIHandler)
	mux.HandleFunc("/config", webUIHandler)
	mux.HandleFunc("/mcp-web", webUIHandler)
	mux.HandleFunc("/rest-web", webUIHandler)
	mux.HandleFunc("/manifest.json", manifestHandler)
	mux.HandleFunc("/sw.js", swHandler)
	mux.HandleFunc("/icon.svg", iconHandler)
	mux.HandleFunc("/api/files", apiFilesHandler)
	mux.HandleFunc("/api/files/", apiFileHandler)
	mux.HandleFunc("/api/analyze/", apiAnalyzeHandler)
	mux.HandleFunc("/api/config/vision", apiVisionConfigHandler)
	mux.HandleFunc("/api/config/vision/models", apiVisionModelsHandler)
	mux.HandleFunc("/api/config/vision/", apiVisionConfigHandler)
	mux.HandleFunc("/api/vision/test", apiVisionTestHandler)
	mux.HandleFunc("/api/vision/test-matrix/status", apiVisionTestMatrixStatusHandler)
	mux.HandleFunc("/api/vision/test-matrix", apiVisionTestMatrixHandler)
	mux.HandleFunc("/api/vision/runtime", apiVisionRuntimeHandler)
	mux.HandleFunc("/api/vision/compare", apiVisionCompareHandler)
	mux.HandleFunc("/api/vision/compare-prompts", apiVisionComparePromptsHandler)
	mux.HandleFunc("/api/prompts", apiPromptsHandler)
	mux.HandleFunc("/api/prompts/", apiPromptHandler)
	mux.HandleFunc("/api/text", apiTextHandler)
	mux.HandleFunc("/api/text/", apiTextItemHandler)
	mux.HandleFunc("/api/upload", apiUploadHandler)
	mux.HandleFunc("/api/upload/archive", apiArchiveFolderHandler)
	mux.HandleFunc("/api/upload/init", apiUploadInitHandler)
	mux.HandleFunc("/api/upload/chunk", apiUploadChunkHandler)
	mux.HandleFunc("/api/upload/status/", apiUploadStatusHandler)
	mux.HandleFunc("/api/upload/complete", apiUploadCompleteHandler)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/identity", identityHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/version", versionHandler)
	mux.HandleFunc("/api/update-check", updateCheckHandler)
	mux.HandleFunc("/api/logs", apiLogsHandler)
	mux.HandleFunc("/api/openapi.json", openapiSpecHandler)
	mux.HandleFunc("/swagger", swaggerHandler)
	mux.HandleFunc("/mcp", mcpHandler)
	mux.HandleFunc("/mcp/", mcpHandler)
	mux.HandleFunc("/link/", directLinkHandler)
	return mux
}

func swaggerHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(assets.SwaggerHTML)
}

func envOr(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
