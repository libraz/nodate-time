package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// googleProviderStub answers the token and userinfo calls for one subject.
func googleProviderStub(t *testing.T, subject, email string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"sub":%q,"email":%q,"email_verified":true,"name":"Provider User"}`, subject, email)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runGoogleCallback drives a full start/callback pair and returns the Location
// the callback redirected to.
func runGoogleCallback(t *testing.T, appURL string) string {
	t.Helper()
	startResp := requestNoRedirect(t, http.MethodGet, appURL+"/auth/oauth/google/start", "")
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	require.NotEmpty(t, state)
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)

	cb := requestNoRedirect(t, http.MethodGet,
		appURL+"/auth/oauth/google/callback?code=provider-code&state="+url.QueryEscape(state),
		cookie.String())
	defer cb.Body.Close()
	require.Equal(t, http.StatusFound, cb.StatusCode)
	return cb.Header.Get("Location")
}

// The takeover this refuses: register a victim's address first, wait for them
// to sign in with a provider, and inherit their account — password included —
// because the provider vouched for an address the local account never proved
// it owns.
func TestProviderSignInRefusesToLinkToAnUnconfirmedAccount(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	victimEmail := fmt.Sprintf("victim-%d@test.local", seq)
	subject := fmt.Sprintf("google-sub-%d", seq)

	provider := googleProviderStub(t, subject, victimEmail)
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

	// The attacker claims the address with a password only they know, and
	// never confirms it — they cannot, the mailbox is not theirs.
	var attacker struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, app.URL+"/auth/register", "",
		map[string]any{"name": "Squatter", "email": victimEmail, "password": "attacker-password"},
		&attacker)

	location := runGoogleCallback(t, app.URL)
	require.Equal(t, helpers.TestWebURL+"/login?error=oauth_email_unverified", location,
		"provider sign-in must not adopt an account that never proved it owns the address")
	require.NotContains(t, location, "#token=")

	// No provider identity was attached to the squatted account.
	var identities int
	require.NoError(t, testDB.QueryRow(`
		SELECT COUNT(*) FROM identities i
		JOIN users u ON u.id = i.user_id
		WHERE u.email = ? AND i.provider = 'google'`, victimEmail).Scan(&identities))
	require.Zero(t, identities)
}

// Once the address is confirmed, the same sign-in links to the same account
// rather than creating a second one.
func TestProviderSignInLinksToAConfirmedAccount(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("owner-%d@test.local", seq)
	subject := fmt.Sprintf("google-sub-owner-%d", seq)

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

	var reg struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, app.URL+"/auth/register", "",
		map[string]any{"name": "Owner", "email": email, "password": "password123"}, &reg)
	confirmEmail(t, app.URL, email)

	location := runGoogleCallback(t, app.URL)
	require.Contains(t, location, "#token=", location)

	token, _ := tokensFromRedirect(t, location)
	var me struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, app.URL+"/user", token, nil, &me)
	require.Equal(t, reg.User.ID, me.ID, "the provider sign-in must land on the same account")
}

// An account created by a provider is confirmed on the spot, so a second
// provider can link to it without a round trip through the mailbox.
func TestProviderCreatedAccountIsAlreadyConfirmed(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("fresh-%d@test.local", seq)
	subject := fmt.Sprintf("google-sub-fresh-%d", seq)

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
		EmailVerified bool `json:"emailVerified"`
	}
	helpers.DoJSON(t, http.MethodGet, app.URL+"/user", token, nil, &me)
	require.True(t, me.EmailVerified)
}
