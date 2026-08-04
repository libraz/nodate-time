package middleware

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// writeJSONError writes a Huma-shaped JSON error body with the correct
// Content-Type, matching what huma.StatusError responses look like so clients
// can rely on a single parsing path regardless of which layer rejected the
// request.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"status":%d,"code":%q,"message":%q}`, status, code, message)
}

// RequireAdmin gates access to platform admin endpoints. It assumes
// RequireAuth has already populated the actor in context, then looks for a
// live grant in instance_admins.
//
// The grant is a row rather than a flag on the user so that revoking it
// leaves a record that it was ever held, and by whom -- a flag flipped back
// to false says nothing about what happened while it was true.
func RequireAdmin(q *generated.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := ActorFromContext(r.Context())
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "AUTH.TOKEN_INVALID", "Authentication required")
				return
			}
			isAdmin, err := q.IsInstanceAdmin(r.Context(), userID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// The token's subject no longer exists (deleted account); treat
					// like any other unauthorized access rather than a server error.
					writeJSONError(w, http.StatusForbidden, "AUTH.ADMIN_REQUIRED", "Admin privileges required")
					return
				}
				slog.ErrorContext(r.Context(), "failed to check instance admin grant", "userID", userID, "error", err)
				writeJSONError(w, http.StatusInternalServerError, "INTERNAL.UNEXPECTED", "An unexpected error occurred")
				return
			}
			if !isAdmin {
				writeJSONError(w, http.StatusForbidden, "AUTH.ADMIN_REQUIRED", "Admin privileges required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
