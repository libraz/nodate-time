// Package apppassword generates and normalises the per-device passwords a
// calendar client presents over HTTP Basic. A password is "ndt_" followed by
// 32 Crockford base32 characters (160 random bits); only its SHA-256 digest is
// ever stored.
package apppassword

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const (
	// Prefix starts every app password.
	Prefix = "ndt_"
	// PrefixLen is how much of a password is kept in the clear, to tell
	// passwords apart in a list.
	PrefixLen = 8

	bodyLen    = 32
	groupLen   = 4
	crockford  = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	bitsPerEnc = 5
)

// Generate returns a new password, its display prefix and the hash to store.
func Generate() (plain, prefix, hash string, err error) {
	// 32 chars x 5 bits = 160 bits = 20 bytes, so no bits are discarded.
	var raw [bodyLen * bitsPerEnc / 8]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return "", "", "", fmt.Errorf("read random bytes: %w", err)
	}
	var b strings.Builder
	b.WriteString(Prefix)
	var acc uint32
	var bits int
	for _, v := range raw {
		acc = acc<<8 | uint32(v)
		bits += 8
		for bits >= bitsPerEnc {
			bits -= bitsPerEnc
			b.WriteByte(crockford[(acc>>bits)&31])
		}
		acc &= 1<<bits - 1
	}
	plain = b.String()
	return plain, plain[:PrefixLen], Hash(plain), nil
}

// Normalize trims s, drops spaces and hyphens, keeps the lowercase "ndt_"
// prefix and upper-cases the rest, so a password typed in groups or in the
// wrong case still matches.
func Normalize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	if len(s) >= len(Prefix) && strings.EqualFold(s[:len(Prefix)], Prefix) {
		return Prefix + strings.ToUpper(s[len(Prefix):])
	}
	return strings.ToUpper(s)
}

// Hash is the hex SHA-256 of the normalised password: the value stored and
// looked up.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(Normalize(plain)))
	return hex.EncodeToString(sum[:])
}

// Format splits the part after the prefix into hyphen-separated groups of
// four, for display.
func Format(plain string) string {
	body := strings.TrimPrefix(Normalize(plain), Prefix)
	var b strings.Builder
	b.WriteString(Prefix)
	for i := 0; i < len(body); i += groupLen {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(body[i:min(i+groupLen, len(body))])
	}
	return b.String()
}
