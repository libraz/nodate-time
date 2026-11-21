package blobref

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
)

// SHA256Len is the byte length of a SHA-256 digest.
const SHA256Len = sha256.Size

var (
	// ErrBadDigest is a declared digest that is not 64 lowercase hex characters.
	ErrBadDigest = errors.New("blobref: digest is not 64 lowercase hex characters")
	// ErrDigestMismatch is stored bytes whose SHA-256 differs from the declared digest.
	ErrDigestMismatch = errors.New("blobref: bytes do not match the declared digest")
	// ErrTooLarge is stored bytes beyond the size the upload was reserved for.
	ErrTooLarge = errors.New("blobref: bytes exceed the reserved size")
)

// ParseDigest decodes a declared SHA-256. Only lowercase hex of exactly 64
// characters is accepted, so one byte string has one spelling.
func ParseDigest(s string) ([]byte, error) {
	if len(s) != 2*SHA256Len {
		return nil, ErrBadDigest
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, ErrBadDigest
		}
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, ErrBadDigest
	}
	return b, nil
}

// VerifyReadBack reads r, the bytes stored for an upload, and checks them
// against the digest declared when it was reserved, reading at most limit+1
// bytes: ErrTooLarge when the object is longer than limit, ErrDigestMismatch
// when the digest differs. Matching the length to the reserved size is left to
// the caller, which stats the object.
func VerifyReadBack(r io.Reader, want []byte, limit int64) error {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return ErrTooLarge
	}
	if len(want) != SHA256Len || subtle.ConstantTimeCompare(h.Sum(nil), want) != 1 {
		return ErrDigestMismatch
	}
	return nil
}
