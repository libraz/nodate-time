package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A changed occurrence that leaves out SUMMARY inherits the series' title,
// so it lands with a title the events API accepts on an unchanged save.
func TestImportedChangedOccurrenceWithoutATitleTakesTheSeriesTitle(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Untitled override")

	ics := wrap(
		vevent("UID:untitled@example.com", "SUMMARY:Standup",
			"DTSTART:20260601T100000Z", "DTEND:20260601T101500Z", "RRULE:FREQ=DAILY;COUNT=3") +
			vevent("UID:untitled@example.com", "RECURRENCE-ID:20260602T100000Z",
				"DTSTART:20260602T140000Z", "DTEND:20260602T141500Z"))
	require.Equal(t, importReport{Imported: 2}, importOK(t, calURL, tt.AccessToken, ics))

	var moved []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-06-02&end=2026-06-02&tz=UTC",
		tt.AccessToken, nil, &moved)
	require.Len(t, moved, 1)
	require.Equal(t, "2026-06-02T14:00:00Z", moved[0].StartAt)
	require.Equal(t, "Standup", moved[0].Title)

	status, raw := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+moved[0].ID+"?scope=this",
		tt.AccessToken, eventBody(moved[0].Title, "2026-06-02T14:00:00Z", "2026-06-02T14:15:00Z"))
	require.Equal(t, http.StatusOK, status, "saving the imported occurrence unchanged: %s", raw)
	deleteEverything(t, calURL, tt.AccessToken, listHostile(t, calURL, tt.AccessToken))
}

// A changed occurrence naming an instant the series never produces, or one
// the file itself cancels, could never be listed. It is skipped and counted
// as skipped, never stored and reported as imported.
func TestImportedChangedOccurrenceTheSeriesDoesNotHaveIsSkipped(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, recurrenceID string }{
		{"off the rule's grid", "RECURRENCE-ID:20260602T113000Z"},
		{"past the series' count", "RECURRENCE-ID:20260610T100000Z"},
		{"cancelled by EXDATE", "RECURRENCE-ID:20260603T100000Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Phantom "+c.name)
			ics := wrap(
				vevent("UID:phantom@example.com", "SUMMARY:Standup",
					"DTSTART:20260601T100000Z", "DTEND:20260601T101500Z",
					"RRULE:FREQ=DAILY;COUNT=5", "EXDATE:20260603T100000Z") +
					vevent("UID:phantom@example.com", "SUMMARY:Phantom", c.recurrenceID,
						"DTSTART:20260604T160000Z", "DTEND:20260604T161500Z"))
			res := importOK(t, calURL, tt.AccessToken, ics)
			require.Equal(t, importReport{Imported: 1, Skipped: 1}, res)

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 4, "five days less the cancelled one, and nothing else")
			for _, e := range listed {
				require.NotEqual(t, "Phantom", e.Title)
			}
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}
