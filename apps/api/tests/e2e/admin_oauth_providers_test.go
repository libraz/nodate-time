package e2e

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// newTxAdminServer serves the routes with every query bound to tx. Provider
// configuration is one row per provider for the whole database, so a test that
// writes it committed would change what every concurrent OAuth test signs in
// against; inside a transaction that is rolled back, nobody else sees it.
func newTxAdminServer(t *testing.T, tx *sql.Tx, oauth users.OAuthConfig) *httptest.Server {
	t.Helper()
	cipher, err := secrets.New(strings.Repeat("ab", 32))
	require.NoError(t, err)
	queries := generated.New(tx)
	deps := router.Deps{
		DB:            testDB,
		Queries:       queries,
		WorkspaceID:   helpers.TestWorkspace(generated.New(testDB)).ID,
		JWTSecret:     helpers.TestJWTSecret,
		Mailer:        testMailer,
		WebURL:        helpers.TestWebURL,
		OAuth:         oauth,
		Cipher:        cipher,
		AuthRateLimit: -1,
	}
	srv := httptest.NewServer(app.NewHandler(deps, app.Options{}))
	t.Cleanup(srv.Close)
	return srv
}

type providerRow struct {
	publicID   []byte
	ciphertext []byte
}

func readProviderRow(t *testing.T, tx *sql.Tx, provider string) providerRow {
	t.Helper()
	var r providerRow
	require.NoError(t, tx.QueryRow(
		`SELECT public_id, client_secret_ciphertext FROM oauth_provider_configs WHERE provider = ?`,
		provider).Scan(&r.publicID, &r.ciphertext))
	return r
}

func putProvider(t *testing.T, base, token, provider string, body map[string]any) (hasSecret bool) {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodPut, base+"/admin/oauth-providers/"+provider, token, body)
	require.Equal(t, http.StatusOK, status, "PUT %s %v: %s", provider, body, raw)
	var out struct {
		HasSecret bool `json:"hasSecret"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out.HasSecret
}

// Every way the admin screen saves a provider -- first save relying on the
// environment's secret, first save with a secret, keeping the stored secret,
// replacing it, clearing it -- is accepted and leaves a row with a stable
// public id and a non-NULL ciphertext.
func TestAdminOAuthProviderSaveCycle(t *testing.T) {
	bootstrap(t)

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	tx, err := testDB.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec(`DELETE FROM oauth_provider_configs WHERE provider IN ('google', 'line')`)
	require.NoError(t, err)

	srv := newTxAdminServer(t, tx, users.OAuthConfig{
		Google: users.OAuthProviderConfig{ClientID: "env-google-client", ClientSecret: "env-google-secret"},
	})

	// Google has an environment secret, so a first save may omit one.
	require.False(t, putProvider(t, srv.URL, admin.AccessToken, "google",
		map[string]any{"clientId": "db-google-client", "clientSecret": "", "enabled": true}))
	first := readProviderRow(t, tx, "google")
	require.Len(t, first.publicID, 16)
	require.NotNil(t, first.ciphertext)
	require.Empty(t, first.ciphertext)

	require.True(t, putProvider(t, srv.URL, admin.AccessToken, "google",
		map[string]any{"clientId": "db-google-client", "clientSecret": "replaced", "enabled": true}))
	require.True(t, putProvider(t, srv.URL, admin.AccessToken, "google",
		map[string]any{"clientId": "db-google-client-2", "clientSecret": "", "enabled": false}),
		"an empty secret keeps the stored one")
	kept := readProviderRow(t, tx, "google")
	require.Equal(t, first.publicID, kept.publicID, "an update must not change the public id")
	require.NotEmpty(t, kept.ciphertext)

	require.False(t, putProvider(t, srv.URL, admin.AccessToken, "google",
		map[string]any{"clientId": "db-google-client-2", "clientSecret": "__clear__", "enabled": true}))
	cleared := readProviderRow(t, tx, "google")
	require.Equal(t, first.publicID, cleared.publicID)
	require.NotNil(t, cleared.ciphertext)
	require.Empty(t, cleared.ciphertext)

	// LINE has no environment secret, so its first save carries one.
	require.True(t, putProvider(t, srv.URL, admin.AccessToken, "line",
		map[string]any{"clientId": "db-line-client", "clientSecret": "line-secret", "enabled": true}))
	require.Len(t, readProviderRow(t, tx, "line").publicID, 16)
}
