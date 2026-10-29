package e2e

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The identity rules of sql/core/PROTOCOL.md hold between applications that
// share users, identities and sessions. The other application is not running
// here, so each test plants the rows it would write -- a confirmed second
// factor, a removed membership, a session of another audience -- and checks
// that this one honours them.

// contractApp is a server wired to a Google stub answering for one address.
type contractApp struct {
	URL string
}

func newContractApp(t *testing.T, subject, email string) contractApp {
	t.Helper()
	provider := googleProviderStub(t, subject, email)
	srv := newOAuthTestServer(t, users.OAuthConfig{
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
	return contractApp{URL: srv.URL}
}

func contractEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@test.local", prefix, time.Now().UnixNano())
}

func (a contractApp) login(t *testing.T, email, password string) (int, string) {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, a.URL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
	return status, string(raw)
}

// oauthError is the error code a provider sign-in redirected to the login page
// with, or "" when it completed.
func oauthError(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	require.NoError(t, err)
	if !strings.HasSuffix(u.Path, "/login") {
		return ""
	}
	return u.Query().Get("error")
}

func userInternalID(t *testing.T, publicID string) uint32 {
	t.Helper()
	var id uint32
	require.NoError(t, testDB.QueryRow(
		`SELECT id FROM users WHERE public_id = UUID_TO_BIN(?)`, publicID).Scan(&id))
	return id
}

// confirmEmailFromOwnSession confirms the address from the registrant's own
// session, which keeps their password: a confirmation from outside the
// account hands it to the mailbox and clears the password with it.
func confirmEmailFromOwnSession(t *testing.T, baseURL, email, token string) {
	t.Helper()
	helpers.DoJSON(t, http.MethodPost, baseURL+"/auth/verify-email/confirm", token,
		map[string]any{"token": verificationToken(t, email)}, nil)
}

// plantConfirmedSecondFactor writes what enrolling TOTP in another application
// leaves on the local identity.
func plantConfirmedSecondFactor(t *testing.T, email string) {
	t.Helper()
	res, err := testDB.Exec(
		`UPDATE identities SET mfa_secret_ciphertext = RANDOM_BYTES(48), mfa_confirmed_at = NOW(3)
		 WHERE provider = 'local' AND subject = ?`, email)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func requireNoTokens(t *testing.T, raw []byte, what string) {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), what)
	require.NotContains(t, body, "token", what)
	require.NotContains(t, body, "refreshToken", what)
}

func requireAPIError(t *testing.T, status int, raw string, wantStatus int, wantCode string) {
	t.Helper()
	require.Equal(t, wantStatus, status, raw)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &body), raw)
	require.Equal(t, wantCode, body.Code)
}

// A confirmed second factor is one this application cannot ask for, so no
// path here may open a session for that account: not the password, not a
// provider, not completing a reset, not confirming the address, and not
// refreshing a session opened before the factor was enrolled.
func TestIdentityContractSecondFactorBlocksEverySessionPath(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := contractEmail("contract-mfa")
	subject := fmt.Sprintf("google-sub-contract-mfa-%d", time.Now().UnixNano())
	app := newContractApp(t, subject, email)

	const password = "password-before-mfa"
	reg := registerAt(t, app.URL, email, password)
	confirmEmailFromOwnSession(t, app.URL, email, reg.Token)

	// Signed in through the provider before the factor existed.
	location := runGoogleCallback(t, app.URL)
	require.Empty(t, oauthError(t, location), location)
	_, providerRefresh := tokensFromRedirect(t, location)
	require.NotEmpty(t, providerRefresh)

	plantConfirmedSecondFactor(t, email)

	status, raw := app.login(t, email, password)
	requireAPIError(t, status, raw, http.StatusForbidden, "AUTH.MFA_REQUIRED")

	location = runGoogleCallback(t, app.URL)
	require.Equal(t, "AUTH.MFA_REQUIRED", oauthError(t, location), location)
	require.NotContains(t, location, "#token=")

	status, _ = helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/refresh", "",
		map[string]any{"refreshToken": providerRefresh})
	require.Equal(t, http.StatusUnauthorized, status, "a session opened before the factor stops renewing")

	status, rawBytes := helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/refresh", "",
		map[string]any{"refreshToken": reg.RefreshToken})
	require.Equal(t, http.StatusUnauthorized, status, string(rawBytes))

	// Completing a reset sets the password and hands out nothing.
	helpers.DoJSON(t, http.MethodPost, app.URL+"/auth/password-reset/request", "",
		map[string]any{"email": email}, nil)
	msg, ok := testMailer.LastFor(email)
	require.True(t, ok)
	const newPassword = "password-after-reset"
	status, rawBytes = helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/password-reset/confirm", "",
		map[string]any{"token": extractResetToken(t, msg.Text), "newPassword": newPassword})
	require.Equal(t, http.StatusOK, status, string(rawBytes))
	requireNoTokens(t, rawBytes, "a completed reset issues no session")
	status, raw = app.login(t, email, newPassword)
	requireAPIError(t, status, raw, http.StatusForbidden, "AUTH.MFA_REQUIRED")
}

