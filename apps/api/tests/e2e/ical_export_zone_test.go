package e2e

import (
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// exportResponse fetches an export and returns its body and headers.
func exportResponse(t *testing.T, calURL, token, query string) (string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, calURL+"/export?"+query, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	return string(body), resp.Header
}

// listedStarts lists a calendar and returns "title@instant" for every
// occurrence, sorted, so two calendars can be compared occurrence by
// occurrence.
func listedStarts(t *testing.T, calURL, token, query string) []string {
	t.Helper()
	var evts []struct {
		Title   string    `json:"title"`
		StartAt time.Time `json:"startAt"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?"+query, token, nil, &evts)
	out := make([]string, 0, len(evts))
	for _, e := range evts {
		out = append(out, e.Title+"@"+e.StartAt.UTC().Format(time.RFC3339))
	}
	sort.Strings(out)
	return out
}

// A series exported and imported again has to land on the same instants. A
// Monday 08:00 Tokyo series is a Sunday in UTC, and a New York one crosses a
// DST change inside the window; both drift if the rule is written in UTC.
func TestICalRoundTripKeepsSeriesOnTheirDays(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + tt.CalendarID

	for _, s := range []struct{ title, start, end, tz string }{
		{"Tokyo Monday", "2026-02-02T08:00:00+09:00", "2026-02-02T09:00:00+09:00", "Asia/Tokyo"},
		{"New York Monday", "2026-02-23T09:00:00-05:00", "2026-02-23T10:00:00-05:00", "America/New_York"},
	} {
		helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", tt.AccessToken, map[string]any{
			"title": s.title, "allDay": false, "startAt": s.start, "endAt": s.end, "timezone": s.tz,
			"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"MO"}},
		}, nil)
	}

	ics, _ := exportResponse(t, sourceURL, tt.AccessToken, "format=ics")
	require.Contains(t, ics, "DTSTART;TZID=Asia/Tokyo:20260202T080000")
	require.Contains(t, ics, "DTSTART;TZID=America/New_York:20260223T090000")
	require.Contains(t, ics, "BEGIN:VTIMEZONE")

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Round trip")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken, map[string]any{"ics": ics}, &res)
	require.Equal(t, 2, res.Imported)

	const window = "start=2026-02-01&end=2026-04-30&tz=UTC"
	want := listedStarts(t, sourceURL, tt.AccessToken, window)
	require.Contains(t, want, "New York Monday@2026-03-09T13:00:00Z", "09:00 EDT after the change")
	require.Contains(t, want, "Tokyo Monday@2026-02-01T23:00:00Z")
	require.Equal(t, want, listedStarts(t, targetURL, tt.AccessToken, window),
		"the imported copy must have the same occurrences on the same instants")
}

// from/to are days in the reader's zone, the same way the listing reads them.
func TestExportWindowIsTheReadersCalendarDays(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	for _, e := range []struct{ title, start string }{
		{"Early October", "2026-10-01T05:00:00+09:00"},
		{"Early November", "2026-11-01T05:00:00+09:00"},
	} {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, map[string]any{
			"title": e.title, "allDay": false, "startAt": e.start,
			"endAt": strings.Replace(e.start, "T05:", "T06:", 1),
		}, nil)
	}

	for _, format := range []string{"ics", "csv"} {
		body, h := exportResponse(t, calURL, tt.AccessToken,
			"format="+format+"&from=2026-10-01&to=2026-10-31&tz=Asia/Tokyo")
		require.Equal(t, "2026-10-01/2026-11-01", h.Get("X-Export-Window"), format)
		require.Contains(t, body, "Early October", format)
		require.NotContains(t, body, "Early November", format)
	}
}

// The download is named after the calendar whatever script the name is in,
// and the header itself stays ASCII.
func TestExportFilenameCarriesANonASCIIName(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "家族の予定")
	for _, format := range []string{"ics", "csv"} {
		_, h := exportResponse(t, calURL, tt.AccessToken, "format="+format)
		cd := h.Get("Content-Disposition")
		for _, r := range cd {
			require.True(t, r >= 0x20 && r <= 0x7e, "header must be ASCII: %q", cd)
		}
		require.Contains(t, cd,
			"filename*=UTF-8''%E5%AE%B6%E6%97%8F%E3%81%AE%E4%BA%88%E5%AE%9A."+format)
	}
}

// A title a spreadsheet would read as a formula leaves the CSV as text.
func TestCSVExportNeutralizesFormulas(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, map[string]any{
		"title": "=HYPERLINK(\"http://example.com\")", "allDay": false,
		"startAt": "2026-10-05T10:00:00Z", "endAt": "2026-10-05T11:00:00Z",
		"location": "@SUM(1)",
	}, nil)

	csv, _ := exportResponse(t, calURL, tt.AccessToken, "format=csv&from=2026-10-01&to=2026-10-31&tz=UTC")
	require.Contains(t, csv, `"'=HYPERLINK(""http://example.com"")"`)
	require.Contains(t, csv, ",'@SUM(1),")
	for _, line := range strings.Split(csv, "\r\n") {
		require.False(t, strings.HasPrefix(line, "="), "a row starts with a formula: %q", line)
	}
}
