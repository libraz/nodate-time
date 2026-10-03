package e2e

import (
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// createDailySeries creates a daily 10:00 Tokyo series starting 2026-05-01 and
// returns its instances for May, sorted by start.
func createDailySeries(t *testing.T, calURL, token string, rule map[string]any) []recInstance {
	t.Helper()
	if rule == nil {
		rule = map[string]any{"freq": "daily", "interval": 1}
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token,
		map[string]any{
			"title":          "Daily stand-up",
			"allDay":         false,
			"startAt":        "2026-05-01T10:00:00+09:00",
			"endAt":          "2026-05-01T10:30:00+09:00",
			"timezone":       "Asia/Tokyo",
			"recurrenceRule": rule,
		}, nil)
	var evts []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31&tz=Asia/Tokyo", token, nil, &evts)
	return evts
}

// TestConcurrentCancellationsAllStick cancels several occurrences of one series
// at the same time. Each cancellation rewrites the whole exclusion list, so a
// writer that read the list before another committed would write it back
// without the other's entry -- a 204 for an occurrence that then reappears.
func TestConcurrentCancellationsAllStick(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	occurrences := createDailySeries(t, calURL, owner.AccessToken, nil)
	require.Len(t, occurrences, 31)

	targets := occurrences[2:14]
	var start, done sync.WaitGroup
	start.Add(1)
	statuses := make([]int, len(targets))
	for i, occ := range targets {
		done.Add(1)
		go func(i int, id string) {
			defer done.Done()
			start.Wait()
			statuses[i], _ = helpers.DoJSONStatus(t, http.MethodDelete,
				calURL+"/events/"+id+"?scope=this", owner.AccessToken, nil)
		}(i, occ.ID)
	}
	start.Done()
	done.Wait()

	for i, status := range statuses {
		require.Equal(t, http.StatusNoContent, status, "cancelling occurrence %d", i)
	}

	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31&tz=Asia/Tokyo",
		owner.AccessToken, nil, &after)
	remaining := map[string]bool{}
	for _, e := range after {
		remaining[e.ID] = true
	}
	for _, occ := range targets {
		require.False(t, remaining[occ.ID], "cancelled occurrence %s came back", occ.ID)
	}
	require.Len(t, after, len(occurrences)-len(targets))
}
