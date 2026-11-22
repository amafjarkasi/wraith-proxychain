package evade

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Canary generates a unique token to embed in traffic.
func Canary() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "CANARY-" + hex.EncodeToString(b)
}

// CanaryLeaked checks if a canary token appears in observed data (interception).
func CanaryLeaked(data, token string) bool {
	return token != "" && strings.Contains(string(data), token)
}
