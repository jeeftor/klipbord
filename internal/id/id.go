// Package id generates opaque identifiers for stored Klipbord items.
package id

import (
	"crypto/rand"
	"fmt"
	"io"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// New returns an opaque, URL-safe identifier of the requested length.
func New(length int) (string, error) {
	if length < 1 {
		return "", fmt.Errorf("identifier length must be positive")
	}

	result := make([]byte, length)
	limit := byte(256 - 256%len(alphabet))
	for i := range result {
		for {
			var randomByte [1]byte
			if _, err := io.ReadFull(rand.Reader, randomByte[:]); err != nil {
				return "", fmt.Errorf("read random identifier byte: %w", err)
			}
			if randomByte[0] < limit {
				result[i] = alphabet[randomByte[0]%byte(len(alphabet))]
				break
			}
		}
	}

	return string(result), nil
}
