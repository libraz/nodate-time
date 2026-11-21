package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// exportRowCap mirrors the number of entries one export may hold.
const exportRowCap = 20000

// exportFillCalendar writes n dated single events straight into a calendar,
// all starting at the same instant, and removes them when the test ends.
func exportFillCalendar(t *testing.T, calendarPublicID string, n int) {
	t.Helper()
	_, err := testDB.ExecContext(t.Context(), `INSERT INTO calendar_events
		(public_id, workspace_id, calendar_id, kind, visibility, title, all_day, start_at, end_at, timezone,
		 owner_user_id, created_by_user_id)
		SELECT UNHEX(REPLACE(UUID(), '-', '')), c.workspace_id, c.id, 'event', 'default', 'Filler', FALSE,
		       '2026-01-05 10:00:00', '2026-01-05 11:00:00', 'UTC', cm.user_id, cm.user_id
		FROM calendars c
		JOIN calendar_members cm ON cm.calendar_id = c.id AND cm.role = 'owner'
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) a
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) b
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) e
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) f
		WHERE c.public_id = UUID_TO_BIN(?) AND a.n + 10*b.n + 100*d.n + 1000*e.n + 10000*f.n < ?`,
		calendarPublicID, n)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DELETE e FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
			WHERE c.public_id = UUID_TO_BIN(?) AND e.title = 'Filler'`, calendarPublicID)
	})
}

// A range that holds more entries than one export may carry is refused, with
// the code that says so; it is never cut short with a flag in a header. A
// range that fits the same calendar is exported whole.
func TestExportOverTheRangeIsRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	exportFillCalendar(t, owner.CalendarID, exportRowCap+1)

	var later struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "Outside the pile", "allDay": false,
		"startAt": "2027-03-01T10:00:00Z", "endAt": "2027-03-01T11:00:00Z",
	}, &later)

	for _, query := range []string{
		"format=ics",
		"format=csv&from=2026-01-01&to=2026-01-31&tz=UTC",
	} {
		status, raw, header := helpers.DoJSONFull(t, http.MethodGet, calURL+"/export?"+query, owner.AccessToken, nil, nil)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", query, raw)
		require.Contains(t, string(raw), `"EXPORT.RANGE_TOO_LARGE"`, query)
		require.Empty(t, header.Get("X-Result-Truncated"))
	}

	for _, format := range []string{"ics", "csv"} {
		body, header := exportResponse(t, calURL, owner.AccessToken,
			"format="+format+"&from=2027-03-01&to=2027-03-01&tz=UTC")
		require.Equal(t, 1, strings.Count(body, "Outside the pile"), format)
		require.NotContains(t, body, "Filler", format)
		require.Empty(t, header.Get("X-Result-Truncated"), "%s: an export is whole or refused", format)
	}
}

// A calendar at exactly the cap is still exported whole.
func TestExportAtTheCapIsWhole(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	exportFillCalendar(t, owner.CalendarID, exportRowCap-1)

	// The tenant's calendar starts empty, so one more event is the cap.
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "Last one", "allDay": false,
		"startAt": "2026-01-06T10:00:00Z", "endAt": "2026-01-06T11:00:00Z",
	}, nil)

	body, _ := exportResponse(t, calURL, owner.AccessToken, "format=ics")
	require.Equal(t, exportRowCap, strings.Count(body, "BEGIN:VEVENT"))
	require.Contains(t, body, "SUMMARY:Last one")
}