// Confirming the address is not a sign-in either, and a provider cannot link
// onto a confirmed account that holds a factor: the refusal comes before the
// identity is written, so nothing is left linked.
func TestIdentityContractSecondFactorBlocksConfirmationAndLinking(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := contractEmail("contract-mfa-link")
	subject := fmt.Sprintf("google-sub-contract-mfa-link-%d", time.Now().UnixNano())
	app := newContractApp(t, subject, email)

	reg := registerAt(t, app.URL, email, "password-mfa-link")
	plantConfirmedSecondFactor(t, email)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/verify-email/confirm", "",
		map[string]any{"token": verificationToken(t, email)})
	require.Equal(t, http.StatusOK, status, string(raw))
	requireNoTokens(t, raw, "confirming the address issues no session")

	location := runGoogleCallback(t, app.URL)
	require.Equal(t, "AUTH.MFA_REQUIRED", oauthError(t, location), location)

	var linked int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM identities WHERE provider = 'google' AND user_id = ?`,
		userInternalID(t, reg.User.ID)).Scan(&linked))
	require.Zero(t, linked, "a refused sign-in leaves no identity linked")
}

// A provider vouching for an address does not hand over an account whose
// owner never proved it while that account holds a password: whoever
// registered it could have set it. The link is refused and the password is
// left as it was.
func TestIdentityContractLinkToAnUnverifiedPasswordAccountIsRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := contractEmail("contract-link")
	subject := fmt.Sprintf("google-sub-contract-link-%d", time.Now().UnixNano())
	app := newContractApp(t, subject, email)

	const password = "registrant-password"
	reg := registerAt(t, app.URL, email, password)
	userID := userInternalID(t, reg.User.ID)

	location := runGoogleCallback(t, app.URL)
	require.Equal(t, "AUTH.LINK_REFUSED", oauthError(t, location), location)
	require.NotContains(t, location, "#token=")

	var hash sql.NullString
	require.NoError(t, testDB.QueryRow(
		`SELECT password_hash FROM identities WHERE provider = 'local' AND user_id = ?`, userID).Scan(&hash))
	require.True(t, hash.Valid, "linking never clears password_hash")
	var verified sql.NullTime
	require.NoError(t, testDB.QueryRow(`SELECT email_verified_at FROM users WHERE id = ?`, userID).Scan(&verified))
	require.False(t, verified.Valid, "a refused link proves nothing about the address")
	var linked int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM identities WHERE provider = 'google' AND user_id = ?`, userID).Scan(&linked))
	require.Zero(t, linked)

	status, raw := app.login(t, email, password)
	require.Equal(t, http.StatusOK, status, "the password still opens the account: %s", raw)
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, app.URL+"/user", reg.Token, nil)
	require.Equal(t, http.StatusOK, status, "and its sessions survive the refusal")
}

