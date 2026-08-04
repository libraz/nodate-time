package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
)

// refreshTokenTTL is how long a sign-in survives without being refreshed.
// It bounds the session row, which is what the access token is checked
// against on every request.
const refreshTokenTTL = 30 * 24 * time.Hour

// Credentials is what a successful sign-in hands back.
type Credentials struct {
	// Token is the short-lived access token. It names the session, so
	// revoking that session stops the token immediately rather than at
	// its own expiry.
	Token string
	// RefreshToken is the long-lived secret that trades for a new access
	// token. Only its hash is stored.
	RefreshToken string
	ExpiresAt    time.Time
}

// hashRefreshToken is what the sessions table stores. Reading every row
// yields no usable token.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newRefreshToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// startSession records a sign-in and issues the pair of tokens for it.
//
// The session row is created before the access token is signed, because the
// token has to carry the row's id: that is what lets a later request ask
// whether this particular sign-in is still good, and what makes signing one
// device out leave the others alone.
func startSession(ctx context.Context, deps Deps, userID uint32, userAgent, ipAddress string) (Credentials, error) {
	refresh, err := newRefreshToken()
	if err != nil {
		return Credentials{}, err
	}
	pubID, err := uuid.NewV7()
	if err != nil {
		return Credentials{}, err
	}
	expiresAt := time.Now().Add(refreshTokenTTL)

	var sessionID uint32
	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		res, err := q.CreateSession(ctx, generated.CreateSessionParams{
			PublicID:    pubID[:],
			UserID:      userID,
			RefreshHash: hashRefreshToken(refresh),
			UserAgent:   nullString(userAgent),
			IpAddress:   nullString(ipAddress),
			ExpiresAt:   expiresAt,
		})
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		sessionID = uint32(id)
		return q.TouchUserLastLogin(ctx, userID)
	})
	if err != nil {
		return Credentials{}, err
	}

	token, err := auth.GenerateToken(userID, sessionID, deps.JWTSecret)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Token: token, RefreshToken: refresh, ExpiresAt: expiresAt}, nil
}

// rotateSession trades a refresh token for a new pair. The old refresh
// token stops working: the row's hash is replaced rather than added to, so
// a stolen token cannot be used alongside the legitimate one.
func rotateSession(ctx context.Context, deps Deps, refreshToken string) (Credentials, uint32, error) {
	session, err := deps.Queries.GetSessionByRefreshHash(ctx, hashRefreshToken(refreshToken))
	if err != nil {
		return Credentials{}, 0, err
	}

	next, err := newRefreshToken()
	if err != nil {
		return Credentials{}, 0, err
	}
	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := deps.Queries.RotateSession(ctx, generated.RotateSessionParams{
		RefreshHash: hashRefreshToken(next),
		ExpiresAt:   expiresAt,
		ID:          session.ID,
	}); err != nil {
		return Credentials{}, 0, err
	}

	token, err := auth.GenerateToken(session.UserID, session.ID, deps.JWTSecret)
	if err != nil {
		return Credentials{}, 0, err
	}
	return Credentials{Token: token, RefreshToken: next, ExpiresAt: expiresAt}, session.UserID, nil
}
