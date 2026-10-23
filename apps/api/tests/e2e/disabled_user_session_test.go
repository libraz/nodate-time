package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// users.enabled = FALSE is the shared contract's soft delete, and every user
// lookup already treats such a user as gone. A session opened before the user
// was disabled must stop with it: no calendar route, no admin route, and no
// refresh that would keep it going.
func TestDisabledUserSessionStopsWorking(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	user := helpers.NewTenant(t, testServerURL)
	access, refresh := signIn(t, user)

	helpers.MakeInstanceAdmin(t, testDB, user.UserID)

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/admin/allowed-emails", access, nil)
	require.Equal(t, http.StatusOK, status, "the grant should work while the user is enabled")

	_, err := testDB.Exec(`UPDATE users SET enabled = FALSE WHERE email = ?`, user.Email)
	require.NoError(t, err)

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars", access, nil)
	require.Equal(t, http.StatusUnauthorized, status, "a calendar route must refuse a disabled user's session")

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/admin/allowed-emails", access, nil)
	require.Equal(t, http.StatusUnauthorized, status, "an admin route must refuse a disabled user's session")

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh})
	require.Equal(t, http.StatusUnauthorized, status, "refresh must issue nothing: %s", string(raw))
}
