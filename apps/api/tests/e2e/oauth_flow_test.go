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
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func TestOAuthGoogleFlowWithVerifiedEmailLinksExistingAccount(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	email := fmt.Sprintf("oauth-link-%d@test.local", seq)
	subject := fmt.Sprintf("google-sub-%d", seq)
	var tokenRequests int
	var userinfoRequests int
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests++
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.NotEmpty(t, r.Form.Get("code_verifier"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			userinfoRequests++
			require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"sub":%q,"email":%q,"email_verified":true,"name":"OAuth Linked"}`, subject, email)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

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
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, app.URL+"/auth/register", "",
		map[string]any{"name": "Password User", "email": email, "password": "password123"},
		&reg)

	startResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/google/start?redirect=%2Fsettings", "")
	require.Equal(t, http.StatusFound, startResp.StatusCode)
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, provider.URL+"/authorize", startURL.Scheme+"://"+startURL.Host+startURL.Path)
	state := startURL.Query().Get("state")
	require.NotEmpty(t, state)
	require.NotEmpty(t, startURL.Query().Get("code_challenge"))
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)
	require.Equal(t, state, cookie.Value)

	callbackURL := app.URL + "/auth/oauth/google/callback?code=provider-code&state=" + url.QueryEscape(state)
	callbackResp := requestNoRedirect(t, http.MethodGet, callbackURL, cookie.String())
	require.Equal(t, http.StatusFound, callbackResp.StatusCode)
	defer callbackResp.Body.Close()
	require.Contains(t, callbackResp.Header.Get("Set-Cookie"), "Max-Age=0")
	location := callbackResp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, helpers.TestWebURL+"/oauth-complete?redirect=%2Fsettings#token="), location)
	token := strings.TrimPrefix(location, helpers.TestWebURL+"/oauth-complete?redirect=%2Fsettings#token=")
	token, err = url.QueryUnescape(token)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	var me struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	helpers.DoJSON(t, http.MethodGet, app.URL+"/user", token, nil, &me)
	require.Equal(t, reg.User.ID, me.ID)
	require.Equal(t, email, me.Email)
	require.Equal(t, 1, tokenRequests)
	require.Equal(t, 1, userinfoRequests)

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, callbackURL, token, nil)
	require.Equal(t, http.StatusBadRequest, status, "OAuth callback must require the state cookie, not a bearer token")
}

func TestOAuthGoogleFlowRejectsUnverifiedEmail(t *testing.T) {
	bootstrap(t)

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"google-sub-unverified","email":"blocked@test.local","email_verified":false,"name":"Blocked"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

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

	startResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/google/start", "")
	require.Equal(t, http.StatusFound, startResp.StatusCode)
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	state := startURL.Query().Get("state")
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)

	callbackResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/google/callback?code=provider-code&state="+url.QueryEscape(state), cookie.String())
	require.Equal(t, http.StatusFound, callbackResp.StatusCode)
	defer callbackResp.Body.Close()
	require.Equal(t, helpers.TestWebURL+"/login?error=oauth_not_allowed", callbackResp.Header.Get("Location"))
	require.NotContains(t, callbackResp.Header.Get("Location"), "#token=")
}

func newOAuthTestServer(t *testing.T, cfg users.OAuthConfig) *httptest.Server {
	t.Helper()
	deps := router.Deps{
		DB:                   testDB,
		Queries:              generated.New(testDB),
		JWTSecret:            helpers.TestJWTSecret,
		Mailer:               &helpers.CapturingMailer{},
		WebURL:               helpers.TestWebURL,
		OAuth:                cfg,
		PasswordLoginEnabled: true,
		AuthRateLimit:        -1,
	}
	srv := httptest.NewServer(router.Build(deps))
	t.Cleanup(srv.Close)
	return srv
}

func requestNoRedirect(t *testing.T, method, rawURL, cookie string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	require.NoError(t, err)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func firstCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
