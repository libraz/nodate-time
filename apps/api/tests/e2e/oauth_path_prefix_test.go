package e2e

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
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

// The shipped deployment publishes the API under /api and strips the prefix
// before the API sees it. A browser holding the state cookie the start step set
// has to present it at the callback URL it is sent back to, prefix included.
func TestOAuthCompletesBehindAPathPrefix(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	seq := time.Now().UnixNano()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"sub":"prefix-sub-%d","email":"prefix-%d@test.local","email_verified":true,"name":"Prefixed"}`, seq, seq)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	// The proxy in front: everything under /api reaches the API without it.
	var api http.Handler
	proxy := httptest.NewServer(http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.ServeHTTP(w, r)
	})))
	defer proxy.Close()

	queries := generated.New(testDB)
	api = app.NewHandler(router.Deps{
		DB:          testDB,
		Queries:     queries,
		WorkspaceID: helpers.TestWorkspace(queries).ID,
		JWTSecret:   helpers.TestJWTSecret,
		Mailer:      testMailer,
		WebURL:      helpers.TestWebURL,
		OAuth: users.OAuthConfig{
			RedirectBase: proxy.URL + "/api",
			Google: users.OAuthProviderConfig{
				ClientID:    "google-client",
				AuthURL:     provider.URL + "/authorize",
				TokenURL:    provider.URL + "/token",
				UserinfoURL: provider.URL + "/userinfo",
				Scopes:      "openid email profile",
			},
		},
		PasswordLoginEnabled: true,
		AuthRateLimit:        -1,
	}, app.Options{})

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	start, err := browser.Get(proxy.URL + "/api/auth/oauth/google/start")
	require.NoError(t, err)
	start.Body.Close()
	require.Equal(t, http.StatusFound, start.StatusCode)
	authorize, err := url.Parse(start.Header.Get("Location"))
	require.NoError(t, err)
	callback := authorize.Query().Get("redirect_uri")
	require.Equal(t, proxy.URL+"/api/auth/oauth/google/callback", callback)

	done, err := browser.Get(callback + "?code=provider-code&state=" + url.QueryEscape(authorize.Query().Get("state")))
	require.NoError(t, err)
	done.Body.Close()
	require.Equal(t, http.StatusFound, done.StatusCode)
	location := done.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/oauth-complete"),
		"the callback should complete the sign-in, got %s", location)
	require.Contains(t, location, "#token=")

	// The clearing cookie has to land on the same path, or the spent state
	// stays in the browser.
	cb, err := url.Parse(callback)
	require.NoError(t, err)
	for _, c := range jar.Cookies(cb) {
		require.NotEqual(t, "oauth_state", c.Name, "the state cookie should be cleared after the callback")
	}
}
