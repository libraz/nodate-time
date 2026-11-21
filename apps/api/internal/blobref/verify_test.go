package blobref

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestParseDigest(t *testing.T) {
	sum := sha256.Sum256([]byte("x"))
	lower := hex.EncodeToString(sum[:])
	if got, err := ParseDigest(lower); err != nil || hex.EncodeToString(got) != lower {
		t.Fatalf("lowercase 64 = %x, %v", got, err)
	}
	for name, in := range map[string]string{
		"uppercase": strings.ToUpper(lower),
		"63 chars":  lower[:63],
		"65 chars":  lower + "a",
		"empty":     "",
		"non-hex":   strings.Repeat("g", 64),
	} {
		if _, err := ParseDigest(in); !errors.Is(err, ErrBadDigest) {
			t.Errorf("%s: err = %v, want ErrBadDigest", name, err)
		}
	}
}

func TestVerifyReadBack(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	other := sha256.Sum256([]byte("other"))
	size := int64(len(data))

	if err := VerifyReadBack(strings.NewReader(string(data)), sum[:], size); err != nil {
		t.Fatalf("matching bytes: %v", err)
	}
	if err := VerifyReadBack(strings.NewReader(string(data)), other[:], size); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("wrong digest: %v", err)
	}
	if err := VerifyReadBack(strings.NewReader(string(data)), sum[:], size-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the cap: %v", err)
	}
}

type countingReader struct{ n int64 }

func (c *countingReader) Read(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func TestVerifyReadBackStopsAtTheCap(t *testing.T) {
	r := &countingReader{}
	if err := VerifyReadBack(r, make([]byte, SHA256Len), 1000); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("endless stream: %v", err)
	}
	if r.n > 1000+32*1024 {
		t.Fatalf("read %d bytes for a 1000 byte cap", r.n)
	}
}
