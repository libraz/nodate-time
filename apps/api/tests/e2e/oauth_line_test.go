package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// unsignedIDToken builds a JWT-shaped token carrying the given claims. The
// callback reads the nonce and email claims out of the token it just received
// over TLS from the token endpoint without verifying the signature, so the
// header and signature segments only have to be present.
func unsignedIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString
	return strings.Join([]string{
		enc([]byte(`{"alg":"none","typ":"JWT"}`)),
		enc(payload),
		"",
	}, ".")
}

// LINE sign-in stores an identity whose provider is 'line'. The column is an
// ENUM, so a value the schema does not list is rejected by the database and
// takes the whole sign-in transaction down with it — after the browser has
// already been handed to the provider and back.
func TestOAuthLineFlowCreatesAnIdentity(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	subject := fmt.Sprintf("line-sub-%d", seq)
	email := fmt.Sprintf("line-user-%d@test.local", seq)

	var nonce string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, r.ParseForm())
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"access-token","id_token":%q}`,
				unsignedIDToken(t, map[string]any{"nonce": nonce, "email": email}))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			// LINE's userinfo never carries an email; the id_token is the only source.
			_, _ = fmt.Fprintf(w, `{"sub":%q,"name":"LINE User"}`, subject)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	app := newOAuthTestServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		LINE: users.OAuthProviderConfig{
			ClientID:     "line-client",
			ClientSecret: "line-secret",
			AuthURL:      provider.URL + "/authorize",
			TokenURL:     provider.URL + "/token",
			UserinfoURL:  provider.URL + "/userinfo",
			Scopes:       "openid profile email",
		},
	})

	startResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/line/start", "")
	require.Equal(t, http.StatusFound, startResp.StatusCode)
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	require.NotEmpty(t, state)
	nonce = startURL.Query().Get("nonce")
	require.NotEmpty(t, nonce, "LINE is an OIDC provider and must get a nonce")
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)

	callbackResp := requestNoRedirect(t, http.MethodGet,
		app.URL+"/auth/oauth/line/callback?code=provider-code&state="+url.QueryEscape(state),
		cookie.String())
	require.Equal(t, http.StatusFound, callbackResp.StatusCode)
	defer callbackResp.Body.Close()

	location := callbackResp.Header.Get("Location")
	require.Contains(t, location, "#token=", "sign-in must complete, not bounce to /login: %s", location)

	var storedProvider string
	require.NoError(t, testDB.QueryRow(
		`SELECT provider FROM identities WHERE subject = ?`, subject,
	).Scan(&storedProvider))
	require.Equal(t, "line", storedProvider)
}

// A failure inside the sign-in transaction happens while the browser is being
// redirected, so it has to land on the login page like every other OAuth
// failure rather than rendering a raw JSON error body.
func TestOAuthFailureAfterTokenExchangeRedirectsToLogin(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	var nonce string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"access-token","id_token":%q}`,
				unsignedIDToken(t, map[string]any{"nonce": nonce}))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			// A subject longer than the column takes the insert down, standing in
			// for any storage failure past the point of no return.
			_, _ = fmt.Fprintf(w, `{"sub":%q,"name":"LINE User"}`,
				strings.Repeat("x", 300)+fmt.Sprint(seq))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	app := newOAuthTestServer(t, users.OAuthConfig{
		RedirectBase: "http://api.test.local",
		LINE: users.OAuthProviderConfig{
			ClientID:     "line-client",
			ClientSecret: "line-secret",
			AuthURL:      provider.URL + "/authorize",
			TokenURL:     provider.URL + "/token",
			UserinfoURL:  provider.URL + "/userinfo",
			Scopes:       "openid profile email",
		},
	})

	startResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/line/start", "")
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	nonce = startURL.Query().Get("nonce")
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)

	callbackResp := requestNoRedirect(t, http.MethodGet,
		app.URL+"/auth/oauth/line/callback?code=provider-code&state="+url.QueryEscape(state),
		cookie.String())
	defer callbackResp.Body.Close()

	require.Equal(t, http.StatusFound, callbackResp.StatusCode,
		"a storage failure must still redirect, not render an API error page")
	require.Equal(t, helpers.TestWebURL+"/login?error=oauth_failed",
		callbackResp.Header.Get("Location"))
}
