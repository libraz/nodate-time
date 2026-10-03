package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The id a listing hands out for a moved occurrence reads back through GET,
// however far the occurrence was moved from the day its id names.
func TestMovedOccurrenceReadsBackAtItsListedID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	target := createWeeklyFriday(t, calURL, owner.AccessToken)[1]

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+target.ID+"?scope=this", owner.AccessToken,
		eventBody("Moved to May", "2026-05-20T15:00:00+09:00", "2026-05-20T16:00:00+09:00"), nil)

	var may []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31&tz=Asia/Tokyo",
		owner.AccessToken, nil, &may)
	var listed *recInstance
	for i := range may {
		if may[i].Title == "Moved to May" {
			listed = &may[i]
		}
	}
	require.NotNil(t, listed, "the moved occurrence is listed in May")
	require.Equal(t, target.ID, listed.ID)

	status, raw, headers := helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+listed.ID, owner.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status, "%s", raw)
	require.NotEmpty(t, headers.Get("ETag"), "the editor reads its revision through this read")
	var got recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed.ID, owner.AccessToken, nil, &got)
	require.Equal(t, listed.ID, got.ID)
	require.Equal(t, "Moved to May", got.Title)
	require.Equal(t, "2026-05-20T06:00:00Z", got.StartAt)

	// A cancelled occurrence is gone at its id; a date the rule does not
	// produce never had one.
	helpers.DoJSON(t, http.MethodDelete, calURL+"/events/"+target.ID+"?scope=this", owner.AccessToken, nil, nil)
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+target.ID, owner.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
}

// An edited occurrence that starts before a window and runs into it is in
// that window, as the unedited occurrence of the same span would be.
func TestEditedOccurrenceSpanningIntoTheWindowIsListed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	occ := createDailySeries(t, calURL, owner.AccessToken, nil)

	// 5 May's occurrence stretched into a block running to 8 May.
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[4].ID+"?scope=this", owner.AccessToken,
		eventBody("Offsite", "2026-05-05T10:00:00+09:00", "2026-05-08T18:00:00+09:00"), nil)

	var day []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-07&end=2026-05-07&tz=Asia/Tokyo",
		owner.AccessToken, nil, &day)
	count := 0
	for _, e := range day {
		if e.Title == "Offsite" {
			count++
			require.Equal(t, occ[4].ID, e.ID)
		}
	}
	require.Equal(t, 1, count, "the block is on the 7th once: %+v", day)
}
