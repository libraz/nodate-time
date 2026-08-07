package e2e

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestOneEventsHistoryIsFoundByIndexNotByReadingTheLog checks the plan, not
// the clock.
//
// Opening an event shows its own history: every log row whose payload points
// at it. Asked of the JSON directly that question is not indexable, so it
// read every row the calendar had ever produced -- and LIMIT bounds the
// result, not the scan. The cost of the most frequent action in the product
// grew with how much the calendar had been used, which is exactly backwards.
//
// A timing assertion would be a flake generator on a laptop and a CI runner
// alike. The plan is the durable statement: if the optimiser stops reaching
// the index, this fails whatever the machine happens to be doing. The log is
// filled first because an empty table makes any plan look equally good.
func TestOneEventsHistoryIsFoundByIndexNotByReadingTheLog(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)
	tenant := helpers.NewTenant(t, srv.BaseURL)

	queries := generated.New(testDB)
	workspaceID := helpers.TestWorkspace(queries).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)

	subject := uuid.NewString()
	seedSubjectHistory(t, workspaceID, calendarID, subject, 2000)

	const query = `SELECT e.id FROM events e
		WHERE e.workspace_id = ? AND e.calendar_id = ? AND e.subject_public_id = ?
		ORDER BY e.id DESC LIMIT 200`

	keyUsed, rowsRead, _ := explainOne(t, query, workspaceID, calendarID, subject)
	require.True(t, strings.Contains(keyUsed, "subject"),
		"the subject history must be found through an index, got key=%q", keyUsed)
	require.Less(t, rowsRead, int64(1000),
		"the plan still reads most of the log: %d rows for one entity's history", rowsRead)
}

// internalCalendarID resolves a public calendar id to the internal one the
// event log records.
func internalCalendarID(t *testing.T, publicID string) uint32 {
	t.Helper()
	parsed, err := uuid.Parse(publicID)
	require.NoError(t, err)
	var id uint32
	require.NoError(t, testDB.QueryRow(
		`SELECT id FROM calendars WHERE public_id = ?`, parsed[:]).Scan(&id))
	return id
}

// seedSubjectHistory fills one calendar's log so the optimiser has something
// to weigh. Most rows belong to other subjects: a log where every row matches
// would make a full scan the right answer.
func seedSubjectHistory(t *testing.T, workspaceID, calendarID uint32, subject string, total int) {
	t.Helper()
	stmt, err := testDB.Prepare(
		`INSERT INTO events (public_id, workspace_id, calendar_id, type, payload_json, occurred_at)
		 VALUES (?, ?, ?, 'event.updated', JSON_OBJECT('id', ?), NOW(3))`)
	require.NoError(t, err)
	defer stmt.Close()

	for i := 0; i < total; i++ {
		id := uuid.NewString()
		if i%100 == 0 {
			id = subject
		}
		publicID := uuid.New()
		_, err := stmt.Exec(publicID[:], workspaceID, calendarID, id)
		require.NoError(t, err)
	}
}

// explainOne reports the index the optimiser chose, how many rows it expects
// to examine, and what it says it will do beyond the lookup -- a sort it has
// to perform itself being the part that does not show up in the row count.
func explainOne(t *testing.T, query string, args ...any) (string, int64, string) {
	t.Helper()
	rows, err := testDB.Query("EXPLAIN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()

	cols, err := rows.Columns()
	require.NoError(t, err)

	var key, extra string
	var examined int64
	found := false
	for rows.Next() {
		cells := make([]any, len(cols))
		holders := make([]sql.NullString, len(cols))
		for i := range cells {
			cells[i] = &holders[i]
		}
		require.NoError(t, rows.Scan(cells...))
		for i, name := range cols {
			switch name {
			case "key":
				key = holders[i].String
			case "rows":
				var n int64
				_, _ = fmt.Sscanf(holders[i].String, "%d", &n)
				examined = n
			case "Extra":
				extra = holders[i].String
			}
		}
		found = true
	}
	require.NoError(t, rows.Err())
	require.True(t, found, "EXPLAIN returned no rows")
	return key, examined, extra
}
