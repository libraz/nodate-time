package middleware

import (
	"net/http"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// RequireAdmin gates access to platform admin endpoints. It assumes RequireAuth
// has already populated the actor in context, then checks the user's is_admin
// flag.
func RequireAdmin(q *generated.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := ActorFromContext(r.Context())
			if !ok {
				http.Error(w, `{"status":401,"code":"AUTH.TOKEN_INVALID","message":"Authentication required"}`, http.StatusUnauthorized)
				return
			}
			user, err := q.GetUserByID(r.Context(), userID)
			if err != nil {
				http.Error(w, `{"status":403,"code":"AUTH.ADMIN_REQUIRED","message":"Admin privileges required"}`, http.StatusForbidden)
				return
			}
			if !user.IsAdmin {
				http.Error(w, `{"status":403,"code":"AUTH.ADMIN_REQUIRED","message":"Admin privileges required"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
