package e2e

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/apppasswords"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type profileLink struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

func profileLinksURL() string { return appPasswordsURL() + "/profile-links" }

// sessionOf is the public id of the session tt's access token names.
func sessionOf(t *testing.T, tt *helpers.TestTenant) string {
	t.Helper()
	claims, err := auth.ValidateToken(tt.AccessToken, helpers.TestJWTSecret)
	require.NoError(t, err)
	return claims.SessionID
}

// issueProfileLink asks for a link and returns the path it points at, which a
// test server with no public URL hands out on its own.
func issueProfileLink(t *testing.T, tt *helpers.TestTenant, name string) string {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, profileLinksURL(), tt.AccessToken,
		map[string]any{"name": name})
	require.Equal(t, http.StatusOK, status, string(raw))
	var link profileLink
	require.NoError(t, json.Unmarshal(raw, &link))
	expires, err := time.Parse(time.RFC3339, link.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(apppasswords.ProfileLinkTTL), expires, time.Minute)
	u, err := url.Parse(link.URL)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(u.Path, apppasswords.ProfilePath), link.URL)
	return u.Path
}

func ticketPath(ticket string) string { return apppasswords.ProfilePath + ticket }

func openProfile(t *testing.T, path string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := http.Get(testServerURL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, raw
}

func requireProfileRefused(t *testing.T, path, code string, status int) {
	t.Helper()
	got, _, raw := openProfile(t, path)
	require.Equal(t, status, got, string(raw))
	require.Equal(t, code, errorCodeOf(t, raw))
}

func requireProfileGone(t *testing.T, path string) {
	t.Helper()
	requireProfileRefused(t, path, "APP_PASSWORD.PROFILE_LINK_EXPIRED", http.StatusGone)
}

// plistString reads one string value of the profile.
func plistString(t *testing.T, body []byte, key string) string {
	t.Helper()
	m := regexp.MustCompile(`<key>` + key + `</key>\s*<string>([^<]*)</string>`).FindSubmatch(body)
	require.NotNil(t, m, "profile has no %s", key)
	return html.UnescapeString(string(m[1]))
}

func appPasswordRows(t *testing.T, tt *helpers.TestTenant) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM dav_app_passwords p JOIN users u ON u.id = p.user_id WHERE u.email = ?`,
		tt.Email).Scan(&n))
	return n
}

func propfindStatus(t *testing.T, tt *helpers.TestTenant, password string) int {
	t.Helper()
	status, _, _ := davRequest(t, "PROPFIND", "/dav/", tt.Email, password,
		map[string]string{"Depth": "0"}, davPropfindPrincipal)
	return status
}

func TestAppPasswordProfileServesTheAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	path := issueProfileLink(t, tt, "iPhone")
	require.Zero(t, appPasswordRows(t, tt), "issuing a link stores nothing")

	status, header, body := openProfile(t, path)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Equal(t, "application/x-apple-aspen-config", header.Get("Content-Type"))
	require.Equal(t, "no-store", header.Get("Cache-Control"))
	require.Contains(t, header.Get("Content-Disposition"), `.mobileconfig"`)
	require.Contains(t, string(body), "<string>com.apple.caldav.account</string>")

	password := plistString(t, body, "CalDAVPassword")
	require.Equal(t, tt.Email, plistString(t, body, "CalDAVUsername"))
	require.True(t, strings.HasSuffix(plistString(t, body, "CalDAVPrincipalURL"), "/dav/"))
	require.True(t, strings.HasPrefix(password, apppassword.Prefix))
	require.Equal(t, apppassword.Normalize(password), password)

	list := listAppPasswords(t, tt)
	require.Len(t, list, 1)
	require.Equal(t, "iPhone", list[0].Name)
	require.Equal(t, password[:apppassword.PrefixLen], list[0].Prefix)
	require.Regexp(t, `<key>PayloadUUID</key>\s*<string>`+list[0].ID+`</string>`, string(body),
		"the profile is the stored row")

	var hash, prefix string
	require.NoError(t, testDB.QueryRow(
		`SELECT p.token_hash, p.token_prefix FROM dav_app_passwords p JOIN users u ON u.id = p.user_id
		 WHERE u.email = ?`, tt.Email).Scan(&hash, &prefix))
	require.Equal(t, apppassword.Hash(password), hash, "only the hash is stored")
	require.NotContains(t, hash+prefix, password)
}

