package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// createDailyAcrossSpringForward creates a daily 19:00 series in
// America/New_York covering the 2026 spring-forward transition (02:00 on
// Sunday 8 March) and returns the instances that fall in the window.
func createDailyAcrossSpringForward(t *testing.T, calURL, token string) []recInstance {
	t.Helper()
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token,
		map[string]any{
			"title":    "Evening call",
			"allDay":   false,
			"startAt":  "2026-03-05T19:00:00-05:00",
			"endAt":    "2026-03-05T20:00:00-05:00",
			"timezone": "America/New_York",
			"recurrenceRule": map[string]any{
				"freq":     "daily",
				"interval": 1,
				"count":    8,
			},
		}, &evt)
	require.NotEmpty(t, evt.ID)

	// The dates are New York days, which is where the series lives; read as
	// days anywhere else the window covers a different set of instants and
	// clips an occurrence off the end.
	var evts []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-03-05&end=2026-03-12&tz=America/New_York", token, nil, &evts)
	require.Len(t, evts, 8)
	return evts
}

// A composite occurrence id names a day, and the day has to be read in the
// event's own timezone. A 19:00 New York series crosses midnight UTC before
// the clocks go forward and not after, so two consecutive occurrences share a
// UTC date — and with it an id. The second one then has no id of its own: it
// cannot be opened, edited or cancelled, and the first one answers in its
// place.
func TestOccurrenceIDsStayUniqueAcrossADSTTransition(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	evts := createDailyAcrossSpringForward(t, calURL, tt.AccessToken)

	seen := make(map[string]string, len(evts))
	for _, e := range evts {
		require.NotContains(t, seen, e.ID,
			"two occurrences share id %s (the other starts at %s)", e.ID, seen[e.ID])
		seen[e.ID] = e.StartAt

		start, perr := time.Parse(time.RFC3339, e.StartAt)
		require.NoError(t, perr)
		_, suffix, found := strings.Cut(e.ID, "_")
		require.True(t, found, "a recurring instance must carry a composite id")
		require.Equal(t, start.In(loc).Format("20060102"), suffix,
			"the id must name the day the occurrence falls on where the event lives")
	}
	require.Len(t, seen, 8)
}

// Each occurrence either side of the transition must resolve back to itself.
// A shared id would make one of them unreachable and hand back the other.
func TestEachOccurrenceAcrossADSTTransitionResolvesToItself(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	for _, e := range createDailyAcrossSpringForward(t, calURL, tt.AccessToken) {
		var got recInstance
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+e.ID, tt.AccessToken, nil, &got)
		require.Equal(t, e.ID, got.ID)
		require.Equal(t, e.StartAt, got.StartAt,
			"id %s must resolve to the occurrence it was minted for", e.ID)
	}
}

// Cancelling the occurrence on the day the clocks change must take that one
// and no other.
func TestCancellingTheTransitionDayOccurrenceLeavesItsNeighbours(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	before := createDailyAcrossSpringForward(t, calURL, tt.AccessToken)
	var target recInstance
	for _, e := range before {
		if strings.HasSuffix(e.ID, "_20260308") {
			target = e
			break
		}
	}
	require.NotEmpty(t, target.ID, "the transition day must have an occurrence of its own")

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		calURL+"/events/"+target.ID+"?scope=this", tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "cancel occurrence: %s", string(raw))

	var after []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-03-05&end=2026-03-12&tz=America/New_York", tt.AccessToken, nil, &after)
	require.Len(t, after, 7)
	for _, e := range after {
		require.NotEqual(t, target.StartAt, e.StartAt, "the cancelled occurrence must be gone")
	}
}
