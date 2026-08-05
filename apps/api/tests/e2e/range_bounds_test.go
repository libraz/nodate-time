package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestWideRangeIsRefusedOnBothDoors verifies the width of a listing is bounded
// wherever it is asked for. The days parameter was capped at a year, but
// naming both ends skipped the cap: every series in the calendar expands per
// occurrence over whatever window is asked for, and the public feed takes no
// credential at all.
func TestWideRangeIsRefusedOnBothDoors(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2020-01-01&end=2030-01-01", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, "a ten-year window should be refused")

	// A month is ordinary and stays ordinary.
	ok, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, ok)

	token := publicShareToken(t, calURL, tt.AccessToken)
	pubStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?start=2020-01-01&end=2030-01-01", "", nil)
	require.Equal(t, http.StatusBadRequest, pubStatus,
		"the door with no credential on it needs the bound most")
}

// TestInvertedRangeAnswersTheSameWayEitherDoor verifies the two listings agree
// about a malformed window. One refused it and the other returned an empty
// list, so the same client bug looked like an empty calendar through one of
// them.
func TestInvertedRangeAnswersTheSameWayEitherDoor(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-04-30&end=2026-04-01", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status)

	token := publicShareToken(t, calURL, tt.AccessToken)
	pubStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?start=2026-04-30&end=2026-04-01", "", nil)
	require.Equal(t, http.StatusBadRequest, pubStatus)
}

// TestMorningEventOnTheFirstStaysInItsMonth verifies the window is resolved as
// days where the caller reads them. Resolved at UTC midnight, a JST window
// opens nine hours into the first of the month, and an event that ends before
// that is missing from the very month it belongs to.
func TestMorningEventOnTheFirstStaysInItsMonth(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "Early meeting",
			"allDay":   false,
			"startAt":  "2026-04-01T08:00:00+09:00",
			"endAt":    "2026-04-01T09:00:00+09:00",
			"timezone": "Asia/Tokyo",
		}, nil)

	var events []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-04-01&end=2026-04-30&tz=Asia/Tokyo",
		tt.AccessToken, nil, &events)
	require.Len(t, events, 1, "an 08:00 event on the first belongs to April")
	require.Equal(t, "Early meeting", events[0].Title)

	// The public feed reads the same days the same way; a share link that
	// dropped the event would show a different calendar than its owner sees.
	token := publicShareToken(t, calURL, tt.AccessToken)
	var shared []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?start=2026-04-01&end=2026-04-30&tz=Asia/Tokyo",
		"", nil, &shared)
	require.Len(t, shared, 1)
	require.Equal(t, "Early meeting", shared[0].Title)
}
