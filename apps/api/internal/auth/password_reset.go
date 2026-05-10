package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateResetToken returns a random URL-safe token and its sha256 hash.
// The plaintext token is sent via email; only the hash is stored in DB.
func GenerateResetToken() (token string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(h[:])
	return token, hash, nil
}

// HashResetToken computes the sha256 hash of a token (for lookups).
func HashResetToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// RandomHex returns n bytes of crypto-random data, hex-encoded.
// Returns an error rather than silently producing zeroed output on RNG failure.
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
