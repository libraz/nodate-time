package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

type ctxKey int

const (
	ctxKeyActorUserID ctxKey = iota
	ctxKeySessionID
)

// WithActor stores the authenticated user ID in the context.
func WithActor(ctx context.Context, userID uint32) context.Context {
	return context.WithValue(ctx, ctxKeyActorUserID, userID)
}

// ActorFromContext retrieves the authenticated user ID.
func ActorFromContext(ctx context.Context) (uint32, bool) {
	v, ok := ctx.Value(ctxKeyActorUserID).(uint32)
	return v, ok
}

// WithSession stores the session the request authenticated with, so a
// handler that revokes sessions can spare the current one.
func WithSession(ctx context.Context, sessionID uint32) context.Context {
	return context.WithValue(ctx, ctxKeySessionID, sessionID)
}

// SessionFromContext retrieves the authenticated session ID.
func SessionFromContext(ctx context.Context) (uint32, bool) {
	v, ok := ctx.Value(ctxKeySessionID).(uint32)
	return v, ok
}

// RequireAuth is middleware that validates the JWT Bearer token.
func RequireAuth(jwtSecret string, queries *generated.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_MISSING", "Authorization header is required")
				return
			}

			tok, ok := strings.CutPrefix(header, "Bearer ")
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Bearer token is invalid")
				return
			}

			claims, err := auth.ValidateToken(tok, jwtSecret)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Bearer token is invalid or expired")
				return
			}
			// The signature alone proves only that this token was issued at
			// some point. The session row is what says it is still good, so
			// a revoked session stops the token here rather than when it
			// eventually expires.
			sessionPub, err := uuid.Parse(claims.SessionID)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Bearer token is invalid or expired")
				return
			}
			session, err := queries.GetLiveSession(r.Context(), sessionPub[:])
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Bearer token is invalid or expired")
				return
			}

			// Whose session it is comes from the row, not from the token.
			ctx := WithActor(r.Context(), session.UserID)
			ctx = WithSession(ctx, session.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
