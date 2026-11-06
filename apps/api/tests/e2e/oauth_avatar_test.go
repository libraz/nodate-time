package e2e

import (
	"database/sql"
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

// googleAvatarServer starts an app wired to a Google stub that reports picture.
func googleAvatarServer(t *testing.T, subject, email, picture string) *httptest.Server {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/userinfo":
			_, _ = fmt.Fprintf(w,
				`{"sub":%q,"email":%q,"email_verified":true,"name":"Avatar User","picture":%q}`,
				subject, email, picture)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	return newOAuthTestServer(t, users.OAuthConfig{
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
}

func storedAvatarURL(t *testing.T, email string) sql.NullString {
	t.Helper()
	var got sql.NullString
	require.NoError(t, testDB.QueryRow(`SELECT avatar_url FROM users WHERE email = ?`, email).Scan(&got))
	return got
}

func registerConfirmed(t *testing.T, appURL, email string) {
	t.Helper()
	helpers.DoJSON(t, http.MethodPost, appURL+"/auth/register", "",
		map[string]any{"name": "Owner", "email": email, "password": "password123"}, nil)
	confirmEmail(t, appURL, email)
}

func TestOAuthGoogleNewUserGetsProviderPicture(t *testing.T) {
	bootstrap(t)
	seq := time.Now().UnixNano()
	email := fmt.Sprintf("avatar-new-%d@test.local", seq)
	pic := "https://lh3.googleusercontent.com/a/new-" + fmt.Sprint(seq)
	app := googleAvatarServer(t, fmt.Sprintf("g-avatar-new-%d", seq), email, pic)

	require.Contains(t, runGoogleCallback(t, app.URL), "#token=")
	require.Equal(t, sql.NullString{String: pic, Valid: true}, storedAvatarURL(t, email))
}

func TestOAuthGoogleLinkSeedsAccountWithoutAvatar(t *testing.T) {
	bootstrap(t)
	seq := time.Now().UnixNano()
	email := fmt.Sprintf("avatar-link-%d@test.local", seq)
	pic := "https://lh3.googleusercontent.com/a/link-" + fmt.Sprint(seq)
	app := googleAvatarServer(t, fmt.Sprintf("g-avatar-link-%d", seq), email, pic)
	registerConfirmed(t, app.URL, email)
	require.False(t, storedAvatarURL(t, email).Valid)

	require.Contains(t, runGoogleCallback(t, app.URL), "#token=")
	require.Equal(t, sql.NullString{String: pic, Valid: true}, storedAvatarURL(t, email))
}

func TestOAuthGoogleLinkKeepsExistingAvatar(t *testing.T) {
	bootstrap(t)
	seq := time.Now().UnixNano()
	email := fmt.Sprintf("avatar-keep-%d@test.local", seq)
	old := "https://example.test/old-" + fmt.Sprint(seq)
	app := googleAvatarServer(t, fmt.Sprintf("g-avatar-keep-%d", seq), email,
		"https://lh3.googleusercontent.com/a/keep")
	registerConfirmed(t, app.URL, email)
	_, err := testDB.Exec(`UPDATE users SET avatar_url = ? WHERE email = ?`, old, email)
	require.NoError(t, err)

	require.Contains(t, runGoogleCallback(t, app.URL), "#token=")
	require.Equal(t, sql.NullString{String: old, Valid: true}, storedAvatarURL(t, email))
}

// A returning sign-in is not a first attach, so an avatar the user removed
// must stay removed.
func TestOAuthGoogleReturningSignInDoesNotReseed(t *testing.T) {
	bootstrap(t)
	seq := time.Now().UnixNano()
	email := fmt.Sprintf("avatar-return-%d@test.local", seq)
	app := googleAvatarServer(t, fmt.Sprintf("g-avatar-return-%d", seq), email,
		"https://lh3.googleusercontent.com/a/return")

	require.Contains(t, runGoogleCallback(t, app.URL), "#token=")
	require.True(t, storedAvatarURL(t, email).Valid)
	_, err := testDB.Exec(`UPDATE users SET avatar_url = NULL WHERE email = ?`, email)
	require.NoError(t, err)

	require.Contains(t, runGoogleCallback(t, app.URL), "#token=")
	require.False(t, storedAvatarURL(t, email).Valid)
}

func TestOAuthGoogleIgnoresUnusablePicture(t *testing.T) {
	bootstrap(t)
	long := "https://lh3.googleusercontent.com/"
	for i := 0; i < 2100; i++ {
		long += "a"
	}
	for name, pic := range map[string]string{
		"http":    "http://lh3.googleusercontent.com/a/x",
		"data":    "data:image/png;base64,AAAA",
		"nohost":  "https:///a/x",
		"relpath": "/a/x",
		"toolong": long,
	} {
		t.Run(name, func(t *testing.T) {
			seq := time.Now().UnixNano()
			email := fmt.Sprintf("avatar-bad-%s-%d@test.local", name, seq)
			app := googleAvatarServer(t, fmt.Sprintf("g-avatar-bad-%s-%d", name, seq), email, pic)

			require.Contains(t, runGoogleCallback(t, app.URL), "#token=", "sign-in must still succeed")
			require.False(t, storedAvatarURL(t, email).Valid)
		})
	}
}

func TestOAuthLineNewUserGetsProviderPicture(t *testing.T) {
	bootstrap(t)
	seq := time.Now().UnixNano()
	subject := fmt.Sprintf("line-avatar-%d", seq)
	pic := "https://profile.line-scdn.net/abc" + fmt.Sprint(seq)

	var nonce string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"access-token","id_token":%q}`,
				unsignedIDToken(t, map[string]any{"nonce": nonce}))
		case "/userinfo":
			_, _ = fmt.Fprintf(w, `{"sub":%q,"name":"LINE Avatar","picture":%q}`, subject, pic)
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
			Scopes:       "openid profile",
		},
	})

	startResp := requestNoRedirect(t, http.MethodGet, app.URL+"/auth/oauth/line/start", "")
	defer startResp.Body.Close()
	startURL, err := url.Parse(startResp.Header.Get("Location"))
	require.NoError(t, err)
	nonce = startURL.Query().Get("nonce")
	cookie := firstCookie(startResp, "oauth_state")
	require.NotNil(t, cookie)
	cb := requestNoRedirect(t, http.MethodGet,
		app.URL+"/auth/oauth/line/callback?code=c&state="+url.QueryEscape(startURL.Query().Get("state")),
		cookie.String())
	defer cb.Body.Close()
	require.Contains(t, cb.Header.Get("Location"), "#token=")

	var got sql.NullString
	require.NoError(t, testDB.QueryRow(
		`SELECT u.avatar_url FROM users u JOIN identities i ON i.user_id = u.id WHERE i.subject = ?`,
		subject).Scan(&got))
	require.Equal(t, sql.NullString{String: pic, Valid: true}, got)
}
