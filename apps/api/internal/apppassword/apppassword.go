// Package apppassword generates and normalises the per-device passwords a
// calendar client presents over HTTP Basic. A password is "ndt_" followed by
// 32 Crockford base32 characters (160 bits); only its SHA-256 digest is ever
// stored. A password handed out through a profile link is derived from the
// link rather than drawn at random, so the link yields the same password
// until the device first syncs.
package apppassword

import (
	"crypto/hmac"
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

// rawLen is the bytes behind a password body: 32 chars x 5 bits = 160 bits,
// so no bits are discarded.
const rawLen = bodyLen * bitsPerEnc / 8

// Generate returns a new password, its display prefix and the hash to store.
func Generate() (plain, prefix, hash string, err error) {
	var raw [rawLen]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return "", "", "", fmt.Errorf("read random bytes: %w", err)
	}
	plain = encode(raw)
	return plain, plain[:PrefixLen], Hash(plain), nil
}

// Derive returns the password a profile link stands for: the leading bytes of
// HMAC(key, nonce || ticketID), encoded as Generate encodes random bytes. The
// results keep Generate's contract.
func Derive(key, nonce, ticketID []byte) (plain, prefix, hash string) {
	mac := hmac.New(sha256.New, key)
	mac.Write(nonce)
	mac.Write(ticketID)
	var raw [rawLen]byte
	copy(raw[:], mac.Sum(nil))
	plain = encode(raw)
	return plain, plain[:PrefixLen], Hash(plain)
}

// encode writes raw as the prefix and 32 Crockford base32 characters.
func encode(raw [rawLen]byte) string {
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
	return b.String()
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
