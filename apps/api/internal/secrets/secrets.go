// Package secrets provides authenticated symmetric encryption (AES-256-GCM)
// for storing OAuth client secrets and other sensitive material in the DB.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Cipher encrypts and decrypts arbitrary byte slices.
type Cipher struct {
	aead cipher.AEAD
}

// New constructs a Cipher from a 32-byte key. The key may be passed as 64-char
// hex or as standard base64 (44 chars including padding). An empty key returns
// (nil, nil): callers should treat that as "encryption unavailable" rather
// than crash, and refuse writes that would create plaintext secrets.
func New(rawKey string) (*Cipher, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, nil
	}
	key, err := decodeKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("secrets: decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes (got %d)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func decodeKey(raw string) ([]byte, error) {
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("must be 32 bytes encoded as hex or base64")
}

// Encrypt seals plaintext and returns nonce || ciphertext.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("secrets: cipher unavailable (TC_SECRETS_KEY not set)")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// Decrypt reverses Encrypt.
func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("secrets: cipher unavailable")
	}
	ns := c.aead.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("secrets: ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return c.aead.Open(nil, nonce, ct, nil)
}

// Available reports whether encryption is configured.
func (c *Cipher) Available() bool {
	return c != nil
}
