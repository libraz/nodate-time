package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// How much mail one address can be made to receive, and who is told about it.

// The per-client budget bounds what one client buys, and the number of clients
// is the attacker's to choose -- so a total for the mailbox itself is what
// actually caps the flood. Four clients asking five times each reach it and
// stop there.
//
// The half that already worked is kept: a client that has spent its own
// allowance does not spend the owner's, which
// TestOneClientCannotSpendAnotherClientsResetBudget still measures.
func TestOneMailboxIsCappedHoweverManyClientsAsk(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url, mc := mailServer(t)
	email := fmt.Sprintf("mailbox-cap-%d@test.local", time.Now().UnixNano())
	helpers.DoJSON(t, http.MethodPost, url+"/auth/register", "",
		map[string]any{"name": "Cap", "email": email, "password": "correcthorsebattery"}, nil)
	registration := mc.countFor(email)

	for _, client := range []string{"198.51.100.11", "198.51.100.12", "198.51.100.13", "198.51.100.14"} {
		for range 5 {
			requestReset(t, url, email, client)
		}
	}

	// Six an hour: two clients' worth of the per-client budget, and nothing
	// after that regardless of how many more addresses ask.
	require.Equal(t, registration+6, mc.countFor(email),
		"the address has a total of its own, so more clients do not buy more mail")
}

// Password resets and address confirmations are different mail asked for by
// different people: one is anonymous and one is the account itself. Spending
// the reset budget must not silence the confirmation the account can resend.
//
// And because the caller of the resend is signed in and asking about its own
// address, there is nothing to hide by pretending: when nothing was sent it is
// told so, rather than getting {ok:true} for a button that did nothing.
func TestResendConfirmationHasItsOwnBudgetAndSaysWhenItSendsNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url, mc := mailServer(t)
	email := fmt.Sprintf("resend-budget-%d@test.local", time.Now().UnixNano())
	var reg struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, url+"/auth/register", "",
		map[string]any{"name": "Resend", "email": email, "password": "correcthorsebattery"}, &reg)

	const client = "198.51.100.15"
	for range 3 {
		requestReset(t, url, email, client)
	}
	spent := mc.countFor(email)

	status, raw := resendVerification(t, url, reg.Token, client)
	require.Equal(t, http.StatusOK, status, "body: %s", string(raw))
	require.Equal(t, spent+1, mc.countFor(email),
		"the reset budget is not the confirmation's to spend")

	// The confirmation has a budget of its own, and it is finite too.
	for range 2 {
		status, raw = resendVerification(t, url, reg.Token, client)
		require.Equal(t, http.StatusOK, status, "body: %s", string(raw))
	}
	require.Equal(t, spent+3, mc.countFor(email))

	status, raw = resendVerification(t, url, reg.Token, client)
	require.Equal(t, http.StatusTooManyRequests, status,
		"the caller must not be told a send happened when none did: %s", string(raw))
	require.Contains(t, string(raw), "RATE.LIMITED")
	require.Equal(t, spent+3, mc.countFor(email))
}

func resendVerification(t *testing.T, url, token, client string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatusWithHeaders(t, http.MethodPost,
		url+"/user/verify-email/resend", token, nil,
		map[string]string{"X-Forwarded-For": client})
}
