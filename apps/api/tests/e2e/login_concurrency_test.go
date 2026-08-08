package e2e

import (
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestOneAccountSigningInTwiceAtOnceDoesNotFail signs one account in from
// several places at the same moment.
//
// A sign-in writes a session row pointing at the user, which takes a shared
// lock on that user row to check the foreign key, and stamps the login time on
// the same row, which needs an exclusive one. Two sign-ins for one account
// each hold the shared lock while waiting for the other to let go; MySQL rolls
// one back and the person signing in gets a 500 with nothing they can do about
// it.
//
// One account is the whole point. Sign-ins from different accounts touch
// different user rows and never meet, which is why this has to reuse a single
// tenant's credentials -- a version of this test that gave each worker its own
// account passes against the bug.
func TestOneAccountSigningInTwiceAtOnceDoesNotFail(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)

	const rounds, workers = 4, 8
	for range rounds {
		var start sync.WaitGroup
		var done sync.WaitGroup
		start.Add(1)
		statuses := make([]int, workers)
		bodies := make([]string, workers)

		for i := range workers {
			done.Add(1)
			go func(i int) {
				defer done.Done()
				start.Wait()
				status, body := helpers.DoJSONStatus(t, http.MethodPost,
					testServerURL+"/auth/login", "",
					map[string]any{"email": tenant.Email, "password": tenant.Password})
				statuses[i] = status
				bodies[i] = string(body)
			}(i)
		}
		start.Done()
		done.Wait()

		for i, status := range statuses {
			require.Equal(t, http.StatusOK, status,
				"sign-in %d must not fail because the same account is signing in beside it: %s",
				i, bodies[i])
		}
	}
}