func TestAppPasswordProfilePasswordSignsIn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	status, _, body := openProfile(t, issueProfileLink(t, tt, "Mac"))
	require.Equal(t, http.StatusOK, status, string(body))
	require.Equal(t, http.StatusMultiStatus, propfindStatus(t, tt, plistString(t, body, "CalDAVPassword")))
}

func TestAppPasswordProfileRepeatsUntilFirstSync(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	path := issueProfileLink(t, tt, "iPhone")
	status, _, first := openProfile(t, path)
	require.Equal(t, http.StatusOK, status, string(first))
	status, _, second := openProfile(t, path)
	require.Equal(t, http.StatusOK, status, string(second))

	require.Equal(t, first, second, "the same profile with the same password")
	require.Equal(t, 1, appPasswordRows(t, tt))
}

func TestAppPasswordProfileGoneAfterSync(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	path := issueProfileLink(t, tt, "iPhone")
	status, _, body := openProfile(t, path)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Equal(t, http.StatusMultiStatus, propfindStatus(t, tt, plistString(t, body, "CalDAVPassword")))

	requireProfileGone(t, path)
	require.Equal(t, 1, appPasswordRows(t, tt))
}

func TestAppPasswordProfileRefusedLinks(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		for _, ticket := range []string{"abc.def", "abc", "a.b.c", strings.Repeat("a", 3000)} {
			requireProfileGone(t, ticketPath(ticket))
		}
	})

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		session := sessionOf(t, tt)
		requireProfileGone(t, ticketPath(helpers.MintProfileTicket(t, session, "old", time.Now().Add(-apppasswords.ProfileLinkTTL-time.Second))))
		require.Zero(t, appPasswordRows(t, tt))

		status, _, raw := openProfile(t, ticketPath(helpers.MintProfileTicket(t, session, "fresh", time.Now())))
		require.Equal(t, http.StatusOK, status, "the same ticket minted now opens: %s", raw)
	})

	t.Run("forged", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		session, err := uuid.Parse(sessionOf(t, tt))
		require.NoError(t, err)
		claims, err := apppasswords.NewProfileClaims(session, "forged")
		require.NoError(t, err)
		ticket, err := apppasswords.NewProfileKeys([]byte("not-the-server-secret")).Mint(claims, time.Now())
		require.NoError(t, err)
		requireProfileGone(t, ticketPath(ticket))
		require.Zero(t, appPasswordRows(t, tt))
	})

	t.Run("unknown session", func(t *testing.T) {
		t.Parallel()
		requireProfileGone(t, ticketPath(helpers.MintProfileTicket(t, uuid.Must(uuid.NewV7()).String(), "x", time.Now())))
	})

	t.Run("revoked row", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		path := issueProfileLink(t, tt, "iPhone")
		status, _, raw := openProfile(t, path)
		require.Equal(t, http.StatusOK, status, string(raw))
		list := listAppPasswords(t, tt)
		require.Len(t, list, 1)
		status, _ = helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/"+list[0].ID, tt.AccessToken, nil)
		require.Equal(t, http.StatusNoContent, status)

		requireProfileGone(t, path)
		require.Equal(t, 1, appPasswordRows(t, tt))
	})

	t.Run("after logout", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		path := issueProfileLink(t, tt, "iPhone")
		status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/logout", tt.AccessToken, nil)
		require.Equal(t, http.StatusNoContent, status)

		requireProfileGone(t, path)
		require.Zero(t, appPasswordRows(t, tt))
	})

	t.Run("after a password change", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		path := issueProfileLink(t, tt, "iPhone")
		helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/password", tt.AccessToken,
			map[string]any{"currentPassword": tt.Password, "newPassword": "new-password-123"}, nil)

		requireProfileGone(t, path)
		require.Zero(t, appPasswordRows(t, tt))
	})
}

