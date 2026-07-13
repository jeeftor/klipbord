package id

import "testing"

func TestNew(t *testing.T) {
	const length = 12

	value, err := New(length)
	if err != nil {
		t.Fatalf("New(%d) returned an error: %v", length, err)
	}
	if len(value) != length {
		t.Fatalf("New(%d) length = %d", length, len(value))
	}
	for _, char := range value {
		if !contains(char) {
			t.Fatalf("New(%d) returned invalid character %q", length, char)
		}
	}
}

func TestNewRejectsNonPositiveLength(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("New(0) returned nil error")
	}
}

func contains(char rune) bool {
	for _, allowed := range alphabet {
		if char == allowed {
			return true
		}
	}
	return false
}
