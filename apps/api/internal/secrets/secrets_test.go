package secrets

import (
	"encoding/hex"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	// non-zero key
	keyBytes, _ := hex.DecodeString(key)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	c, err := New(hex.EncodeToString(keyBytes))
	if err != nil || c == nil {
		t.Fatalf("New: %v c=%v", err, c)
	}

	plain := []byte("hello-secret-12345")
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(enc) == string(plain) {
		t.Fatal("ciphertext equals plaintext")
	}

	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("got %q want %q", dec, plain)
	}
}

func TestNewEmpty(t *testing.T) {
	c, err := New("")
	if err != nil {
		t.Fatalf("New(\"\"): %v", err)
	}
	if c != nil {
		t.Fatal("expected nil cipher for empty key")
	}
}

func TestNewBadKey(t *testing.T) {
	if _, err := New("not-hex-or-base64-of-right-length"); err == nil {
		t.Fatal("expected error")
	}
}
