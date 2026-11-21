package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func localLoginStatus(t *testing.T, email, password string) int {
	t.Helper()
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	return status
}

// A password sign-in finds the account by the address on the user row, not by
// the subject its local identity was created with.
func TestLocalLoginMatchesTheUserEmail(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	// Another application created the identity under a subject that is not the
	// address; the user row still holds the address.
	res, err := testDB.Exec(
		`UPDATE identities SET subject = CONCAT('legacy-', HEX(public_id))
		 WHERE provider = 'local' AND user_id = (SELECT id FROM users WHERE email = ?)`, tt.Email)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	require.Equal(t, http.StatusOK, localLoginStatus(t, tt.Email, tt.Password))
	require.Equal(t, http.StatusOK, localLoginStatus(t, strings.ToUpper(tt.Email), tt.Password),
		"the address is matched without regard to case")
	require.Equal(t, http.StatusUnauthorized, localLoginStatus(t, tt.Email, tt.Password+"x"))
}

// An address changed on the user row is the one that signs in afterwards.
func TestLocalLoginFollowsAChangedUserEmail(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	moved := "moved-" + tt.Email

	_, err := testDB.Exec(`UPDATE users SET email = ? WHERE email = ?`, moved, tt.Email)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, localLoginStatus(t, moved, tt.Password))
	require.Equal(t, http.StatusUnauthorized, localLoginStatus(t, tt.Email, tt.Password),
		"the address the identity was created under no longer names the account")
}

// A user with no local identity cannot sign in with a password, whatever the
// address and password are.
func TestLocalLoginRefusesAnAccountWithoutALocalIdentity(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	_, err := testDB.Exec(
		`DELETE FROM identities WHERE provider = 'local'
		 AND user_id = (SELECT id FROM users WHERE email = ?)`, tt.Email)
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized, localLoginStatus(t, tt.Email, tt.Password))
}
