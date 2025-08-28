package helpers

import (
	"crypto/rand"
	"encoding/hex"
)

func GenerateID(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
