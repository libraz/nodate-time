package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

type ctxKey int

const (
	ctxKeyActorUserID ctxKey = iota
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
			user, err := queries.GetUserByID(r.Context(), claims.UserID)
			if err != nil || user.TokenVersion != claims.TokenVersion {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Bearer token is invalid or expired")
				return
			}

			ctx := WithActor(r.Context(), claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
