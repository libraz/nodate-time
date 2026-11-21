package e2e

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

const adminListCap = 500

func addInstanceAdmin(t *testing.T, base, token, email string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost, base+"/admin/instance-admins", token,
		map[string]any{"email": email})
}

func TestAddInstanceAdminGrantsTheRights(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)
	target := helpers.NewTenant(t, testServerURL)

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/admin/instance-admins", target.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status)

	status, raw := addInstanceAdmin(t, testServerURL, admin.AccessToken, strings.ToUpper(target.Email))
	require.Equal(t, http.StatusCreated, status, string(raw))
	var granted instanceAdminRow
	require.NoError(t, json.Unmarshal(raw, &granted))
	require.Equal(t, target.UserID, granted.UserID)
	require.Equal(t, target.Email, granted.Email)
	require.False(t, granted.GrantedAt.IsZero())

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/admin/instance-admins", target.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "the grant takes effect at once")
	var seen bool
	for _, row := range listInstanceAdmins(t, admin.AccessToken) {
		seen = seen || row.UserID == target.UserID
	}
	require.True(t, seen, "the new administrator appears in the listing")
}

func TestAddInstanceAdminRefusals(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)
	outsider := helpers.NewTenant(t, testServerURL)
	target := helpers.NewTenant(t, testServerURL)

	status, raw := addInstanceAdmin(t, testServerURL, outsider.AccessToken, target.Email)
	require.Equal(t, http.StatusForbidden, status, string(raw))

	status, raw = addInstanceAdmin(t, testServerURL, admin.AccessToken, "nobody-"+target.Email)
	require.Equal(t, http.StatusNotFound, status, string(raw))
	require.Equal(t, "ADMIN.USER_NOT_FOUND", adCode(t, raw))

	status, raw = addInstanceAdmin(t, testServerURL, admin.AccessToken, target.Email)
	require.Equal(t, http.StatusCreated, status, string(raw))
	status, raw = addInstanceAdmin(t, testServerURL, admin.AccessToken, target.Email)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "ADMIN.ALREADY_ADMIN", adCode(t, raw))
}

// fillTo adds placeholder rows inside tx until count reaches want. The rows
// roll back with tx, so nothing outside the test sees a full list.
func fillTo(t *testing.T, tx *sql.Tx, countSQL string, want int, add func(i int) error) {
	t.Helper()
	var have int
	require.NoError(t, tx.QueryRow(countSQL).Scan(&have))
	require.LessOrEqual(t, have, want, "the list is already past its cap")
	for i := 0; have < want; i, have = i+1, have+1 {
		require.NoError(t, add(i))
	}
}

func TestInstanceAdminListHasACap(t *testing.T) {
	bootstrap(t)

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)

	tx, err := testDB.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	srv := newTxAdminServer(t, tx, users.OAuthConfig{})

	fillTo(t, tx, `SELECT COUNT(*) FROM instance_admins WHERE revoked_at IS NULL AND enabled = TRUE`,
		adminListCap-1, func(i int) error {
			res, err := tx.Exec(`INSERT INTO users (public_id, email, display_name)
				VALUES (UUID_TO_BIN(UUID()), ?, 'filler')`, fmt.Sprintf("admin-fill-%d-%s", i, first.Email))
			if err != nil {
				return err
			}
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			_, err = tx.Exec(`INSERT INTO instance_admins (public_id, user_id) VALUES (UUID_TO_BIN(UUID()), ?)`, id)
			return err
		})

	status, raw := addInstanceAdmin(t, srv.URL, admin.AccessToken, first.Email)
	require.Equal(t, http.StatusCreated, status, "the last free place is granted: %s", raw)
	status, raw = addInstanceAdmin(t, srv.URL, admin.AccessToken, second.Email)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "ADMIN.ADMINS_LIMIT_REACHED", adCode(t, raw))

	// A full list still reports a user who already holds the rights as such.
	status, raw = addInstanceAdmin(t, srv.URL, admin.AccessToken, first.Email)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "ADMIN.ALREADY_ADMIN", adCode(t, raw))
}

func TestAllowedEmailListHasACap(t *testing.T) {
	bootstrap(t)

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	tx, err := testDB.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	srv := newTxAdminServer(t, tx, users.OAuthConfig{})

	fillTo(t, tx, `SELECT COUNT(*) FROM oauth_signin_allowlist WHERE entry_kind = 'email' AND enabled = TRUE`,
		adminListCap-1, func(i int) error {
			_, err := tx.Exec(`INSERT INTO oauth_signin_allowlist (public_id, entry_kind, entry_value)
				VALUES (UUID_TO_BIN(UUID()), 'email', ?)`, fmt.Sprintf("allow-fill-%d@fill.test", i))
			return err
		})

	create := func(email string) (int, []byte) {
		return helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/admin/allowed-emails", admin.AccessToken,
			map[string]any{"email": email})
	}
	status, raw := create("last-place@allow.test")
	require.Equal(t, http.StatusCreated, status, string(raw))
	status, raw = create("one-too-many@allow.test")
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "ADMIN.ALLOWED_EMAILS_LIMIT_REACHED", adCode(t, raw))
}

func TestOAuthProviderPatchKeepsWhatIsAbsent(t *testing.T) {
	bootstrap(t)

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	tx, err := testDB.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec(`DELETE FROM oauth_provider_configs WHERE provider IN ('google', 'line')`)
	require.NoError(t, err)
	srv := newTxAdminServer(t, tx, users.OAuthConfig{})
	url := srv.URL + "/admin/oauth-providers/line"

	patchProvider := func(body map[string]any) (int, []byte) {
		return helpers.DoJSONStatus(t, http.MethodPatch, url, admin.AccessToken, body)
	}
	var out struct {
		ClientID  string `json:"clientId"`
		HasSecret bool   `json:"hasSecret"`
		Enabled   bool   `json:"enabled"`
	}

	status, raw := patchProvider(map[string]any{"clientId": "line-client", "clientSecret": "line-secret", "enabled": true})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &out))
	require.True(t, out.HasSecret)
	stored := readProviderRow(t, tx, "line")

	status, raw = patchProvider(map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "line-client", out.ClientID, "an absent member keeps the stored value")
	require.True(t, out.HasSecret)
	require.False(t, out.Enabled)
	require.Equal(t, stored.ciphertext, readProviderRow(t, tx, "line").ciphertext, "the secret is untouched")

	status, raw = patchProvider(map[string]any{"clientSecret": nil})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &out))
	require.False(t, out.HasSecret, "null removes the stored secret")
	require.Equal(t, "line-client", out.ClientID)

	for name, body := range map[string]map[string]any{
		"null client id": {"clientId": nil},
		"null enabled":   {"enabled": nil},
		"empty secret":   {"clientSecret": ""},
	} {
		status, raw = patchProvider(body)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		require.Equal(t, "REQUEST.VALIDATION", adCode(t, raw), name)
	}

	status, _ = helpers.DoJSONStatus(t, http.MethodPut, url, admin.AccessToken, map[string]any{"enabled": true})
	require.Equal(t, http.StatusMethodNotAllowed, status)
}
