package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// signIn opens a second session for an existing account and returns its
// credentials, so a test can act as two devices of the same person.
func signIn(t *testing.T, tt *helpers.TestTenant) (accessToken, refreshToken string) {
	t.Helper()
	var creds struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": tt.Email, "password": tt.Password}, &creds)
	require.NotEmpty(t, creds.Token)
	require.NotEmpty(t, creds.RefreshToken)
	return creds.Token, creds.RefreshToken
}

// TestSignOutEndsTheSession verifies that signing out revokes the session on
// the server, so a token copied from a shared machine stops working rather
// than staying good until it expires on its own.
func TestSignOutEndsTheSession(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	access, refresh := signIn(t, owner)

	before, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", access, nil)
	require.Equal(t, 200, before)

	logout, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/logout", access, nil)
	require.True(t, logout >= 200 && logout < 300)

	after, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", access, nil)
	require.Equal(t, 401, after, "an access token must stop working once its session is revoked")

	// And the refresh token cannot resurrect it: otherwise signing out would
	// only pause access for whoever also copied that value.
	refreshStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh})
	require.Equal(t, 401, refreshStatus)

	// The account's other sessions are untouched.
	other, _ := signIn(t, owner)
	stillGood, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", other, nil)
	require.Equal(t, 200, stillGood)
}

// TestRefreshOutlivesTheAccessToken verifies that the refresh token is issued
// on every sign-in path and buys a working access token, which is what keeps a
// signed-in browser from being thrown out when the access token expires.
func TestRefreshOutlivesTheAccessToken(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	_, refresh := signIn(t, owner)

	var renewed struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh}, &renewed)
	require.NotEmpty(t, renewed.Token)
	require.NotEmpty(t, renewed.RefreshToken)
	require.NotEqual(t, refresh, renewed.RefreshToken, "the refresh token must rotate")

	works, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", renewed.Token, nil)
	require.Equal(t, 200, works)

	// The one just spent stops working, so a stolen copy cannot be used
	// alongside the legitimate one.
	reused, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh})
	require.Equal(t, 401, reused)
}

// TestSessionListIsReachableAndOwn verifies that a person can see their live
// sign-ins, tell which one they are looking at, and end another -- and that
// the list reaches nobody else's devices.
func TestSessionListIsReachableAndOwn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	stranger := helpers.NewTenant(t, testServerURL)
	second, _ := signIn(t, owner)

	type sessionItem struct {
		ID        string `json:"id"`
		Current   bool   `json:"current"`
		UserAgent string `json:"userAgent"`
	}
	var list []sessionItem
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user/sessions", second, nil, &list)
	require.GreaterOrEqual(t, len(list), 2, "registration and this sign-in are both sessions")

	current := 0
	var otherID string
	for _, s := range list {
		if s.Current {
			current++
			continue
		}
		otherID = s.ID
	}
	require.Equal(t, 1, current, "exactly one session is the one being used")
	require.NotEmpty(t, otherID)

	// The device hint is recorded, or the list cannot do the one job it has.
	require.NotEmpty(t, list[0].UserAgent)

	// A stranger cannot end somebody else's session, and is told the same
	// thing they would be told about one that does not exist.
	forbidden, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/user/sessions/"+otherID, stranger.AccessToken, nil)
	require.Equal(t, 404, forbidden)

	revoke, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/user/sessions/"+otherID, second, nil)
	require.True(t, revoke >= 200 && revoke < 300)

	var after []sessionItem
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user/sessions", second, nil, &after)
	for _, s := range after {
		require.NotEqual(t, otherID, s.ID, "a revoked session must leave the list")
	}
}
