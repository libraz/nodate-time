package e2e

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type importResult struct {
	Imported  int `json:"imported"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
	Truncated int `json:"truncated"`
}

// newCalendar creates a second calendar for the tenant to import into.
func newCalendar(t *testing.T, tt *helpers.TestTenant, name string) string {
	t.Helper()
	var cal struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", tt.AccessToken,
		map[string]any{"name": name, "color": "#2ECC87"}, &cal)
	require.NotEmpty(t, cal.ID)
	return cal.ID
}

var (
	uidLine    = regexp.MustCompile(`(?m)^UID:.*$`)
	dtstampCmp = regexp.MustCompile(`(?m)^DTSTAMP:.*$`)
)

// comparableICS strips the two properties that are expected to differ between
// two exports of the same content: the identity the server minted and the
// moment the file was written.
func comparableICS(body string) string {
	body = uidLine.ReplaceAllString(body, "UID:-")
	return dtstampCmp.ReplaceAllString(body, "DTSTAMP:-")
}

// An export is only a backup if it can be read back. A series with a
// cancellation and a changed occurrence has to survive the trip: the rule as a
// rule, the cancellation as a cancellation, and the changed occurrence still
// attached to the series rather than sitting beside it.
func TestICalRoundTripPreservesASeriesAndItsDepartures(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, sourceURL, tt.AccessToken)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		sourceURL+"/events/"+evts[1].ID+"?scope=this", tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "cancel occurrence: %s", string(raw))

	helpers.DoJSON(t, http.MethodPut, sourceURL+"/events/"+evts[2].ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title": "Moved to the evening", "allDay": false,
			"startAt": "2026-04-17T20:00:00+09:00", "endAt": "2026-04-17T21:00:00+09:00",
			"location": "", "memo": "", "url": "", "notificationOffset": nil,
			"participants": []string{}, "ownerId": nil, "recurrenceRule": nil,
		}, nil)

	exported := fetchICS(t, sourceURL, tt.AccessToken)

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Imported")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken,
		map[string]any{"ics": exported}, &res)
	require.Equal(t, 2, res.Imported, "the series and its changed occurrence")
	require.Zero(t, res.Skipped)
	require.Zero(t, res.Failed)
	require.Zero(t, res.Truncated)

	reExported := fetchICS(t, targetURL, tt.AccessToken)
	require.Equal(t,
		comparableICS(strings.Replace(exported, "X-WR-CALNAME:"+"テストカレンダー", "X-WR-CALNAME:Imported", 1)),
		comparableICS(reExported),
		"what came back out must be what went in")
}

// The occurrences themselves have to agree, not just the file. The cancelled
// one stays gone and the changed one is rendered once, in its new slot.
func TestICalRoundTripLeavesTheSameOccurrencesOnTheCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, sourceURL, tt.AccessToken)
	helpers.DoJSONStatus(t, http.MethodDelete,
		sourceURL+"/events/"+evts[1].ID+"?scope=this", tt.AccessToken, nil)
	helpers.DoJSON(t, http.MethodPut, sourceURL+"/events/"+evts[2].ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title": "Moved to the evening", "allDay": false,
			"startAt": "2026-04-17T20:00:00+09:00", "endAt": "2026-04-17T21:00:00+09:00",
			"location": "", "memo": "", "url": "", "notificationOffset": nil,
			"participants": []string{}, "ownerId": nil, "recurrenceRule": nil,
		}, nil)

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Imported")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken,
		map[string]any{"ics": fetchICS(t, sourceURL, tt.AccessToken)}, &res)
	require.Equal(t, 2, res.Imported)

	var source, imported []recInstance
	helpers.DoJSON(t, http.MethodGet,
		sourceURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &source)
	helpers.DoJSON(t, http.MethodGet,
		targetURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &imported)

	require.Equal(t, startsOf(source), startsOf(imported),
		"the same occurrences on the same instants")
	require.Len(t, imported, 3, "four Fridays less the cancelled one")
	titles := make([]string, 0, len(imported))
	for _, e := range imported {
		titles = append(titles, e.Title)
	}
	require.Contains(t, titles, "Moved to the evening")
	require.Equal(t, 1, strings.Count(strings.Join(titles, "|"), "Moved to the evening"),
		"the changed occurrence replaces one, it does not join it")
}

// A file bigger than the import limit loses events. Counting the loss is the
// difference between a migration the user can act on and one that reads as a
// success while a slice of the calendar is missing.
func TestImportReportsWhatItDidNotLookAt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Bulk")

	const overLimit = 5003
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	for i := range overLimit {
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:bulk-%d@example.com\r\nSUMMARY:Bulk %d\r\n"+
			"DTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\nEND:VEVENT\r\n", i, i)
	}
	b.WriteString("END:VCALENDAR\r\n")

	var res importResult
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", tt.AccessToken,
		map[string]any{"ics": b.String()}, &res)

	require.Equal(t, 3, res.Truncated, "the events past the limit must be counted, not dropped quietly")
	require.Equal(t, overLimit, res.Imported+res.Skipped+res.Failed+res.Truncated,
		"every event in the file must be accounted for exactly once")
}

// EXDATE is how a file says an occurrence does not happen. Dropping it hands
// back every occurrence the author cancelled.
func TestImportHonorsExdate(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "With exclusions")

	ics := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:daily-with-holes@example.com",
		"SUMMARY:Daily standup",
		"DTSTART:20260601T100000Z",
		"DTEND:20260601T101500Z",
		"RRULE:FREQ=DAILY;COUNT=5",
		"EXDATE:20260602T100000Z,20260604T100000Z",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	var res importResult
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", tt.AccessToken,
		map[string]any{"ics": ics}, &res)
	require.Equal(t, 1, res.Imported)

	var listed []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-06-01&end=2026-06-10", tt.AccessToken, nil, &listed)
	require.Equal(t,
		[]string{"2026-06-01T10:00:00Z", "2026-06-03T10:00:00Z", "2026-06-05T10:00:00Z"},
		startsOf(listed),
		"the two excluded days must not come back")
}
