package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A deployment that signs in through a provider only has no use for a
// password on its first administrator, and a password is exactly what stops
// the provider from linking: an unconfirmed account holding one may have been
// registered by someone other than the address's owner. Without -password the
// account is created with nothing to sign in with, and the first provider
// sign-in whose verified email matches claims it, rights included.
func TestCreateUserWithoutAPasswordIsClaimedByTheFirstProviderSignIn(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("cu-provider-%d@test.local", seq)
	subject := fmt.Sprintf("google-sub-cu-provider-%d", seq)

	out, code := runCreateUser(t, "-email", email, "-name", "First Admin", "-admin")
	require.Zero(t, code, "output: %s", out)
	require.True(t, isInstanceAdmin(t, email))

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": "any-password-at-all"})
	require.Equal(t, http.StatusUnauthorized, status,
		"an account created without a password has no password to sign in with")

	provider := googleProviderStub(t, subject, email)
	app := newOAuthTestServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		Google: users.OAuthProviderConfig{
			ClientID:     "google-client",
			ClientSecret: "google-secret",
			AuthURL:      provider.URL + "/authorize",
			TokenURL:     provider.URL + "/token",
			UserinfoURL:  provider.URL + "/userinfo",
			Scopes:       "openid email profile",
		},
	})

	location := runGoogleCallback(t, app.URL)
	require.Contains(t, location, "#token=", location)

	token, _ := tokensFromRedirect(t, location)
	var me struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"emailVerified"`
	}
	helpers.DoJSON(t, http.MethodGet, app.URL+"/user", token, nil, &me)
	require.Equal(t, email, me.Email, "the provider sign-in must land on the created account")
	require.True(t, me.EmailVerified, "the provider's verified address confirms the account")
	require.True(t, isInstanceAdmin(t, email), "and the account keeps the rights it was created with")
}
