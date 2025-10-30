package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a 128-bit random identifier encoded as hex.
func New() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
