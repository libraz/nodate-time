package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type appPasswordItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"lastUsedAt"`
	CreatedAt  string  `json:"createdAt"`
}

type createdAppPassword struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  string `json:"createdAt"`
	Password   string `json:"password"`
	ServerHost string `json:"serverHost"`
	Username   string `json:"username"`
}

func appPasswordsURL() string { return testServerURL + "/user/app-passwords" }

func verifiedTenant(t *testing.T) *helpers.TestTenant {
	t.Helper()
	tt := helpers.NewTenant(t, testServerURL)
	helpers.VerifyEmail(t, testDB, tt.UserID)
	return tt
}

func createAppPassword(t *testing.T, tt *helpers.TestTenant, name string) createdAppPassword {
	t.Helper()
	var out createdAppPassword
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, appPasswordsURL(), tt.AccessToken,
		map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func listAppPasswords(t *testing.T, tt *helpers.TestTenant) []appPasswordItem {
	t.Helper()
	var out []appPasswordItem
	helpers.DoJSON(t, http.MethodGet, appPasswordsURL(), tt.AccessToken, nil, &out)
	return out
}

func errorCodeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(raw, &e))
	return e.Code
}

func TestAppPasswordCreateShowsThePlaintextOnce(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	created := createAppPassword(t, tt, "iPhone")

	require.True(t, strings.HasPrefix(created.Password, apppassword.Prefix))
	require.Equal(t, created.Password[:apppassword.PrefixLen], created.Prefix)
	require.Equal(t, "iPhone", created.Name)
	require.Equal(t, tt.Email, created.Username)
	require.NotEmpty(t, created.CreatedAt)
	require.NotEmpty(t, created.ID)

	list := listAppPasswords(t, tt)
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)
	require.Equal(t, created.Prefix, list[0].Prefix)
	require.Nil(t, list[0].LastUsedAt, "a password nobody has used has no last use")

	_, raw := helpers.DoJSONStatus(t, http.MethodGet, appPasswordsURL(), tt.AccessToken, nil)
	require.NotContains(t, string(raw), created.Password, "the list never carries the secret")

	var storedHash string
	require.NoError(t, testDB.QueryRow(
		`SELECT p.token_hash FROM dav_app_passwords p JOIN users u ON u.id = p.user_id
		 WHERE u.email = ?`, tt.Email).Scan(&storedHash))
	require.Equal(t, apppassword.Hash(created.Password), storedHash)
	require.NotEqual(t, created.Password, storedHash)
}

func TestAppPasswordNamesAreTrimmedAndBounded(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	require.Equal(t, "Mac", createAppPassword(t, tt, "  Mac \t").Name)
	createAppPassword(t, tt, strings.Repeat("a", 100))

	for name, body := range map[string]map[string]any{
		"empty":           {"name": ""},
		"blank":           {"name": "   "},
		"over 100 chars":  {"name": strings.Repeat("a", 101)},
		"missing name":    {},
		"wrong type name": {"name": 7},
	} {
		status, _ := helpers.DoJSONStatus(t, http.MethodPost, appPasswordsURL(), tt.AccessToken, body)
		require.Equal(t, http.StatusUnprocessableEntity, status, name)
	}
	require.Len(t, listAppPasswords(t, tt), 2, "a refused request creates nothing")
}

func TestAppPasswordCapAtTwentyLive(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	var first createdAppPassword
	for i := 0; i < 20; i++ {
		c := createAppPassword(t, tt, fmt.Sprintf("device %d", i))
		if i == 0 {
			first = c
		}
	}

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, appPasswordsURL(), tt.AccessToken,
		map[string]any{"name": "one too many"})
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "APP_PASSWORD.LIMIT_REACHED", errorCodeOf(t, raw))
	require.Len(t, listAppPasswords(t, tt), 20)

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/"+first.ID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)
	createAppPassword(t, tt, "after a revoke")
	require.Len(t, listAppPasswords(t, tt), 20, "a revoked password frees a slot")
}

func TestAppPasswordRefusesAnUnverifiedLocalAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, appPasswordsURL(), tt.AccessToken,
		map[string]any{"name": "iPhone"})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "APP_PASSWORD.EMAIL_UNVERIFIED", errorCodeOf(t, raw))
	require.Empty(t, listAppPasswords(t, tt))

	helpers.VerifyEmail(t, testDB, tt.UserID)
	createAppPassword(t, tt, "iPhone")
}

func TestAppPasswordAllowsAnAccountWithoutALocalIdentity(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	user, err := generated.New(testDB).GetUserByEmail(context.Background(), tt.Email)
	require.NoError(t, err)
	require.NoError(t, generated.New(testDB).DisableIdentity(context.Background(),
		generated.DisableIdentityParams{UserID: user.ID, Provider: "local"}))

	createAppPassword(t, tt, "iPhone")
}

func TestAppPasswordRefusesAnAccountWithMFA(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	_, err := testDB.Exec(
		`UPDATE identities i JOIN users u ON u.id = i.user_id
		    SET i.mfa_confirmed_at = NOW(3)
		  WHERE u.email = ? AND i.provider = 'local'`, tt.Email)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, appPasswordsURL(), tt.AccessToken,
		map[string]any{"name": "iPhone"})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "APP_PASSWORD.MFA_ENABLED", errorCodeOf(t, raw))
}

func TestAppPasswordRevoke(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := verifiedTenant(t)
	keep := createAppPassword(t, tt, "keep")
	drop := createAppPassword(t, tt, "drop")

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/"+drop.ID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	list := listAppPasswords(t, tt)
	require.Len(t, list, 1, "a revoked password is not listed")
	require.Equal(t, keep.ID, list[0].ID)

	var revoked int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM dav_app_passwords WHERE token_hash = ? AND revoked_at IS NOT NULL`,
		apppassword.Hash(drop.Password)).Scan(&revoked))
	require.Equal(t, 1, revoked, "the row is kept so its hash cannot be reissued")

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/"+drop.ID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "revoking twice")
	require.Equal(t, "APP_PASSWORD.NOT_FOUND", errorCodeOf(t, raw))

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/not-a-uuid", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	status, _ = helpers.DoJSONStatus(t, http.MethodDelete,
		appPasswordsURL()+"/018f0000-0000-7000-8000-000000000000", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
}

func TestAppPasswordOfAnotherUserIsNotFound(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := verifiedTenant(t)
	other := verifiedTenant(t)
	mine := createAppPassword(t, owner, "mine")

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, appPasswordsURL()+"/"+mine.ID, other.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "APP_PASSWORD.NOT_FOUND", errorCodeOf(t, raw))

	require.Empty(t, listAppPasswords(t, other), "another user's passwords are not listed")
	require.Len(t, listAppPasswords(t, owner), 1, "the failed revoke changed nothing")
}

func TestAppPasswordRequiresSignIn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "", nil},
		{http.MethodPost, "", map[string]any{"name": "iPhone"}},
		{http.MethodDelete, "/018f0000-0000-7000-8000-000000000000", nil},
	} {
		status, _ := helpers.DoJSONStatus(t, c.method, appPasswordsURL()+c.path, "", c.body)
		require.Equal(t, http.StatusUnauthorized, status, c.method)
	}
}