func setMFA(t *testing.T, tt *helpers.TestTenant) {
	t.Helper()
	_, err := testDB.Exec(
		`UPDATE identities i JOIN users u ON u.id = i.user_id
		    SET i.mfa_confirmed_at = NOW(3)
		  WHERE u.email = ? AND i.provider = 'local'`, tt.Email)
	require.NoError(t, err)
}

func unverifyEmail(t *testing.T, tt *helpers.TestTenant) {
	t.Helper()
	_, err := testDB.Exec(`UPDATE users SET email_verified_at = NULL WHERE email = ?`, tt.Email)
	require.NoError(t, err)
}

func TestAppPasswordProfileChecksTheAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	t.Run("issuing", func(t *testing.T) {
		t.Parallel()
		unverified := helpers.NewTenant(t, testServerURL)
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, profileLinksURL(), unverified.AccessToken,
			map[string]any{"name": "iPhone"})
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, "APP_PASSWORD.EMAIL_UNVERIFIED", errorCodeOf(t, raw))

		mfa := verifiedTenant(t)
		setMFA(t, mfa)
		status, raw = helpers.DoJSONStatus(t, http.MethodPost, profileLinksURL(), mfa.AccessToken,
			map[string]any{"name": "iPhone"})
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, "APP_PASSWORD.MFA_ENABLED", errorCodeOf(t, raw))

		for name, body := range map[string]map[string]any{
			"blank":          {"name": "   "},
			"over 100 chars": {"name": strings.Repeat("a", 101)},
		} {
			status, _ := helpers.DoJSONStatus(t, http.MethodPost, profileLinksURL(), mfa.AccessToken, body)
			require.Equal(t, http.StatusUnprocessableEntity, status, name)
		}
	})

	t.Run("limit", func(t *testing.T) {
		t.Parallel()
		tt := verifiedTenant(t)
		for i := 0; i < apppasswords.MaxLivePasswords-1; i++ {
			helpers.CreateAppPassword(t, testDB, tt.UserID, "device")
		}
		path := issueProfileLink(t, tt, "the last slot")
		helpers.CreateAppPassword(t, testDB, tt.UserID, "taken first")

		requireProfileRefused(t, path, "APP_PASSWORD.LIMIT_REACHED", http.StatusConflict)
		require.Equal(t, apppasswords.MaxLivePasswords, appPasswordRows(t, tt))
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, profileLinksURL(), tt.AccessToken,
			map[string]any{"name": "one too many"})
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "APP_PASSWORD.LIMIT_REACHED", errorCodeOf(t, raw))
	})

	t.Run("first open", func(t *testing.T) {
		t.Parallel()
		mfa := verifiedTenant(t)
		path := issueProfileLink(t, mfa, "iPhone")
		setMFA(t, mfa)
		requireProfileRefused(t, path, "APP_PASSWORD.MFA_ENABLED", http.StatusForbidden)
		require.Zero(t, appPasswordRows(t, mfa))

		unverified := verifiedTenant(t)
		path = issueProfileLink(t, unverified, "iPhone")
		unverifyEmail(t, unverified)
		requireProfileRefused(t, path, "APP_PASSWORD.EMAIL_UNVERIFIED", http.StatusForbidden)
		require.Zero(t, appPasswordRows(t, unverified))
	})

	t.Run("second open", func(t *testing.T) {
		t.Parallel()
		mfa := verifiedTenant(t)
		path := issueProfileLink(t, mfa, "iPhone")
		status, _, raw := openProfile(t, path)
		require.Equal(t, http.StatusOK, status, string(raw))
		setMFA(t, mfa)
		requireProfileRefused(t, path, "APP_PASSWORD.MFA_ENABLED", http.StatusForbidden)
		require.Equal(t, 1, appPasswordRows(t, mfa))

		unverified := verifiedTenant(t)
		path = issueProfileLink(t, unverified, "iPhone")
		status, _, raw = openProfile(t, path)
		require.Equal(t, http.StatusOK, status, string(raw))
		unverifyEmail(t, unverified)
		requireProfileRefused(t, path, "APP_PASSWORD.EMAIL_UNVERIFIED", http.StatusForbidden)
		require.Equal(t, 1, appPasswordRows(t, unverified))
	})
}
