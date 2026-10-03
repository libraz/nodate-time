package middleware

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

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
				apierrors.WriteSpec(w, apierrors.AuthTokenInvalid)
				return
			}
			isAdmin, err := q.IsInstanceAdmin(r.Context(), userID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// The token's subject no longer exists (deleted account); treat
					// like any other unauthorized access rather than a server error.
					apierrors.WriteSpec(w, apierrors.AuthAdminRequired)
					return
				}
				slog.ErrorContext(r.Context(), "failed to check instance admin grant", "userID", userID, "error", err)
				apierrors.WriteSpec(w, apierrors.InternalUnexpected)
				return
			}
			if !isAdmin {
				apierrors.WriteSpec(w, apierrors.AuthAdminRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