// The refusal is about what the account holds, not about it being
// unconfirmed: one with neither a password nor a factor is the provider's to
// claim, and is confirmed by it.
func TestIdentityContractLinkToAnUnverifiedAccountWithoutCredentialsIsAllowed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := contractEmail("contract-claim")
	subject := fmt.Sprintf("google-sub-contract-claim-%d", time.Now().UnixNano())
	app := newContractApp(t, subject, email)

	reg := registerAt(t, app.URL, email, "password-to-drop")
	_, err := testDB.Exec(
		`UPDATE identities SET password_hash = NULL WHERE provider = 'local' AND subject = ?`, email)
	require.NoError(t, err)

	location := runGoogleCallback(t, app.URL)
	require.Empty(t, oauthError(t, location), location)
	token, _ := tokensFromRedirect(t, location)
	var me struct {
		ID            string `json:"id"`
		EmailVerified bool   `json:"emailVerified"`
	}
	helpers.DoJSON(t, http.MethodGet, app.URL+"/user", token, nil, &me)
	require.Equal(t, reg.User.ID, me.ID)
	require.True(t, me.EmailVerified)
}

// One live row in the core allowlist restricts sign-in on the whole instance,
// so this test does not run beside the others and withdraws its rows on the
// way out.
func TestIdentityContractCoreAllowlistRestrictsSignIn(t *testing.T) {
	bootstrap(t)

	seq := time.Now().UnixNano()
	insider := fmt.Sprintf("contract-insider-%d@test.local", seq)
	outsider := fmt.Sprintf("contract-outsider-%d@test.local", seq)
	latecomer := fmt.Sprintf("contract-latecomer-%d@test.local", seq)
	app := newContractApp(t, fmt.Sprintf("google-sub-contract-allow-%d", seq), latecomer)

	const password = "password-allowlist"
	registerAt(t, app.URL, outsider, password)

	t.Cleanup(func() {
		_, err := testDB.Exec(
			`UPDATE oauth_signin_allowlist SET enabled = FALSE WHERE entry_kind = 'email' AND entry_value = ?`,
			insider)
		require.NoError(t, err)
	})
	// Written the way the other application's admin screen writes it.
	_, err := testDB.Exec(
		`INSERT INTO oauth_signin_allowlist (public_id, entry_kind, entry_value, notes)
		 VALUES (UUID_TO_BIN(UUID()), 'email', ?, 'identity contract')`, insider)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/register", "",
		map[string]any{"name": "Latecomer", "email": latecomer, "password": password})
	requireAPIError(t, status, string(raw), http.StatusForbidden, "AUTH.SIGNUP_NOT_ALLOWED")

	status, _ = app.login(t, outsider, password)
	require.Equal(t, http.StatusUnauthorized, status, "an existing account off the list stops signing in")

	location := runGoogleCallback(t, app.URL)
	require.Equal(t, "oauth_not_allowed", oauthError(t, location), location)

	status, raw = helpers.DoJSONStatus(t, http.MethodPost, app.URL+"/auth/register", "",
		map[string]any{"name": "Insider", "email": strings.ToUpper(insider), "password": password})
	require.Equal(t, http.StatusOK, status, "the listed address is admitted, in any case: %s", raw)
	status, _ = app.login(t, insider, password)
	require.Equal(t, http.StatusOK, status)
}

