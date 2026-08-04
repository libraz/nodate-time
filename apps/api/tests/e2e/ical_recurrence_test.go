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
	req, err := http.NewRequest(http.MethodGet, calURL+"/export?format=ics", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
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

// TestICalExportHonorsRecurrenceExceptions verifies that the export reflects
// what the app displays: cancelled occurrences are absent and edited ones
// carry their overridden values.
func TestICalExportHonorsRecurrenceExceptions(t *testing.T) {
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

	// The cancelled occurrence (2026-04-10 06:00 UTC) must not reappear.
	assert.NotContains(t, ics, "DTSTART:20260410T060000Z")
	// The edited occurrence shows its overridden title and time (11:00 UTC).
	assert.Contains(t, ics, "Special export")
	assert.Contains(t, ics, "DTSTART:20260417T110000Z")
	assert.NotContains(t, ics, "DTSTART:20260417T060000Z")
	// The untouched occurrences are still present.
	assert.Contains(t, ics, "DTSTART:20260403T060000Z")
	assert.Contains(t, ics, "DTSTART:20260424T060000Z")
}
