package e2e

import (
	"context"
	"database/sql"
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

// Every path that opens a session for an account answers the same admission
// question -- the domain restriction, or an individual allow-list entry -- on
// an address the user has proven they own.

// A restriction is enforced at every sign-in, not only at registration:
// withdrawing an address from the allow-list stops its password login, and
// the refusal reads exactly like a wrong password.
func TestPasswordLoginAppliesTheSignInRestriction(t *testing.T) {
	bootstrap(t)

	srv := newRestrictedOAuthServer(t, users.OAuthConfig{}, []string{"allowed.test"})
	email := fmt.Sprintf("admission-%d@outside.test", time.Now().UnixNano())
	const password = "correcthorsebattery"
	_, err := testDB.Exec(
		`INSERT INTO oauth_allowed_emails (public_id, email) VALUES (UUID_TO_BIN(UUID()), ?)`, email)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/register", "",
		map[string]any{"name": "Admission", "email": email, "password": password})
	require.Equal(t, http.StatusOK, status, "an allow-listed address registers: %s", string(raw))
	status, raw = helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	require.Equal(t, http.StatusOK, status, "and signs in while listed: %s", string(raw))

	_, err = testDB.Exec(`UPDATE oauth_allowed_emails SET enabled = FALSE WHERE email = ?`, email)
	require.NoError(t, err)

	status, refused := helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	require.Equal(t, http.StatusUnauthorized, status, "a withdrawn address must not sign in: %s", string(refused))
	_, wrong := helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/login", "",
		map[string]any{"email": email, "password": "not-the-password"})
	require.JSONEq(t, string(wrong), string(refused),
		"the refusal must say no more than a wrong password does")
}

// LINE's email claim is not proof the user owns the address, so on a
// restricted deployment it can satisfy neither the domain list nor the
// allow-list, even when the claimed address is inside the permitted domain.
func TestLINESignInIsRefusedUnderARestriction(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	subject := fmt.Sprintf("line-restricted-%d", seq)
	email := fmt.Sprintf("line-%d@allowed.test", seq)

	var nonce string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"access-token","id_token":%q}`,
				unsignedIDToken(t, map[string]any{"nonce": nonce, "email": email}))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"sub":%q,"name":"LINE User","email":%q}`, subject, email)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	srv := newRestrictedOAuthServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		LINE: users.OAuthProviderConfig{
			ClientID:     "line-client",
			ClientSecret: "line-secret",
			AuthURL:      provider.URL + "/authorize",
			TokenURL:     provider.URL + "/token",
			UserinfoURL:  provider.URL + "/userinfo",
			Scopes:       "openid profile email",
		},
	}, []string{"allowed.test"})

	startResp := requestNoRedirect(t, http.MethodGet, srv.URL+"/auth/oauth/line/start", "")
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	nonce = startURL.Query().Get("nonce")
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)

	resp := requestNoRedirect(t, http.MethodGet,
		srv.URL+"/auth/oauth/line/callback?code=provider-code&state="+url.QueryEscape(state), cookie.String())
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Equal(t, helpers.TestWebURL+"/login?error=oauth_not_allowed", resp.Header.Get("Location"))

	var identities int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM identities WHERE provider = 'line' AND subject = ?`, subject).Scan(&identities))
	require.Zero(t, identities, "a refused sign-in must create no account")
}

// The bootstrap administrator signs in with Google on a deployment whose
// domain restriction does not cover the administrator's own address: the
// account the CLI wrote is admitted, links to the Google identity, and keeps
// its rights.
func TestBootstrapAdminSignsInOutsideTheAllowedDomains(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("bootstrap-admin-%d@outside.test", seq)
	out, code := runCreateUser(t, "-email", email, "-password", "correcthorsebattery", "-admin")
	require.Zero(t, code, "output: %s", out)

	provider := fakeGoogle(t, fmt.Sprintf("google-admin-%d", seq), email)
	srv := newRestrictedOAuthServer(t, googleConfig(provider.URL), []string{"allowed.test"})
	state, cookie := startOAuth(t, srv.URL, "/")
	resp := requestNoRedirect(t, http.MethodGet,
		srv.URL+"/auth/oauth/google/callback?code=provider-code&state="+url.QueryEscape(state), cookie.String())
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/oauth-complete?redirect="),
		"the bootstrap admin must be admitted: %s", location)
	require.True(t, isInstanceAdmin(t, email))
}

// failingAllowListDB breaks the allow-list lookup and nothing else, so a
// sign-in reaches the point where it consults the list.
type failingAllowListDB struct{ generated.DBTX }

func (d failingAllowListDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if strings.Contains(q, "name: IsEmailAllowed") {
		q = "SELECT 1 FROM allow_list_lookup_failure WHERE ? IS NOT NULL"
	}
	return d.DBTX.QueryRowContext(ctx, q, args...)
}

// The browser is mid-redirect when the allow-list is consulted, so a failed
// lookup lands on the login page with the state cookie cleared, like every
// other callback failure, rather than rendering a JSON error body.
func TestOAuthAllowListFailureRedirectsToLogin(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	provider := fakeGoogle(t, fmt.Sprintf("google-fail-%d", seq), fmt.Sprintf("fail-%d@example.com", seq))
	deps := router.Deps{
		DB:                   testDB,
		Queries:              generated.New(failingAllowListDB{testDB}),
		WorkspaceID:          helpers.TestWorkspace(generated.New(testDB)).ID,
		JWTSecret:            helpers.TestJWTSecret,
		Mailer:               testMailer,
		WebURL:               helpers.TestWebURL,
		OAuth:                googleConfig(provider.URL),
		GoogleAllowedDomains: []string{"allowed.test"},
		PasswordLoginEnabled: true,
		AuthRateLimit:        -1,
	}
	srv := httptest.NewServer(app.NewHandler(deps, app.Options{}))
	t.Cleanup(srv.Close)

	state, cookie := startOAuth(t, srv.URL, "/")
	resp := requestNoRedirect(t, http.MethodGet,
		srv.URL+"/auth/oauth/google/callback?code=provider-code&state="+url.QueryEscape(state), cookie.String())
	defer resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Equal(t, helpers.TestWebURL+"/login?error=oauth_failed", resp.Header.Get("Location"))
	cleared := firstCookie(resp, "oauth_state")
	require.NotNil(t, cleared, "the state cookie must be cleared")
	require.Negative(t, cleared.MaxAge)
}
