package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// fakeGoogle stands in for the provider: it answers the token exchange and
// returns the userinfo the caller asks it to.
func fakeGoogle(t *testing.T, subject, email string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"sub":%q,"email":%q,"email_verified":true,"name":"Limits"}`, subject, email)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func googleConfig(providerURL string) users.OAuthConfig {
	return users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		Google: users.OAuthProviderConfig{
			ClientID:     "google-client",
			ClientSecret: "google-secret",
			AuthURL:      providerURL + "/authorize",
			TokenURL:     providerURL + "/token",
			UserinfoURL:  providerURL + "/userinfo",
			Scopes:       "openid email profile",
		},
	}
}

// newRestrictedOAuthServer builds a deployment that restricts sign-in to a
// domain allow-list.
//
// The allow-list is what sends an address to the database: with no restriction
// configured, emailAllowedToSignIn answers yes without looking anything up, so
// the column that cannot hold the address is never reached. A restricted
// deployment is the one where the address is looked up, which is why these
// tests configure one rather than reusing the unrestricted server the other
// OAuth tests build.
func newRestrictedOAuthServer(t *testing.T, cfg users.OAuthConfig, allowedDomains []string) *httptest.Server {
	t.Helper()
	queries := generated.New(testDB)
	deps := router.Deps{
		DB:                   testDB,
		Queries:              queries,
		WorkspaceID:          helpers.TestWorkspace(queries).ID,
		JWTSecret:            helpers.TestJWTSecret,
		Mailer:               testMailer,
		WebURL:               helpers.TestWebURL,
		OAuth:                cfg,
		GoogleAllowedDomains: allowedDomains,
		PasswordLoginEnabled: true,
		AuthRateLimit:        -1,
	}
	srv := httptest.NewServer(app.NewHandler(deps, app.Options{}))
	t.Cleanup(srv.Close)
	return srv
}

// startOAuth begins a sign-in and returns the state and its cookie.
func startOAuth(t *testing.T, baseURL, redirect string) (state string, cookie *http.Cookie) {
	t.Helper()
	resp := requestNoRedirect(t, http.MethodGet,
		baseURL+"/auth/oauth/google/start?redirect="+url.QueryEscape(redirect), "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode,
		"beginning a sign-in must not fail on a value the caller supplied")
	startURL, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	state = startURL.Query().Get("state")
	require.NotEmpty(t, state)
	cookie = firstCookie(resp, "oauth_state")
	require.NotNil(t, cookie)
	return state, cookie
}

// TestOAuthStartAcceptsARedirectTooLongToStore covers a value the caller
// controls answering with a server error.
//
// The return path is stored in signin_states.redirect_to, a VARCHAR(512). A
// longer one made the insert fail and the whole sign-in came back 500 -- a
// query parameter turning into a server error, on the endpoint whose entire
// job is to begin signing in.
//
// The return path is a convenience. Every other unusable shape of it already
// degrades to "/" rather than refusing sign-in, and refusing here would be
// worse than elsewhere: the browser is following a redirect, so a JSON error
// body would render in the address bar the user is watching.
func TestOAuthStartAcceptsARedirectTooLongToStore(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("oauth-longredirect-%d@test.local", seq)
	provider := fakeGoogle(t, fmt.Sprintf("google-sub-%d", seq), email)
	srv := newOAuthTestServer(t, googleConfig(provider.URL))

	// One character past what the column holds.
	tooLong := "/" + strings.Repeat("a", 512)
	state, cookie := startOAuth(t, srv.URL, tooLong)

	callback := srv.URL + "/auth/oauth/google/callback?code=provider-code&state=" + url.QueryEscape(state)
	resp := requestNoRedirect(t, http.MethodGet, callback, cookie.String())
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)

	location := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/oauth-complete?redirect=%2F#token="),
		"an unusable return path lands on the root, the way every other rejected one does: %s", location)
	token, _ := tokensFromRedirect(t, location)
	require.NotEmpty(t, token, "and the sign-in itself still completes")
}

// TestOAuthRefusesAnAddressTheColumnCannotHold covers the second value a
// caller controls that answered with a server error.
//
// users.email and oauth_allowed_emails.email are latin1 and documented as
// ASCII only. An address outside that reached the allow-list lookup, MySQL
// refused the comparison, and the handler turned that into a 500 -- mid
// redirect, so the browser rendered a JSON error body.
//
// Refusing is right here where degrading was right for the return path: the
// address is the identity being signed in, not a convenience, and there is no
// safe value to substitute for it.
func TestOAuthRefusesAnAddressTheColumnCannotHold(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	// A non-ASCII local part on a domain outside the allow-list, so the
	// address is looked up rather than waved through by the domain rule.
	email := fmt.Sprintf("%s-%d@example.com", "田中", seq)
	provider := fakeGoogle(t, fmt.Sprintf("google-sub-%d", seq), email)
	srv := newRestrictedOAuthServer(t, googleConfig(provider.URL), []string{"allowed.test"})

	state, cookie := startOAuth(t, srv.URL, "/")

	callback := srv.URL + "/auth/oauth/google/callback?code=provider-code&state=" + url.QueryEscape(state)
	resp := requestNoRedirect(t, http.MethodGet, callback, cookie.String())
	defer resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode,
		"an address this deployment cannot store is the caller's input, not a server fault")
	location := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/login?error="),
		"the browser is mid-redirect, so the answer belongs on the login page: %s", location)
	require.Contains(t, location, "oauth_email_unsupported",
		"and it should say the address is the problem, not that sign-in failed for unknown reasons")
}

// TestOAuthStillAdmitsAnOrdinaryAddressOnARestrictedDeployment is the other
// half: refusing what the column cannot hold must not refuse what it can. The
// address here is outside the allowed domain and is looked up in exactly the
// same way, so it exercises the path the guard was added to.
func TestOAuthStillAdmitsAnOrdinaryAddressOnARestrictedDeployment(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("oauth-allowlisted-%d@example.com", seq)
	provider := fakeGoogle(t, fmt.Sprintf("google-sub-%d", seq), email)
	srv := newRestrictedOAuthServer(t, googleConfig(provider.URL), []string{"allowed.test"})

	// Allow-listed individually, which is the admin panel's answer for an
	// address outside the permitted domains.
	_, err := testDB.Exec(
		`INSERT INTO oauth_allowed_emails (public_id, email) VALUES (UUID_TO_BIN(UUID()), ?)`, email)
	require.NoError(t, err)

	state, cookie := startOAuth(t, srv.URL, "/")

	callback := srv.URL + "/auth/oauth/google/callback?code=provider-code&state=" + url.QueryEscape(state)
	resp := requestNoRedirect(t, http.MethodGet, callback, cookie.String())
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)

	location := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/oauth-complete?redirect="),
		"an allow-listed ASCII address still signs in: %s", location)
}
