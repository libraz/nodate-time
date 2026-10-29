package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestAllowedEmailReasonSurvivesTheRoundTrip pins the field name the allow-list
// is written and read under. An administrator states why an address is
// excepted; if the value is accepted under one name and served under another it
// is stored and never seen again, and the list is unreadable a year later.
func TestAllowedEmailReasonSurvivesTheRoundTrip(t *testing.T) {
	bootstrap(t)
	// A live allow-list row restricts sign-in for the whole instance, so this
	// test runs in the serial phase, while parallel tests are still paused,
	// and withdraws its row before it ends.

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	// The allow-list is unique on the address and this test leaves its row
	// behind, so the address has to differ per run.
	address := fmt.Sprintf("reasoned-%d@example.com", time.Now().UnixNano())
	reason := fmt.Sprintf("contractor until %d", time.Now().UnixNano())
	t.Cleanup(func() { withdrawAllowedEmail(t, address) })

	var created struct {
		ID     string `json:"id"`
		Email  string `json:"email"`
		Reason string `json:"reason"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": address, "reason": reason}, &created)
	require.Equal(t, reason, created.Reason, "the creation response should echo the stated reason")

	var list struct {
		Emails []struct {
			ID     string `json:"id"`
			Email  string `json:"email"`
			Reason string `json:"reason"`
		} `json:"emails"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		nil, &list)

	var found bool
	for _, e := range list.Emails {
		if e.Email == address {
			found = true
			require.Equal(t, reason, e.Reason, "the listing should serve the reason that was written")
		}
	}
	require.True(t, found, "the address that was just allowed should be in the list")
}

// An address the account columns cannot store is refused as a bad request; it
// used to reach the insert and come back as a server error.
func TestAllowedEmailRefusesAnAddressItCannotStore(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": fmt.Sprintf("山田-%d@example.com", time.Now().UnixNano()), "reason": "non-ASCII"})
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	require.Contains(t, string(raw), "AUTH.EMAIL_UNSUPPORTED")
}

// withdrawAllowedEmail clears a test's allow-list row so it stops restricting
// sign-in for the tests that run after it.
func withdrawAllowedEmail(t *testing.T, address string) {
	t.Helper()
	_, err := testDB.Exec(
		`UPDATE oauth_signin_allowlist SET enabled = FALSE WHERE entry_kind = 'email' AND entry_value = ?`, address)
	require.NoError(t, err)
}

// With no configured domain list, one live row in oauth_signin_allowlist is
// enough to restrict sign-in to the addresses it admits, and withdrawing the
// row opens sign-in again.
func TestAllowedEmailRowRestrictsSignInWithoutADomainList(t *testing.T) {
	bootstrap(t)
	// The row restricts the whole instance, so this runs in the serial phase.

	var live int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM oauth_signin_allowlist WHERE enabled = TRUE`).Scan(&live))
	require.Zero(t, live, "an earlier test left a live allow-list row behind")

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	seq := time.Now().UnixNano()
	insider := fmt.Sprintf("insider-%d@example.com", seq)
	outsider := fmt.Sprintf("outsider-%d@example.org", seq)
	const password = "correcthorsebattery"
	register := func(email string) (int, []byte) {
		return helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/register", "",
			map[string]any{"name": "Rule", "email": email, "password": password})
	}
	login := func(email string) int {
		status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
			map[string]any{"email": email, "password": password})
		return status
	}

	status, raw := register(outsider)
	require.Equal(t, http.StatusOK, status, "open before any row exists: %s", string(raw))

	t.Cleanup(func() { withdrawAllowedEmail(t, insider) })
	var created struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": fmt.Sprintf("  %s ", insider)}, &created)

	var list struct {
		Restricted bool `json:"restricted"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/allowed-emails", admin.AccessToken, nil, &list)
	require.True(t, list.Restricted, "a live row reports the instance as restricted")

	status, raw = register(fmt.Sprintf("other-%d@example.org", seq))
	require.Equal(t, http.StatusForbidden, status, string(raw))
	require.Contains(t, string(raw), "AUTH.SIGNUP_NOT_ALLOWED")
	require.Equal(t, http.StatusUnauthorized, login(outsider),
		"an existing account outside the list stops signing in")
	status, raw = register(insider)
	require.Equal(t, http.StatusOK, status, "the listed address registers: %s", string(raw))
	require.Equal(t, http.StatusOK, login(insider))

	status, raw = helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/admin/allowed-emails/"+created.ID, admin.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	require.Equal(t, http.StatusOK, login(outsider), "withdrawing the only row opens sign-in again")
}
