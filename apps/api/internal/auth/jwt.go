package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenTTL is deliberately shorter than the session it belongs to.
// The session row is the revocation point; this only bounds how long a
// token stays usable between checks.
const AccessTokenTTL = 24 * time.Hour

// Claims names the session the token was issued for, not a version counter
// on the user. Revocation is per-session, so signing one device out leaves
// the others alone -- a counter can only invalidate every token a user
// holds at once, which turns "log out this browser" into "log out
// everywhere".
//
// The session is named by its public id, and the user is not named at all:
// the session row says whose it is, so carrying the user as well would add a
// second answer that could disagree with the first. Internal ids stay out of
// the token because a token is a value a client holds, and those ids are one
// sequence per deployment.
type Claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

func GenerateToken(sessionPublicID string, secret string) (string, error) {
	now := time.Now()
	claims := Claims{
		SessionID: sessionPublicID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenStr string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		// Pin the signing method to HMAC to defend against algorithm-confusion
		// attacks (e.g. a token forged with alg=none or an RS256/HS256 swap).
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	return claims, nil
}
