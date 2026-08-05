package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fetchICS(t *testing.T, calURL, token string) string {
	t.Helper()
	body, _ := fetchICSWithWindow(t, calURL, token, "")
	return body
}

// fetchICSWithWindow returns the exported body and the window the response
// says it covers.
func fetchICSWithWindow(t *testing.T, calURL, token, query string) (string, string) {
	t.Helper()
	url := calURL + "/export?format=ics"
	if query != "" {
		url += "&" + query
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body), resp.Header.Get("X-Export-Window")
}

// TestICalImportPreservesTimezoneAndRecurrence verifies that a TZID wall-clock
// DTSTART is anchored in its zone (not read as UTC) and that an RRULE imports
// as a recurring series rather than a single event.
func TestICalImportPreservesTimezoneAndRecurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	ics := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:tokyo-daily@example.com",
		"SUMMARY:Tokyo meeting",
		"DTSTART;TZID=Asia/Tokyo:20260601T100000",
		"DTEND;TZID=Asia/Tokyo:20260601T110000",
		"RRULE:FREQ=DAILY;COUNT=3",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	var imp struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", tt.AccessToken,
		map[string]any{"ics": ics}, &imp)
	require.Equal(t, 1, imp.Imported)
	require.Zero(t, imp.Skipped)
	require.Zero(t, imp.Failed)

	var listed []struct {
		Title    string `json:"title"`
		StartAt  string `json:"startAt"`
		Timezone string `json:"timezone"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-25&end=2026-06-10", tt.AccessToken, nil, &listed)
	require.Len(t, listed, 3, "RRULE must expand into its occurrences")
	for _, e := range listed {
		assert.Equal(t, "Tokyo meeting", e.Title)
		assert.Equal(t, "Asia/Tokyo", e.Timezone)
	}
	// 10:00 JST is 01:00 UTC — parsing the wall clock as UTC would yield 10:00Z.
	assert.Contains(t, listed[0].StartAt, "2026-06-01T01:00:00Z")
}

// TestICalImportSkipsUnsupportedRRule verifies that an RRULE outside the
// supported subset skips the event with a counted warning instead of silently
// importing it as a single occurrence.
func TestICalImportSkipsUnsupportedRRule(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	ics := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:hourly@example.com",
		"SUMMARY:Hourly thing",
		"DTSTART:20260601T100000Z",
		"DTEND:20260601T110000Z",
		"RRULE:FREQ=HOURLY;COUNT=5",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	var imp struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", tt.AccessToken,
		map[string]any{"ics": ics}, &imp)
	assert.Zero(t, imp.Imported)
	assert.Equal(t, 1, imp.Skipped)
	assert.Zero(t, imp.Failed)
}

// The export reflects what the app holds: the series leaves as a rule, its
// cancellation as an EXDATE, and its edited occurrence as a second VEVENT
// under the same UID naming the one it replaces.
func TestICalExportWritesTheSeriesAndItsDepartures(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)

	// Cancel 2026-04-10 and override 2026-04-17.
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evts[1].ID+"?scope=this", tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evts[2].ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title":              "Special export",
			"allDay":             false,
			"startAt":            "2026-04-17T20:00:00+09:00",
			"endAt":              "2026-04-17T21:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, nil)

	ics := fetchICS(t, calURL, tt.AccessToken)

	// The series is one VEVENT anchored on its first occurrence, carrying the
	// rule rather than a copy of every date it produces.
	assert.Contains(t, ics, "DTSTART:20260403T060000Z")
	assert.Contains(t, ics, "RRULE:FREQ=WEEKLY;BYDAY=FR;WKST=SU")

	// The cancelled occurrence (2026-04-10 06:00 UTC) leaves as an exclusion,
	// not as a missing date the reader has to infer.
	assert.Contains(t, ics, "EXDATE:20260410T060000Z")
	assert.NotContains(t, ics, "DTSTART:20260410T060000Z")

	// The edited occurrence is a second VEVENT with its overridden title and
	// time (11:00 UTC), naming the occurrence it stands in for.
	assert.Contains(t, ics, "Special export")
	assert.Contains(t, ics, "DTSTART:20260417T110000Z")
	assert.Contains(t, ics, "RECURRENCE-ID:20260417T060000Z")

	// Two VEVENTs and one UID between them: the series and its one departure.
	assert.Equal(t, 2, strings.Count(ics, "BEGIN:VEVENT"))
	uid := strings.Split(strings.Split(ics, "UID:")[1], "\r\n")[0]
	assert.Equal(t, 2, strings.Count(ics, "UID:"+uid),
		"a changed occurrence belongs to its series, so it shares the UID")
}

// The window is the caller's to choose, and whatever it was is stated in the
// response. An export that silently stops at a boundary the caller never set
// is a backup with a hole in it.
func TestICalExportCoversTheWholeCalendarUnlessAskedNotTo(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Far outside the window a fixed-range export would have used.
	for _, when := range []string{"1998-03-14", "2064-09-02"} {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
			map[string]any{
				"title": "Anniversary " + when, "allDay": false,
				"startAt": when + "T10:00:00+09:00", "endAt": when + "T11:00:00+09:00",
			}, nil)
	}

	full, window := fetchICSWithWindow(t, calURL, tt.AccessToken, "")
	assert.Equal(t, "full", window)
	assert.Contains(t, full, "Anniversary 1998-03-14")
	assert.Contains(t, full, "Anniversary 2064-09-02")

	narrow, window := fetchICSWithWindow(t, calURL, tt.AccessToken, "from=2060-01-01&to=2070-01-01")
	assert.Equal(t, "2060-01-01/2070-01-02", window,
		"the response must say what it actually covered")
	assert.NotContains(t, narrow, "Anniversary 1998-03-14")
	assert.Contains(t, narrow, "Anniversary 2064-09-02")
}
