package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func NewID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}
