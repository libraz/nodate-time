package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// An occurrence moved past the end of its series stays visible after a later
// whole-series edit. The edit recomputes the series end from its rule, and
// that end alone no longer reaches the moved occurrence.
func TestSeriesEditKeepsAnOccurrenceMovedPastTheEnd(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	rule := map[string]any{"freq": "daily", "interval": 1, "count": 3}
	occ := createDailySeries(t, calURL, owner.AccessToken, rule)
	require.Len(t, occ, 3)

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[2].ID+"?scope=this", owner.AccessToken,
		eventBody("Moved to June", "2026-06-15T10:00:00+09:00", "2026-06-15T10:30:00+09:00"), nil)

	// A title change, nothing else.
	body := eventBody("Renamed stand-up", "2026-05-01T10:00:00+09:00", "2026-05-01T10:30:00+09:00")
	body["recurrenceRule"] = rule
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+strings.Split(occ[0].ID, "_")[0], owner.AccessToken, body, nil)

	var june []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-01&end=2026-06-30&tz=Asia/Tokyo",
		owner.AccessToken, nil, &june)
	require.Len(t, june, 1, "the moved occurrence must survive the series edit")
	require.Equal(t, occ[2].ID, june[0].ID)
	require.Equal(t, "Moved to June", june[0].Title)
}

// Removing the repeat from a series whose edited occurrences share a
// participant succeeds, and keeps succeeding on a later collapse involving
// the same person.
func TestRemovingRecurrenceWithEditedOccurrencesSharingAParticipant(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")

	occ := createWeeklyFriday(t, calURL, owner.AccessToken)
	seriesURL := calURL + "/events/" + strings.Split(occ[0].ID, "_")[0]
	weekly := map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}

	for round := range 2 {
		if round > 0 {
			body := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00")
			body["recurrenceRule"] = weekly
			helpers.DoJSON(t, http.MethodPut, seriesURL, owner.AccessToken, body, nil)
		}
		for _, i := range []int{1, 2} {
			helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[i].ID+"?scope=this", owner.AccessToken,
				eventBody("With guest", occ[i].StartAt, occ[i].StartAt, guest.UserID), nil)
		}

		status, raw := helpers.DoJSONStatus(t, http.MethodPut, seriesURL, owner.AccessToken,
			eventBody("Single meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00"))
		require.Equal(t, http.StatusOK, status, "round %d: removing the repeat: %s", round, raw)

		var april []recInstance
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30&tz=Asia/Tokyo",
			owner.AccessToken, nil, &april)
		require.Len(t, april, 1, "round %d: one event, no leftover occurrences: %+v", round, april)
		require.False(t, april[0].IsRecurrence)
	}
}
