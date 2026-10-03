package e2e

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// providerOnlyAccount signs a new account in through a provider that vouches
// for email, and returns the access token it was given.
func providerOnlyAccount(t *testing.T, email string) string {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"sub":"reset-%s","email":%q,"email_verified":true,"name":"Provider Only"}`, email, email)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	srv := newOAuthTestServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		Google: users.OAuthProviderConfig{
			ClientID:    "google-client",
			AuthURL:     provider.URL + "/authorize",
			TokenURL:    provider.URL + "/token",
			UserinfoURL: provider.URL + "/userinfo",
			Scopes:      "openid email profile",
		},
	})

	start := requestNoRedirect(t, http.MethodGet, srv.URL+"/auth/oauth/google/start", "")
	start.Body.Close()
	authorize, err := url.Parse(start.Header.Get("Location"))
	require.NoError(t, err)
	cookie := firstCookie(start, "oauth_state")
	require.NotNil(t, cookie)
	done := requestNoRedirect(t, http.MethodGet,
		srv.URL+"/auth/oauth/google/callback?code=c&state="+url.QueryEscape(authorize.Query().Get("state")), cookie.String())
	done.Body.Close()
	token, _ := tokensFromRedirect(t, done.Header.Get("Location"))
	require.NotEmpty(t, token)
	return token
}

// An account created through a provider has no password. A reset must not
// pretend to set one: it would sign the account out everywhere and leave it a
// password that does not exist.
func TestPasswordResetSetsNoPasswordForAProviderOnlyAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("provider-reset-%d@test.local", time.Now().UnixNano())
	session := providerOnlyAccount(t, email)

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
		map[string]any{"email": email}, nil)
	if msg, ok := testMailer.LastFor(email); ok {
		require.NotContains(t, msg.Subject, "Reset", "no reset link is sent to an account with no password")
	}

	// A token issued anyway -- before this rule, or by any other route --
	// must fail without spending itself or the account's sessions.
	token := fmt.Sprintf("provider-only-reset-%d", time.Now().UnixNano())
	_, err := testDB.Exec(
		`INSERT INTO password_resets (public_id, user_id, token_hash, expires_at)
		 SELECT UUID_TO_BIN(UUID()), id, ?, NOW(3) + INTERVAL 1 HOUR FROM users WHERE email = ?`,
		auth.HashResetToken(token), email)
	require.NoError(t, err)

	status, body := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/password-reset/confirm", "",
		map[string]any{"token": token, "newPassword": "a-new-long-password"})
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	require.True(t, strings.Contains(string(body), "AUTH.RESET_INVALID"), "%s", body)

	var usedAt sql.NullTime
	require.NoError(t, testDB.QueryRow(
		`SELECT used_at FROM password_resets WHERE token_hash = ?`, auth.HashResetToken(token)).Scan(&usedAt))
	require.False(t, usedAt.Valid, "the token is not consumed")

	meStatus, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", session, nil)
	require.Equal(t, http.StatusOK, meStatus, "the account's sessions survive")
}
