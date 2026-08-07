package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

// packIP converts a client address to the 16-byte form the column stores.
// IPv4 goes in as its IPv4-mapped IPv6 address, so every row has one width
// and unpacking needs no length check. An address that does not parse — or
// an empty one, which is what a request with no usable remote address
// yields — is stored as NULL rather than failing the sign-in.
func packIP(ip string) []byte {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	return parsed.To16()
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
// token has to name the row: that is what lets a later request ask whether
// this particular sign-in is still good, and what makes signing one device
// out leave the others alone.
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

	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		_, err := q.CreateSession(ctx, generated.CreateSessionParams{
			PublicID:    pubID[:],
			UserID:      userID,
			RefreshHash: hashRefreshToken(refresh),
			UserAgent:   nullString(userAgent),
			IpAddress:   packIP(ipAddress),
			ExpiresAt:   expiresAt,
		})
		if err != nil {
			return err
		}
		return q.TouchUserLastLogin(ctx, userID)
	})
	if err != nil {
		return Credentials{}, err
	}

	token, err := auth.GenerateToken(pubID.String(), deps.JWTSecret)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Token: token, RefreshToken: refresh, ExpiresAt: expiresAt}, nil
}

// errRefreshReplayed reports a refresh token presented after it was already
// traded in. It is distinct from sql.ErrNoRows so the caller can tell a
// token that was once real from one that never was, even though both end
// the same way for whoever sent it.
var errRefreshReplayed = errors.New("refresh token already spent")

// rotateSession trades a refresh token for a new pair.
//
// The exchange is one statement matching on the hash being spent, so two
// requests carrying the same token cannot both succeed: whichever reaches
// the row first replaces the hash, and the other matches nothing. Reading
// the row and then updating it -- which is what this did -- let both pass,
// and the loser's credentials were revoked by the winner's write with
// nothing recording that it had happened.
//
// A token that matches nothing may still be one this session already spent.
// That is the shape a leaked refresh token takes: the copy is used and then
// the original, or the other way round. It cannot be told apart from a
// client retrying an exchange whose reply it never received, and both mean
// the token is in more hands than one place can account for, so the session
// is closed rather than continued.
func rotateSession(ctx context.Context, deps Deps, refreshToken string) (Credentials, uint32, error) {
	spent := hashRefreshToken(refreshToken)
	next, err := newRefreshToken()
	if err != nil {
		return Credentials{}, 0, err
	}
	nextHash := hashRefreshToken(next)
	expiresAt := time.Now().Add(refreshTokenTTL)

	res, err := deps.Queries.RotateSessionByHash(ctx, generated.RotateSessionByHashParams{
		RefreshHash:   nextHash,
		ExpiresAt:     expiresAt,
		RefreshHash_2: spent,
	})
	if err != nil {
		return Credentials{}, 0, err
	}
	rotated, err := res.RowsAffected()
	if err != nil {
		return Credentials{}, 0, err
	}
	if rotated == 0 {
		return Credentials{}, 0, closeReplayedSession(ctx, deps, spent)
	}

	// Read back by the hash just written. Nobody else can hold that value --
	// it has not left this function yet -- so no second exchange can be
	// between the two statements.
	session, err := deps.Queries.GetSessionByRefreshHash(ctx, nextHash)
	if err != nil {
		return Credentials{}, 0, err
	}

	token, err := auth.GenerateToken(pubIDToHex(session.PublicID), deps.JWTSecret)
	if err != nil {
		return Credentials{}, 0, err
	}
	return Credentials{Token: token, RefreshToken: next, ExpiresAt: expiresAt}, session.UserID, nil
}

// closeReplayedSession decides what a refresh token that opened no session
// was. If it is the one a live session last traded in, it is being presented
// a second time: that session is revoked, which stops the access tokens
// naming it at their next request and makes the refresh token issued in its
// place worthless too.
//
// The refusal handed back is the same either way. Telling the sender that
// their replay was recognised only informs whoever is holding a token they
// should not have.
func closeReplayedSession(ctx context.Context, deps Deps, spent string) error {
	session, err := deps.Queries.GetSessionByPrevRefreshHash(ctx,
		sql.NullString{String: spent, Valid: true})
	if err != nil {
		// Including sql.ErrNoRows, which is the ordinary case: a value no
		// session ever issued.
		return err
	}
	if err := deps.Queries.RevokeSession(ctx, session.ID); err != nil {
		return err
	}
	slog.WarnContext(ctx, "refresh token replayed, session revoked",
		"sessionID", session.ID, "userID", session.UserID)
	return errRefreshReplayed
}
