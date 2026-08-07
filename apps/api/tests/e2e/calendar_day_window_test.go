package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A calendar day is a different span of instants in every zone, so a listing
// window is only meaningful once a zone is named. Resolving it as UTC days
// costs the edges of the day, and which edge depends on which side of UTC the
// caller is on -- so a test written from one zone alone passes while the other
// half of the world loses events.

// listedEvent is the part of a listing these tests read.
type listedEvent struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	StartAt string `json:"startAt"`
}

// createTimedEvent makes one event in a named zone and returns its id.
func createTimedEvent(t *testing.T, calURL, token, title, startAt, endAt, tz string) string {
	t.Helper()
	var evt struct {
		ID string `json:"id"`
	}
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", token,
		map[string]any{
			"title": title, "allDay": false,
			"startAt": startAt, "endAt": endAt, "timezone": tz,
		})
	require.Equal(t, http.StatusCreated, status, "create %s: %s", title, cut(string(raw), 300))
	require.NoError(t, json.Unmarshal(raw, &evt))
	return evt.ID
}

// listDay asks for one calendar day read in tz.
func listDay(t *testing.T, calURL, token, day, tz string) []listedEvent {
	t.Helper()
	var out []listedEvent
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start="+day+"&end="+day+"&tz="+tz, token, nil, &out)
	return out
}

func titlesOf(rows []listedEvent) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Title)
	}
	return out
}

// Ahead of UTC, the early hours of a calendar day are the previous UTC day. A
// window built on UTC days opens nine hours late in Tokyo, so anything before
// 09:00 falls out of the month it belongs to.
func TestADayAheadOfUTCKeepsItsEarlyHours(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	createTimedEvent(t, calURL, tt.AccessToken, "Tokyo dawn",
		"2026-06-15T00:30:00+09:00", "2026-06-15T01:30:00+09:00", "Asia/Tokyo")

	require.Contains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-15", "Asia/Tokyo")),
		"Tokyo dawn", "00:30 is the day it is in Tokyo, not the one before it in UTC")
	require.NotContains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-14", "Asia/Tokyo")),
		"Tokyo dawn", "and it must not also show up on the day before")
}

// Behind UTC the loss is at the other end: a New York evening is already the
// next day in UTC, so a UTC-day window closes before the day the caller is
// looking at does.
func TestADayBehindUTCKeepsItsLateEvening(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	createTimedEvent(t, calURL, tt.AccessToken, "New York nightcap",
		"2026-06-15T23:30:00-04:00", "2026-06-15T23:45:00-04:00", "America/New_York")

	require.Contains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-15", "America/New_York")),
		"New York nightcap", "23:30 is still the fifteenth in New York")
	require.NotContains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-16", "America/New_York")),
		"New York nightcap", "and it has not spilled into the next day")
}

// An event whose end equals its start is a marker at a moment, which the API
// accepts and the schema allows. The range comparison asks for end strictly
// after the window start, so a marker sitting exactly on a day boundary
// satisfies neither the day it opens nor the one it closes: it is not late
// enough for the first and not early enough for the second, and it is
// reachable from no view at all.
func TestAMarkerOnADayBoundaryIsStillOnTheCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	createTimedEvent(t, calURL, tt.AccessToken, "Midnight marker",
		"2026-06-15T00:00:00+09:00", "2026-06-15T00:00:00+09:00", "Asia/Tokyo")

	require.Contains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-15", "Asia/Tokyo")),
		"Midnight marker", "a marker at midnight belongs to the day it opens")
	require.NotContains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-14", "Asia/Tokyo")),
		"Midnight marker", "and not to the day that ends there")
}

// The same marker away from any boundary is the control: it already worked, so
// a fix that only moved the boundary would show up here.
func TestAMarkerInsideTheDayIsUnaffected(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	createTimedEvent(t, calURL, tt.AccessToken, "Noon marker",
		"2026-06-15T12:00:00+09:00", "2026-06-15T12:00:00+09:00", "Asia/Tokyo")

	require.Contains(t, titlesOf(listDay(t, calURL, tt.AccessToken, "2026-06-15", "Asia/Tokyo")),
		"Noon marker")
}

// The expander applies the same overlap test in Go that the range query
// applies in SQL, so a recurring marker loses its boundary occurrences the
// same way. Fixing only the query would leave a daily marker visible on every
// day except the ones where it matters.
func TestARecurringMarkerKeepsItsBoundaryOccurrences(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	rule := json.RawMessage(`{"freq":"daily","interval":1,"count":5}`)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Daily marker", "allDay": false,
			"startAt": "2026-06-15T00:00:00+09:00", "endAt": "2026-06-15T00:00:00+09:00",
			"timezone": "Asia/Tokyo", "recurrenceRule": rule,
		})
	require.Equal(t, http.StatusCreated, status, "create: %s", cut(string(raw), 300))
	require.NoError(t, json.Unmarshal(raw, &evt))

	// Each occurrence sits at midnight, which is the opening edge of its own
	// day and the closing edge of the day before.
	for _, day := range []string{"2026-06-15", "2026-06-16", "2026-06-17"} {
		require.Contains(t, titlesOf(listDay(t, calURL, tt.AccessToken, day, "Asia/Tokyo")),
			"Daily marker", "the occurrence at midnight on %s belongs to that day", day)
	}
}

// A recurrence the validator accepts must not be one the storage layer chokes
// on. interval and count are bounded independently, and their product walks the
// series end past the last date a DATETIME column can hold -- which arrives as
// a 500 on a request the form itself allowed the user to build.
func TestAFarFutureSeriesIsNotAServerError(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	for _, freq := range []string{"yearly", "monthly", "weekly"} {
		t.Run(freq, func(t *testing.T) {
			rule := json.RawMessage(`{"freq":"` + freq + `","interval":999,"count":1000}`)
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken,
				map[string]any{
					"title": "Far future " + freq, "allDay": false,
					"startAt":        "2026-06-15T10:00:00+09:00",
					"endAt":          "2026-06-15T11:00:00+09:00",
					"timezone":       "Asia/Tokyo",
					"recurrenceRule": rule,
				})
			require.NotEqual(t, http.StatusInternalServerError, status,
				"a rule the validator accepted must not fall over in storage: %s", cut(string(raw), 300))
			require.Less(t, status, 500, "unexpected server error: %s", cut(string(raw), 300))
		})
	}
}
