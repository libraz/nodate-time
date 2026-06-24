package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GeneratePKCE returns a PKCE code_verifier and its S256 code_challenge
// (RFC 7636). The verifier is high-entropy URL-safe random; the challenge is
// base64url(sha256(verifier)) without padding, as required by the spec.
func GeneratePKCE() (verifier string, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	challenge = S256Challenge(verifier)
	return verifier, challenge, nil
}

// S256Challenge computes the PKCE S256 code_challenge for a verifier:
// base64url(sha256(verifier)) without padding.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
