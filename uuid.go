package main

import (
	"crypto/rand"
	"fmt"
)

func NewUUIDv4() (string, error) {
	uuid := make([]byte, 16)

	// Fill the 16-byte slice with random bytes
	if _, err := rand.Read(uuid); err != nil {
		return "", err
	}

	// Set version to 4 (bits 4-7 of byte 6 to 0100)
	uuid[6] = (uuid[6] & 0x0f) | 0x40

	// Set variant to RFC 4122 (bits 6-7 of byte 8 to 10)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	// Format as standard UUID string: 8-4-4-4-12 hex format
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16]), nil
}
