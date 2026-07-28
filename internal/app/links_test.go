package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"report.pdf", "report.pdf"},
		{"sub/dir/file.txt", "file.txt"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"normal name.gif", "normal name.gif"},
		{"path\\with\\backslashes", "pathwithbackslashes"}, // backslashes are stripped
		{"\x00null.bin", "null.bin"},
	}
	for _, tc := range cases {
		got := sanitizeFilename(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, expected %q", tc.input, got, tc.want)
		}
	}
}

func TestLinkURLEdgeCases(t *testing.T) {
	originalBaseURL := baseURL
	baseURL = "https://kb.test"
	t.Cleanup(func() { baseURL = originalBaseURL })

	// No name → bare /{id}
	if got, want := linkURL("abc123def456"), "https://kb.test/abc123def456"; got != want {
		t.Errorf("linkURL no name = %q, expected %q", got, want)
	}
	// Empty name → bare /{id}
	if got, want := linkURL("abc123def456", ""), "https://kb.test/abc123def456"; got != want {
		t.Errorf("linkURL empty name = %q, expected %q", got, want)
	}
	// Name that sanitizes to empty ("..") → bare /{id}
	if got, want := linkURL("abc123def456", ".."), "https://kb.test/abc123def456"; got != want {
		t.Errorf("linkURL '..' name = %q, expected %q", got, want)
	}
	// Trailing slash in baseURL is trimmed
	baseURL = "https://kb.test/"
	if got, want := linkURL("abc123def456", "file.gif"), "https://kb.test/abc123def456/file.gif"; got != want {
		t.Errorf("linkURL with trailing slash baseURL = %q, expected %q", got, want)
	}
}

func TestLooksLikeItemID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"abc123def456", true},   // 12 lowercase alphanumeric
		{"aaaaaaaaaaaa", true},   // 12 lowercase
		{"123456789012", true},   // 12 digits
		{"ABC123def456", false},  // uppercase
		{"abc123def45", false},   // 11 chars (too short)
		{"abc123def4567", false}, // 13 chars (too long)
		{"abc-123def45", false},  // hyphen
		{"", false},
		{"abc/def", false},
	}
	for _, tc := range cases {
		got := looksLikeItemID(tc.input)
		if got != tc.want {
			t.Errorf("looksLikeItemID(%q) = %v, expected %v", tc.input, got, tc.want)
		}
	}
}

func TestLooksLikeIDWithFilename(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"abc123def456/file.gif", true},
		{"abc123def456/report.pdf", true},
		{"abc123def456", false},       // no slash
		{"/file.gif", false},          // empty ID part
		{"short/file.gif", false},     // ID too short
		{"ABC123def456/f.txt", false}, // uppercase ID
		{"", false},
	}
	for _, tc := range cases {
		got := looksLikeIDWithFilename(tc.input)
		if got != tc.want {
			t.Errorf("looksLikeIDWithFilename(%q) = %v, expected %v", tc.input, got, tc.want)
		}
	}
}

func TestIsGenericMimeType(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"application/octet-stream", true},
		{"application/x-www-form-urlencoded", true},
		{"image/gif", false},
		{"text/plain", false},
		{"application/pdf", false},
	}
	for _, tc := range cases {
		got := isGenericMimeType(tc.input)
		if got != tc.want {
			t.Errorf("isGenericMimeType(%q) = %v, expected %v", tc.input, got, tc.want)
		}
	}
}

func TestDetectMimeTypeFromExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.gif")
	if err := os.WriteFile(path, []byte("not real gif"), 0644); err != nil {
		t.Fatal(err)
	}
	got := detectMimeType("application/octet-stream", "animation.gif", path)
	if got != "image/gif" {
		t.Errorf("detectMimeType .gif = %q, expected image/gif", got)
	}
}

func TestDetectMimeTypeFromSniffing(t *testing.T) {
	dir := t.TempDir()
	// Write a real PNG header so content sniffing detects image/png.
	// Use no extension so extension lookup doesn't short-circuit.
	path := filepath.Join(dir, "upload")
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if err := os.WriteFile(path, pngHeader, 0644); err != nil {
		t.Fatal(err)
	}
	got := detectMimeType("application/octet-stream", "upload", path)
	if got != "image/png" {
		t.Errorf("detectMimeType sniffed = %q, expected image/png", got)
	}
}

func TestDetectMimeTypeFallsBackToOctetStream(t *testing.T) {
	dir := t.TempDir()
	// No recognizable extension, no sniffable magic bytes
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0644); err != nil {
		t.Fatal(err)
	}
	got := detectMimeType("", "data", path)
	if got != "application/octet-stream" {
		t.Errorf("detectMimeType fallback = %q, expected application/octet-stream", got)
	}
}

func TestDetectMimeTypeKeepsClientTypeWhenSpecific(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.dat")
	if err := os.WriteFile(path, []byte{0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}
	// Client sent a specific type — should be kept even though extension
	// and sniffing would say something else.
	got := detectMimeType("application/x-custom", "file.dat", path)
	if got != "application/x-custom" {
		t.Errorf("detectMimeType with specific fallback = %q, expected application/x-custom", got)
	}
}

func TestDetectMimeTypeSniffingReturnsGeneric(t *testing.T) {
	dir := t.TempDir()
	// Content that sniffs as application/octet-stream (random bytes)
	// with no extension — should fall through to the fallback.
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03, 0x04}, 0644); err != nil {
		t.Fatal(err)
	}
	got := detectMimeType("application/x-custom", "blob", path)
	if got != "application/x-custom" {
		t.Errorf("detectMimeType generic sniff = %q, expected application/x-custom", got)
	}
}

func TestDetectMimeTypeAudioExtensions(t *testing.T) {
	dir := t.TempDir()
	// init() in handlers_uploads.go registers canonical types via
	// mime.AddExtensionType, so these are consistent across platforms.
	cases := []struct {
		ext, want string
	}{
		{".mp3", "audio/mpeg"},
		{".wav", "audio/wav"},
		{".ogg", "audio/ogg"},
		{".flac", "audio/flac"},
		{".m4a", "audio/mp4"},
		{".aac", "audio/aac"},
		{".opus", "audio/opus"},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, "test"+tc.ext)
		if err := os.WriteFile(path, []byte("fake audio"), 0644); err != nil {
			t.Fatal(err)
		}
		got := detectMimeType("application/octet-stream", "test"+tc.ext, path)
		if got != tc.want {
			t.Errorf("detectMimeType(%s) = %q, expected %q", tc.ext, got, tc.want)
		}
	}
}

func TestDetectMimeTypeVideoExtensions(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		ext, want string
	}{
		{".mp4", "video/mp4"},
		{".webm", "video/webm"},
		{".mkv", "video/x-matroska"},
		{".avi", "video/x-msvideo"},
		{".mov", "video/quicktime"},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, "test"+tc.ext)
		if err := os.WriteFile(path, []byte("fake video"), 0644); err != nil {
			t.Fatal(err)
		}
		got := detectMimeType("application/octet-stream", "test"+tc.ext, path)
		if got != tc.want {
			t.Errorf("detectMimeType(%s) = %q, expected %q", tc.ext, got, tc.want)
		}
	}
}
