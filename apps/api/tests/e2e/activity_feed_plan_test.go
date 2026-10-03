package e2e

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// handlerReads sums the row reads MySQL reports for the current connection,
// which is what a page of the feed costs regardless of how many rows it keeps.
func handlerReads(t *testing.T, conn *sql.Conn) int64 {
	t.Helper()
	rows, err := conn.QueryContext(testCtx(),
		`SHOW SESSION STATUS WHERE Variable_name IN
		 ('Handler_read_first','Handler_read_last','Handler_read_next','Handler_read_prev',
		  'Handler_read_rnd','Handler_read_rnd_next')`)
	require.NoError(t, err)
	defer rows.Close()
	var total int64
	for rows.Next() {
		var name string
		var n int64
		require.NoError(t, rows.Scan(&name, &n))
		total += n
	}
	require.NoError(t, rows.Err())
	return total
}

// A page of the activity feed reads a number of index entries bounded by its
// limit, whether it is the first page or one after a cursor, and however much
// history the calendar has. The calendar's log sits among other calendars'
// rows in the same workspace, which is what a primary-key walk would have to
// read through and a sort would have to hold in full.
func TestActivityFeedPageReadsOnlyItsOwnPage(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	other := helpers.NewTenant(t, testServerURL)
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)

	const history = 800
	seedSubjectHistory(t, workspaceID, calendarID, uuid.NewString(), history)
	seedSubjectHistory(t, workspaceID, internalCalendarID(t, other.CalendarID), uuid.NewString(), history)
	analyze(t, "events")

	conn, err := testDB.Conn(testCtx())
	require.NoError(t, err)
	defer conn.Close()
	q := generated.New(conn)

	const limit = 50
	page := func(after uint64) ([]generated.ListEventsByCalendarRow, int64) {
		before := handlerReads(t, conn)
		rows, err := q.ListEventsByCalendar(testCtx(), generated.ListEventsByCalendarParams{
			WorkspaceID: workspaceID,
			CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
			AfterID:     after,
			Limit:       limit,
		})
		require.NoError(t, err)
		return rows, handlerReads(t, conn) - before
	}

	first, reads := page(0)
	require.Len(t, first, limit)
	require.LessOrEqual(t, reads, int64(limit*2),
		"the first page read %d entries for %d rows kept", reads, limit)

	// Resume from deep in the history: the rows after the cursor are still
	// hundreds, and the page must stop at its limit rather than collect them.
	cursor := first[limit-1].ID
	second, reads := page(cursor)
	require.Len(t, second, limit)
	require.Less(t, second[0].ID, cursor, "a page after a cursor continues strictly older")
	require.LessOrEqual(t, reads, int64(limit*2),
		"the page after a cursor read %d entries for %d rows kept", reads, limit)

	for i := 1; i < len(first); i++ {
		require.Greater(t, first[i-1].ID, first[i].ID, "row %d out of order", i)
	}
}
