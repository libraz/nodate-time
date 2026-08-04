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
type Claims struct {
	UserID    uint32 `json:"uid"`
	SessionID uint32 `json:"sid"`
	jwt.RegisteredClaims
}

func GenerateToken(userID, sessionID uint32, secret string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:    userID,
		SessionID: sessionID,
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
