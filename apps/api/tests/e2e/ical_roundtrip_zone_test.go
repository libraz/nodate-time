package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// zonedInstance is one listed occurrence with the zone its event lives in.
type zonedInstance struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	StartAt  string `json:"startAt"`
	Timezone string `json:"timezone"`
}

func zonesByTitle(t *testing.T, calURL, token, query string) map[string]zonedInstance {
	t.Helper()
	var listed []zonedInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?"+query, token, nil, &listed)
	out := make(map[string]zonedInstance, len(listed))
	for _, e := range listed {
		out[e.Title] = e
	}
	return out
}

// A backup restores every timed event in the zone it was made in, single
// events and changed occurrences alike. Read back as UTC, an event later made
// recurring here would expand in UTC and drift across DST.
func TestICalRoundTripKeepsEveryTimedEventsZone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + tt.CalendarID

	helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", tt.AccessToken, map[string]any{
		"title": "Dentist", "allDay": false, "timezone": "America/New_York",
		"startAt": "2026-06-10T09:00:00-04:00", "endAt": "2026-06-10T10:00:00-04:00",
	}, nil)
	helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", tt.AccessToken, map[string]any{
		"title": "Standup", "allDay": false, "timezone": "Asia/Tokyo",
		"startAt": "2026-06-01T09:00:00+09:00", "endAt": "2026-06-01T09:30:00+09:00",
		"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"MO"}},
	}, nil)

	const window = "start=2026-06-01&end=2026-06-30&tz=UTC"
	var second string
	for _, e := range zonesByTitle(t, sourceURL, tt.AccessToken, "start=2026-06-08&end=2026-06-08&tz=Asia/Tokyo") {
		if e.Title == "Standup" {
			second = e.ID
		}
	}
	require.NotEmpty(t, second)
	moved := eventBody("Standup from New York", "2026-06-08T08:00:00-04:00", "2026-06-08T08:30:00-04:00")
	moved["timezone"] = "America/New_York"
	helpers.DoJSON(t, http.MethodPut, sourceURL+"/events/"+second+"?scope=this", tt.AccessToken, moved, nil)

	source := zonesByTitle(t, sourceURL, tt.AccessToken, window)
	require.Equal(t, "America/New_York", source["Dentist"].Timezone)
	require.Equal(t, "America/New_York", source["Standup from New York"].Timezone)

	ics := fetchICS(t, sourceURL, tt.AccessToken)
	require.Contains(t, ics, "DTSTART;TZID=America/New_York:20260610T090000")
	require.Contains(t, ics, "DTSTART;TZID=America/New_York:20260608T080000")
	require.Contains(t, ics, "BEGIN:VTIMEZONE\r\nTZID:America/New_York\r\n",
		"every TZID the file references has to be defined in it")

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Restored")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken, map[string]any{"ics": ics}, &res)
	require.Equal(t, 3, res.Imported, "the single event, the series and its changed occurrence")

	restored := zonesByTitle(t, targetURL, tt.AccessToken, window)
	for _, title := range []string{"Dentist", "Standup", "Standup from New York"} {
		require.Equal(t, source[title].Timezone, restored[title].Timezone, "%s keeps its zone", title)
		require.Equal(t, source[title].StartAt, restored[title].StartAt, "%s keeps its instant", title)
	}
	require.False(t, strings.Contains(ics, "DTSTART:20260610T130000Z"),
		"a zoned event must not leave as a bare UTC instant")
}