// A membership the workspace disabled is a removal, and only an explicit
// addition undoes it: signing in again and following a join link both leave
// it disabled, and the grants the member still holds stop working.
func TestIdentityContractRemovedWorkspaceMemberStaysOut(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	removed := helpers.NewTenant(t, testServerURL)
	removedID := userInternalID(t, removed.UserID)
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+owner.CalendarID+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)

	// The other application removes the member: the workspace row first.
	_, err := testDB.Exec(
		`UPDATE workspace_members SET enabled = FALSE WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, removedID)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+removed.CalendarID, removed.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status,
		"a calendar grant counts only inside the workspace: %s", string(raw))

	// Then the calendar grants, which is the state its removal leaves.
	_, err = testDB.Exec(
		`UPDATE calendar_members SET enabled = FALSE WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, removedID)
	require.NoError(t, err)

	membershipEnabled := func() bool {
		var enabled bool
		require.NoError(t, testDB.QueryRow(
			`SELECT enabled FROM workspace_members WHERE workspace_id = ? AND user_id = ?`,
			workspaceID, removedID).Scan(&enabled))
		return enabled
	}

	var login struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": removed.Email, "password": removed.Password}, &login)
	require.False(t, membershipEnabled(), "signing in does not rejoin a removed member")

	status, raw = helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+inv.Token+"/accept", login.Token, nil)
	require.Equal(t, http.StatusForbidden, status, string(raw))
	require.False(t, membershipEnabled(), "nor does a join link")
	var grants int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM calendar_members WHERE user_id = ? AND enabled = TRUE`, removedID).Scan(&grants))
	require.Zero(t, grants, "and the link grants no calendar either")
}

// newRefreshSecret is a refresh token in the shape any application might use.
func newRefreshSecret(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// plantFlowSession writes a session the way the other application does,
// returning its public id and the plaintext refresh token.
func plantFlowSession(t *testing.T, userID uint32, expiresIn time.Duration) (publicID, refresh string) {
	t.Helper()
	refresh = newRefreshSecret(t)
	require.NoError(t, testDB.QueryRow(`SELECT UUID()`).Scan(&publicID))
	_, err := testDB.Exec(
		`INSERT INTO sessions (public_id, user_id, audience, refresh_hash, expires_at)
		 VALUES (UUID_TO_BIN(?), ?, 'flow', ?, NOW(3) + INTERVAL ? SECOND)`,
		publicID, userID, helpers.SHA256Hex([]byte(refresh)), int64(expiresIn/time.Second))
	require.NoError(t, err)
	return publicID, refresh
}

// Sessions are shared rows told apart by audience. This application neither
// refreshes nor accepts nor purges another's sessions, and revoking all of a
// user's sessions reaches every audience.
func TestIdentityContractSessionsOfAnotherAudience(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	userID := userInternalID(t, tt.UserID)

	var audience string
	require.NoError(t, testDB.QueryRow(
		`SELECT audience FROM sessions WHERE user_id = ? ORDER BY id LIMIT 1`, userID).Scan(&audience))
	require.Equal(t, "time", audience, "this application writes its own name")

	flowPub, flowRefresh := plantFlowSession(t, userID, 24*time.Hour)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": flowRefresh})
	require.Equal(t, http.StatusUnauthorized, status, "another audience's refresh token is not this one's")
	var hash string
	require.NoError(t, testDB.QueryRow(
		`SELECT refresh_hash FROM sessions WHERE public_id = UUID_TO_BIN(?)`, flowPub).Scan(&hash))
	require.Equal(t, helpers.SHA256Hex([]byte(flowRefresh)), hash, "and it is not rotated either")

	access, err := auth.GenerateToken(flowPub, helpers.TestJWTSecret)
	require.NoError(t, err)
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", access, nil)
	require.Equal(t, http.StatusUnauthorized, status, "nor does it open a request here")

	expiredPub, _ := plantFlowSession(t, userID, -time.Hour)
	_, err = generated.New(testDB).DeleteExpiredSessions(context.Background(), generated.DeleteExpiredSessionsParams{
		ExpiresAt: time.Now(),
		Limit:     10000,
	})
	require.NoError(t, err)
	var kept int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE public_id = UUID_TO_BIN(?)`, expiredPub).Scan(&kept))
	require.Equal(t, 1, kept, "the other application purges its own expired sessions")

	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/password", tt.AccessToken,
		map[string]any{"currentPassword": tt.Password, "newPassword": "another-long-password"}, nil)
	var revoked sql.NullTime
	require.NoError(t, testDB.QueryRow(
		`SELECT revoked_at FROM sessions WHERE public_id = UUID_TO_BIN(?)`, flowPub).Scan(&revoked))
	require.True(t, revoked.Valid, "changing the password ends the user's sessions in every application")
}
