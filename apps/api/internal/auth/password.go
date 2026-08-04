package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Passwords are stored as Argon2id in the PHC string format:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
//
// The format is not an internal detail. The shared contract documents
// identities.password_hash as carrying exactly this, so another product
// pointed at the same database can verify a password this one wrote. A
// hash in some other scheme is indistinguishable from a wrong password to
// that reader: it would simply refuse a correct credential.
//
// The parameters are encoded in every hash rather than read from a constant
// at verification time, so raising them later leaves existing hashes
// verifiable instead of locking their owners out.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var errBadHashFormat = errors.New("auth: password hash is not in Argon2id PHC format")

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// CheckPassword reports whether password matches the stored hash. A hash
// that cannot be parsed is treated as no match rather than as an error: a
// caller able to tell "malformed" from "wrong" would leak which accounts
// have an unusable credential.
func CheckPassword(password, hash string) bool {
	salt, want, t, memory, threads, err := parseHash(hash)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func parseHash(hash string) (salt, key []byte, t, memory uint32, threads uint8, err error) {
	parts := strings.Split(hash, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errBadHashFormat
	}
	var version int
	if _, serr := fmt.Sscanf(parts[2], "v=%d", &version); serr != nil || version != argon2.Version {
		return nil, nil, 0, 0, 0, errBadHashFormat
	}
	if _, serr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &t, &threads); serr != nil {
		return nil, nil, 0, 0, 0, errBadHashFormat
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, errBadHashFormat
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, errBadHashFormat
	}
	return salt, key, t, memory, threads, nil
}
