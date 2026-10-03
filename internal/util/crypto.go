package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Ciphertext formats:
//
//	v1:aes256gcm:<b64>          legacy, no key id; every key in the ring is tried
//	v2:<kid>:<b64>              kid = first 8 hex chars of sha256(key)
const (
	legacyPrefix = "v1:aes256gcm:"
	v2Prefix     = "v2:"
)

// KeyRing encrypts with the primary key and decrypts with any known key, which
// is what makes DB_ENCRYPTION_KEY rotatable.
type KeyRing struct {
	primary  cipher.AEAD
	primaryK string
	byID     map[string]cipher.AEAD
	all      []cipher.AEAD
}

func NewKeyRing(primary string, old ...string) (*KeyRing, error) {
	kr := &KeyRing{byID: map[string]cipher.AEAD{}}
	for i, k := range append([]string{primary}, old...) {
		aead, err := newAEAD(k)
		if err != nil {
			return nil, err
		}
		id := keyID(k)
		if i == 0 {
			kr.primary, kr.primaryK = aead, id
		}
		if _, dup := kr.byID[id]; !dup {
			kr.byID[id] = aead
			kr.all = append(kr.all, aead)
		}
	}
	return kr, nil
}

func newAEAD(key string) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func keyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:4])
}

// PrimaryKeyID identifies the key new data is encrypted with.
func (k *KeyRing) PrimaryKeyID() string { return k.primaryK }

// Encrypt returns "" for "" so empty optional fields stay empty.
func (k *KeyRing) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, k.primary.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := k.primary.Seal(nonce, nonce, []byte(plaintext), nil)
	return v2Prefix + k.primaryK + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt returns values without a known prefix unchanged (legacy plaintext).
func (k *KeyRing) Decrypt(value string) (string, error) {
	switch {
	case strings.HasPrefix(value, v2Prefix):
		rest := strings.TrimPrefix(value, v2Prefix)
		id, b64, ok := strings.Cut(rest, ":")
		if !ok {
			return "", errors.New("malformed ciphertext")
		}
		aead, ok := k.byID[id]
		if !ok {
			return "", fmt.Errorf("no key with id %s in DB_ENCRYPTION_KEY / DB_ENCRYPTION_KEYS_OLD", id)
		}
		return open(aead, b64)
	case strings.HasPrefix(value, legacyPrefix):
		b64 := strings.TrimPrefix(value, legacyPrefix)
		for _, aead := range k.all {
			if pt, err := open(aead, b64); err == nil {
				return pt, nil
			}
		}
		return "", errors.New("no configured key decrypts this legacy value")
	default:
		return value, nil
	}
}

// NeedsRotation reports whether value is encrypted with something other than
// the primary key (or not encrypted at all).
func (k *KeyRing) NeedsRotation(value string) bool {
	if value == "" {
		return false
	}
	return !strings.HasPrefix(value, v2Prefix+k.primaryK+":")
}

func open(aead cipher.AEAD, b64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := aead.NonceSize()
	if len(data) < ns {
		return "", errors.New("ciphertext too short")
	}
	pt, err := aead.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}
