package e2e

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The User-Agent is stored on the session row, a utf8mb4 column under strict
// mode. A long non-ASCII header cut mid-character, or one carrying bytes that
// are not UTF-8 at all, must not turn the sign-in into a server error.
func TestSignInWithAnAwkwardUserAgentStoresValidUTF8(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	userAgent := strings.Repeat("a", 254) + "あいう \xff"
	email := uniqueEmail()

	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPost,
		testServerURL+"/auth/register", "",
		map[string]any{"email": email, "password": "password123", "name": "UA User"},
		map[string]string{"User-Agent": userAgent},
	)
	require.Equal(t, http.StatusOK, status, "register: %s", string(raw))

	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost,
		testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": "password123"},
		map[string]string{"User-Agent": "Mozilla/5.0 \xfe\xff"},
	)
	require.Equal(t, http.StatusOK, status, "login: %s", string(raw))

	rows, err := testDB.Query(`
		SELECT s.user_agent FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE u.email = ?`, email)
	require.NoError(t, err)
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var stored string
		require.NoError(t, rows.Scan(&stored))
		require.True(t, utf8.ValidString(stored), "stored user agent is not UTF-8: %q", stored)
		seen++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 2, seen)
}
