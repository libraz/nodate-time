package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
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
func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, `{"status":401,"code":"AUTH.TOKEN_MISSING","message":"Authorization header is required"}`, http.StatusUnauthorized)
				return
			}

			tok, ok := strings.CutPrefix(header, "Bearer ")
			if !ok {
				http.Error(w, `{"status":401,"code":"AUTH.TOKEN_INVALID","message":"Bearer token is invalid"}`, http.StatusUnauthorized)
				return
			}

			claims, err := auth.ValidateToken(tok, jwtSecret)
			if err != nil {
				http.Error(w, `{"status":401,"code":"AUTH.TOKEN_INVALID","message":"Bearer token is invalid or expired"}`, http.StatusUnauthorized)
				return
			}

			ctx := WithActor(r.Context(), claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
