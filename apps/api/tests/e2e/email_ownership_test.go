package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// registration is what /auth/register hands back.
type registration struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	User         struct {
		ID string `json:"id"`
	} `json:"user"`
}

func registerAt(t *testing.T, baseURL, email, password string) registration {
	t.Helper()
	var reg registration
	helpers.DoJSON(t, http.MethodPost, baseURL+"/auth/register", "",
		map[string]any{"name": "Registrant", "email": email, "password": password}, &reg)
	return reg
}

// requireCredentialsDead asserts nothing the registrant held still works:
// not the access token, not the refresh token, not the password.
func requireCredentialsDead(t *testing.T, baseURL, email, password string, reg registration) {
	t.Helper()
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, baseURL+"/user", reg.Token, nil)
	require.Equal(t, http.StatusUnauthorized, status, "the registrant's session must end")
	status, _ = helpers.DoJSONStatus(t, http.MethodPost, baseURL+"/auth/refresh", "",
		map[string]any{"refreshToken": reg.RefreshToken})
	require.Equal(t, http.StatusUnauthorized, status, "the registrant's refresh token must end")
	status, _ = helpers.DoJSONStatus(t, http.MethodPost, baseURL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	require.Equal(t, http.StatusUnauthorized, status, "the registrant's password must end")
}

// Someone registers an address they do not read; its owner then signs in with
// a provider that vouches for it. The provider's word does not hand over an
// account that holds a password nobody proved belongs to the address's owner
// (sql/core/PROTOCOL.md, Identity rule 2): the link is refused and the account
// is left exactly as the registrant set it up. The owner's way in is the
// confirmation link, below.
func TestEmailOwnershipProviderSignInCannotClaimASquattedAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("claimed-%d@test.local", seq)
	provider := googleProviderStub(t, fmt.Sprintf("google-sub-claim-%d", seq), email)
	app := newOAuthTestServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		Google: users.OAuthProviderConfig{
			ClientID:    "google-client",
			AuthURL:     provider.URL + "/authorize",
			TokenURL:    provider.URL + "/token",
			UserinfoURL: provider.URL + "/userinfo",
			Scopes:      "openid email profile",
		},
	})

	const password = "squatter-password"
	squatter := registerAt(t, app.URL, email, password)

	location := runGoogleCallback(t, app.URL)
	require.Equal(t, helpers.TestWebURL+"/login?error=AUTH.LINK_REFUSED", location)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	require.Equal(t, http.StatusOK, status, "the refusal leaves the squatter's password in place")
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, app.URL+"/user", squatter.Token, nil)
	require.Equal(t, http.StatusOK, status, "and the sessions it opened")
}

// The owner follows the confirmation link the registration sent them, from a
// browser that holds no session of the account. The registrant loses
// everything; the owner sets a password with a reset.
func TestEmailOwnershipLinkOpenedOutsideTheAccountEndsTheRegistrantsAccess(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("linked-%d@test.local", time.Now().UnixNano())
	const password = "squatter-password"
	squatter := registerAt(t, testServerURL, email, password)

	confirmEmail(t, testServerURL, email)

	requireCredentialsDead(t, testServerURL, email, password, squatter)

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
		map[string]any{"email": email}, nil)
	msg, ok := testMailer.LastFor(email)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(msg.Subject, "Reset"), "the owner can still set a password: %q", msg.Subject)
}

// The usual case: the person who registered confirms from their own session.
// Nothing ends.
func TestEmailOwnershipConfirmingInYourOwnSessionKeepsYouSignedIn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("owner-confirms-%d@test.local", time.Now().UnixNano())
	const password = "owner-password"
	reg := registerAt(t, testServerURL, email, password)

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", reg.Token,
		map[string]any{"token": verificationToken(t, email)}, nil)

	var me struct {
		EmailVerified bool `json:"emailVerified"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", reg.Token, nil, &me)
	require.True(t, me.EmailVerified)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	require.Equal(t, http.StatusOK, status, "the password still works")
}
