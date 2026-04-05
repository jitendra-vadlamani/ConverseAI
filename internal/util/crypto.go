package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const EncodeInputPrefix = "v1:aes256gcm:"

// EncryptString encrypts a plaintext string using AES-256-GCM.
// The key must be exactly 32 bytes long.
func EncryptString(plaintext, key string) (string, error) {
	if plaintext == "" {
		return "", nil // Nothing to encrypt
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncodeInputPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString decrypts a ciphertext string using AES-256-GCM.
// Returns the original text if it doesn't match the prefix (e.g. legacy data).
func DecryptString(ciphertext, key string) (string, error) {
	if ciphertext == "" || !strings.HasPrefix(ciphertext, EncodeInputPrefix) {
		return ciphertext, nil // Return as-is if empty or not encrypted
	}

	b64Data := strings.TrimPrefix(ciphertext, EncodeInputPrefix)
	encryptedData, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(encryptedData) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := encryptedData[:nonceSize], encryptedData[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
