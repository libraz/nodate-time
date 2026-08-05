package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// limitedServer builds a server with the two unauthenticated budgets set
// independently, which is the thing under test.
func limitedServer(t *testing.T, authLimit, shareLimit int) *httptest.Server {
	t.Helper()
	queries := generated.New(testDB)
	srv := httptest.NewServer(app.NewHandler(router.Deps{
		DB:                   testDB,
		Queries:              queries,
		WorkspaceID:          helpers.TestWorkspace(queries).ID,
		JWTSecret:            helpers.TestJWTSecret,
		WebURL:               helpers.TestWebURL,
		PasswordLoginEnabled: true,
		AuthRateLimit:        authLimit,
		ShareRateLimit:       shareLimit,
	}, app.Options{}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReadingASharedCalendarDoesNotExhaustSignIn verifies the two public doors
// are counted apart.
//
// A share link is meant to be opened by an audience, and one page view costs
// more than one request. Sharing a budget with sign-in, an ordinary readership
// behind a single address spends the allowance that exists to slow down
// credential guessing -- and everyone at that address is then locked out of
// signing in by other people reading a calendar.
func TestReadingASharedCalendarDoesNotExhaustSignIn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	// The sign-in budget is deliberately tighter than the number of reads
	// below: on one shared bucket the audience spends it entirely, which is
	// exactly what happens to a real deployment behind a single address.
	srv := limitedServer(t, 5, 500)

	for i := 0; i < 20; i++ {
		resp, err := http.Get(srv.URL + "/share/no-such-token")
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Sign-in is still reachable: the reads above were counted elsewhere. The
	// credentials are wrong, so 401 is the answer -- what matters is that it
	// is not 429.
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/login", "",
		map[string]any{"email": "nobody@example.com", "password": "wrong-password"})
	require.NotEqual(t, http.StatusTooManyRequests, status,
		"reading a shared calendar must not spend the sign-in budget")
}

// TestEachPublicBudgetStillHolds verifies neither door is simply unlimited.
func TestEachPublicBudgetStillHolds(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := limitedServer(t, 3, 3)

	var lastShare int
	for i := 0; i < 6; i++ {
		resp, err := http.Get(srv.URL + "/share/no-such-token")
		require.NoError(t, err)
		resp.Body.Close()
		lastShare = resp.StatusCode
	}
	require.Equal(t, http.StatusTooManyRequests, lastShare, "the share budget must still bite")

	var lastAuth int
	for i := 0; i < 6; i++ {
		lastAuth, _ = helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/login", "",
			map[string]any{"email": "nobody@example.com", "password": "wrong-password"})
	}
	require.Equal(t, http.StatusTooManyRequests, lastAuth, "the sign-in budget must still bite")
}
