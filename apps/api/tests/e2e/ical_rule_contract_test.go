package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// One rule contract holds for the API and for import alike: a rule with both
// end conditions, or one whose first occurrence falls past its own until, is
// refused on create, on a series edit, and in a file.
func TestRuleContractIsTheSameForTheAPIAndForImport(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	occ := createWeeklyFriday(t, calURL, tt.AccessToken)
	seriesURL := calURL + "/events/" + strings.Split(occ[0].ID, "_")[0]

	// The series starts on Friday 2026-04-03.
	refused := map[string]map[string]any{
		"count and until": {"freq": "weekly", "interval": 1, "byDay": []string{"FR"},
			"count": 3, "until": "2026-12-31"},
		"first Monday after the until": {"freq": "weekly", "interval": 1, "byDay": []string{"MO"},
			"until": "2026-04-05"},
		"day of month already past, until this month": {"freq": "monthly", "interval": 1,
			"byMonthDay": 2, "until": "2026-04-30"},
	}
	for name, rule := range refused {
		create := map[string]any{
			"title": "Refused", "allDay": false, "timezone": "Asia/Tokyo",
			"startAt": "2026-04-03T15:00:00+09:00", "endAt": "2026-04-03T16:00:00+09:00",
			"recurrenceRule": rule,
		}
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken, create)
		require.Equal(t, http.StatusBadRequest, status, "create, %s: %s", name, raw)

		update := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00")
		update["recurrenceRule"] = rule
		status, raw = helpers.DoJSONStatus(t, http.MethodPut, seriesURL, tt.AccessToken, update)
		require.Equal(t, http.StatusBadRequest, status, "update, %s: %s", name, raw)
	}

	for name, rrule := range map[string]string{
		"count and until":              "FREQ=DAILY;COUNT=3;UNTIL=20260701T000000Z",
		"first Monday after the until": "FREQ=WEEKLY;BYDAY=MO;UNTIL=20260607T235959Z",
	} {
		importURL := hostileCalendar(t, tt, "Import "+name)
		ics := wrap(vevent("UID:contract@example.com", "SUMMARY:Rule",
			"DTSTART:20260603T100000Z", "DTEND:20260603T110000Z", "RRULE:"+rrule)) // a Wednesday
		require.Equal(t, importReport{Skipped: 1}, importOK(t, importURL, tt.AccessToken, ics), name)
		require.Empty(t, listHostile(t, importURL, tt.AccessToken), name)
	}
}

// BYDAY=2MO is how mainstream clients write "the second Monday", and it has
// to land on the same days as the BYSETPOS spelling of the same rule.
func TestImportTakesAnOrdinalWeekdayLikeItsSetPositionForm(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Second Monday")

	ics := wrap(
		vevent("UID:ordinal@example.com", "SUMMARY:Ordinal",
			"DTSTART:20260608T100000Z", "DTEND:20260608T110000Z", "RRULE:FREQ=MONTHLY;BYDAY=2MO") +
			vevent("UID:setpos@example.com", "SUMMARY:Setpos",
				"DTSTART:20260608T100000Z", "DTEND:20260608T110000Z", "RRULE:FREQ=MONTHLY;BYDAY=MO;BYSETPOS=2") +
			vevent("UID:last@example.com", "SUMMARY:Last Friday",
				"DTSTART:20260626T100000Z", "DTEND:20260626T110000Z", "RRULE:FREQ=MONTHLY;BYDAY=-1FR"))
	require.Equal(t, importReport{Imported: 2, Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics),
		"a negative ordinal is still one the expander cannot reproduce")

	var listed []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-01&end=2026-10-31&tz=UTC",
		tt.AccessToken, nil, &listed)
	byTitle := map[string][]string{}
	for _, e := range listed {
		byTitle[e.Title] = append(byTitle[e.Title], e.StartAt)
	}
	require.Equal(t, []string{
		"2026-06-08T10:00:00Z", "2026-07-13T10:00:00Z", "2026-08-10T10:00:00Z",
		"2026-09-14T10:00:00Z", "2026-10-12T10:00:00Z",
	}, byTitle["Ordinal"])
	require.Equal(t, byTitle["Setpos"], byTitle["Ordinal"])
}

// A series on the 31st runs on the last day of every shorter month here. It
// leaves in the RFC form that does the same, so another client agrees on the
// dates, and comes back in as the same series. The plain RFC form, which
// skips short months, is refused rather than imported onto other dates.
func TestMonthEndSeriesCrossesTheICalBoundaryOnTheSameDays(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + tt.CalendarID
	helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", tt.AccessToken, map[string]any{
		"title": "Month end", "allDay": false, "timezone": "UTC",
		"startAt": "2026-01-31T10:00:00Z", "endAt": "2026-01-31T11:00:00Z",
		"recurrenceRule": map[string]any{"freq": "monthly", "interval": 1},
	}, nil)

	ics := fetchICS(t, sourceURL, tt.AccessToken)
	require.Contains(t, ics, "RRULE:FREQ=MONTHLY;BYMONTHDAY=-1;WKST=SU")

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Month end copy")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken, map[string]any{"ics": ics}, &res)
	require.Equal(t, 1, res.Imported)

	const window = "start=2026-01-01&end=2026-06-30&tz=UTC"
	want := listedStarts(t, sourceURL, tt.AccessToken, window)
	require.Equal(t, []string{
		"Month end@2026-01-31T10:00:00Z", "Month end@2026-02-28T10:00:00Z", "Month end@2026-03-31T10:00:00Z",
		"Month end@2026-04-30T10:00:00Z", "Month end@2026-05-31T10:00:00Z", "Month end@2026-06-30T10:00:00Z",
	}, want)
	require.Equal(t, want, listedStarts(t, targetURL, tt.AccessToken, window))

	plainURL := hostileCalendar(t, tt, "Plain 31st")
	plain := wrap(vevent("UID:plain31@example.com", "SUMMARY:Plain",
		"DTSTART:20260131T100000Z", "DTEND:20260131T110000Z", "RRULE:FREQ=MONTHLY;BYMONTHDAY=31"))
	require.Equal(t, importReport{Skipped: 1}, importOK(t, plainURL, tt.AccessToken, plain))
}
