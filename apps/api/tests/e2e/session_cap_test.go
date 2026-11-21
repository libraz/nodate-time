package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

const liveSessionCap = 50

// Signing in past the cap ends the oldest session instead of refusing the new
// one, and leaves sessions another application issued alone.
func TestSessionCapRevokesTheOldestAndSparesOtherAudiences(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	first := tt.AccessToken

	// A session of another audience, as the sibling product would open one.
	_, err := testDB.Exec(
		`INSERT INTO sessions (public_id, user_id, audience, refresh_hash, expires_at)
		 SELECT UUID_TO_BIN(UUID()), id, 'flow', SHA2(UUID(), 256), NOW(3) + INTERVAL 1 DAY
		 FROM users WHERE email = ?`, tt.Email)
	require.NoError(t, err)

	tokens := []string{first}
	for len(tokens) < liveSessionCap {
		access, _ := signIn(t, tt)
		tokens = append(tokens, access)
	}

	var list []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user/sessions", tokens[len(tokens)-1], nil, &list)
	require.Len(t, list, liveSessionCap, "at the cap nothing has been revoked yet")
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", first, nil)
	require.Equal(t, http.StatusOK, status)

	// Two more sign-ins push the two oldest out.
	newest, _ := signIn(t, tt)
	newer, _ := signIn(t, tt)

	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user/sessions", newer, nil, &list)
	require.Len(t, list, liveSessionCap)

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", first, nil)
	require.Equal(t, http.StatusUnauthorized, status, "the oldest session is the one that goes")
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", tokens[1], nil)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", tokens[2], nil)
	require.Equal(t, http.StatusOK, status, "only as many are ended as the new sign-ins exceed the cap by")
	for _, token := range []string{newest, newer} {
		status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", token, nil)
		require.Equal(t, http.StatusOK, status)
	}

	var flowLive int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE u.email = ? AND s.audience = 'flow' AND s.revoked_at IS NULL`, tt.Email).Scan(&flowLive))
	require.Equal(t, 1, flowLive, "a session of another audience is not counted or revoked")
}
