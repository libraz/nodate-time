package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// A changed occurrence is a row of its own, so it carries its own participant
// rows; without them it would drop out of the attendees' availability.
func TestCalimportWritesAttendeesOnSeriesAndChangedOccurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, nil)
	code, out, err := s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)

	userID := func(email string) uint32 {
		var id uint32
		require.NoError(t, testDB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id))
		return id
	}
	var calID uint32
	require.NoError(t, testDB.QueryRow(`SELECT id FROM calendars WHERE name = ?`, s.calName).Scan(&calID))
	cal, err := generated.New(testDB).GetCalendarByID(context.Background(), calID)
	require.NoError(t, err)

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	people := []attendees.Person{{UserID: userID(s.hanako)}, {UserID: userID(s.mgr)}}
	base := calimport.Event{
		SourceUID: "att-series", Visibility: generated.CalendarEventsVisibilityDefault,
		ShowAs: generated.CalendarEventsShowAsBusy, Attendees: people,
	}
	head, changed := base, base
	head.Title, head.Start, head.End, head.RRule = "weekly", start, start.Add(time.Hour), "FREQ=WEEKLY;COUNT=3"
	week := 7 * 24 * time.Hour
	changed.Title, changed.Start, changed.End = "moved", start.Add(week+time.Hour), start.Add(week+2*time.Hour)
	changed.RecurrenceID, changed.HasRecurrenceID = start.Add(week), true
	res := calimport.Run(context.Background(), testDB, cal.WorkspaceID, cal, userID(s.importer),
		[]calimport.Event{head, changed}, "test")
	require.Equal(t, calimport.Result{Imported: 2}, res)

	var series, override int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM calendar_event_attendees a JOIN calendar_events e ON e.id = a.event_id
		 WHERE e.calendar_id = ? AND e.source_uid = 'att-series' AND a.enabled = TRUE`, calID).Scan(&series))
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM calendar_event_attendees a JOIN calendar_events e ON e.id = a.event_id
		 JOIN calendar_events p ON p.id = e.recurrence_parent_id
		 WHERE p.calendar_id = ? AND p.source_uid = 'att-series' AND a.enabled = TRUE`, calID).Scan(&override))
	require.Equal(t, 2, series)
	require.Equal(t, 2, override)
}
