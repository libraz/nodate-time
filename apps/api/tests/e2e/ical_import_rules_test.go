package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func vcalendar(lines ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n"
}

// A file naming a quarter of a million cancellations is refused, quickly. Read
// one by one into a list that checks for duplicates, it pinned a core for
// minutes. The bound is far above the linear cost so a loaded host never trips
// it; only that quadratic walk would.
func TestImportRefusesAnExdateFlood(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	values := make([]string, 0, 250000)
	for i := range 250000 {
		values = append(values, base.Add(time.Duration(i)*time.Minute).Format("20060102T150405Z"))
	}
	ics := vcalendar("BEGIN:VEVENT", "UID:flood", "SUMMARY:Flood",
		"DTSTART:20260101T090000Z", "DTEND:20260101T100000Z", "RRULE:FREQ=DAILY",
		"EXDATE:"+strings.Join(values, ","), "END:VEVENT")

	began := time.Now()
	report := importOK(t, calURL, tt.AccessToken, ics)
	require.Less(t, time.Since(began), 30*time.Second)
	require.Equal(t, 1, report.Rejected)
	require.Zero(t, report.Imported)
}

// Without WKST a rule's weeks start on Monday, and a biweekly weekend rule then
// pairs Saturday with the following Sunday. Both week starts are stored, so
// each rule imports and keeps the week start it was written with.
func TestImportKeepsTheWeekStartOfABiweeklyWeekendRule(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	report := importOK(t, calURL, tt.AccessToken, vcalendar(
		"BEGIN:VEVENT", "UID:custody-mo", "SUMMARY:Custody",
		"DTSTART:20260110T090000Z", "DTEND:20260110T100000Z",
		"RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", "END:VEVENT",
		"BEGIN:VEVENT", "UID:custody-su", "SUMMARY:Custody SU",
		"DTSTART:20260110T090000Z", "DTEND:20260110T100000Z",
		"RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU;WKST=SU", "END:VEVENT",
	))
	require.Zero(t, report.Skipped)
	require.Equal(t, 2, report.Imported, "the rule without WKST (Monday) and the one with WKST=SU")
}

// DURATION is the other way to say how long an event is.
func TestImportKeepsDuration(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	report := importOK(t, calURL, tt.AccessToken, vcalendar(
		"BEGIN:VEVENT", "UID:three-hours", "SUMMARY:Three hours",
		`DTSTART;X-NOTE="a:b";TZID="America/New_York":20260601T100000`, "DURATION:PT3H", "END:VEVENT",
	))
	require.Equal(t, 1, report.Imported)

	var evts []struct {
		StartAt  time.Time `json:"startAt"`
		EndAt    time.Time `json:"endAt"`
		Timezone string    `json:"timezone"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-01&end=2026-06-01&tz=UTC",
		tt.AccessToken, nil, &evts)
	require.Len(t, evts, 1)
	require.Equal(t, time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC), evts[0].StartAt.UTC(),
		"the quoted TZID is read as the zone")
	require.Equal(t, "America/New_York", evts[0].Timezone)
	require.Equal(t, 3*time.Hour, evts[0].EndAt.Sub(evts[0].StartAt))
}

// A changed occurrence an import writes shows in the activity feed like one
// made through the API.
func TestImportedChangedOccurrenceIsLogged(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	report := importOK(t, calURL, tt.AccessToken, vcalendar(
		"BEGIN:VEVENT", "UID:series-log", "SUMMARY:Series",
		"DTSTART:20260601T090000Z", "DTEND:20260601T100000Z", "RRULE:FREQ=DAILY;COUNT=5", "END:VEVENT",
		"BEGIN:VEVENT", "UID:series-log", "SUMMARY:Changed occurrence",
		"RECURRENCE-ID:20260602T090000Z",
		"DTSTART:20260602T150000Z", "DTEND:20260602T160000Z", "END:VEVENT",
	))
	require.Equal(t, 2, report.Imported)

	var feed struct {
		Items []struct {
			Action  string `json:"action"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity", tt.AccessToken, nil, &feed)
	found := false
	for _, item := range feed.Items {
		if item.Summary == "Changed occurrence" {
			found = true
			require.Equal(t, "calendar.event.updated", item.Action)
		}
	}
	require.True(t, found, "the imported changed occurrence is missing from the feed: %+v", feed.Items)
}

// iCalendar names are case-insensitive, component names included, so a file
// whose BEGIN/END values are in any case imports like any other.
func TestImportReadsComponentNamesInAnyCase(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	file := vcalendar(
		"BEGIN:VEVENT", "UID:case@example.com", "SUMMARY:Fine",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
		"BEGIN:VALARM", "TRIGGER:-PT15M", "DESCRIPTION:not the memo", "END:VALARM",
		"END:VEVENT",
	)
	mixed := strings.NewReplacer("BEGIN:VCALENDAR", "Begin:VCalendar", "BEGIN:VEVENT", "begin:VEvent",
		"END:VEVENT", "End:vevent", "BEGIN:VALARM", "BEGIN:valarm", "END:VALARM", "end:VAlarm").Replace(file)

	for name, ics := range map[string]string{"lower case": strings.ToLower(file), "mixed case": mixed} {
		t.Run(name, func(t *testing.T) {
			calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Case "+name)
			require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

			var evts []struct {
				StartAt time.Time `json:"startAt"`
				Memo    string    `json:"memo"`
			}
			helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-01&end=2026-06-01&tz=UTC",
				tt.AccessToken, nil, &evts)
			require.Len(t, evts, 1)
			require.Equal(t, time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), evts[0].StartAt.UTC())
			require.Empty(t, evts[0].Memo, "the alarm's DESCRIPTION stays inside the alarm")
		})
	}
}

// A changed occurrence an import moves past the end of its series still
// lists: the series' end boundary widens to cover it, as it does for one
// moved through the API.
func TestImportedOccurrenceMovedPastTheSeriesEndStillLists(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	report := importOK(t, calURL, tt.AccessToken, vcalendar(
		"BEGIN:VEVENT", "UID:short-series", "SUMMARY:Short series",
		"DTSTART:20260601T090000Z", "DTEND:20260601T100000Z", "RRULE:FREQ=DAILY;COUNT=3", "END:VEVENT",
		"BEGIN:VEVENT", "UID:short-series", "SUMMARY:Moved far out",
		"RECURRENCE-ID:20260602T090000Z",
		"DTSTART:20260620T090000Z", "DTEND:20260620T100000Z", "END:VEVENT",
	))
	require.Equal(t, 2, report.Imported)

	var evts []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-20&end=2026-06-20&tz=UTC",
		tt.AccessToken, nil, &evts)
	require.Len(t, evts, 1)
	require.Equal(t, "Moved far out", evts[0].Title)
}
