package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestOpeningAnEventReadsItsThreadWithoutSortingIt checks the plan.
//
// Both tables carry an index on (workspace_id, event_id, ordering column). A
// query naming only the event cannot enter it -- a composite index is no use
// from its second column -- and lands instead on the single-column index the
// foreign key brings with it. That still finds the rows, so the row count
// looks fine; what it cannot do is deliver them in order, and the sort it
// falls back to is over the whole matched set on every event opened.
//
// Naming the workspace as well reaches the composite index, and the ordering
// comes free with the lookup.
func TestOpeningAnEventReadsItsThreadWithoutSortingIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)
	tenant := helpers.NewTenant(t, srv.BaseURL)
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calURL := srv.BaseURL + "/calendars/" + tenant.CalendarID

	// Spread the rows over several events. A table where every row belongs to
	// the event being asked about would make a full scan the right plan.
	const events, perEvent = 10, 60
	eventIDs := make([]uint32, 0, events)
	for i := range events {
		var created struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", tenant.AccessToken,
			map[string]any{
				"title":   fmt.Sprintf("索引 %d", i),
				"allDay":  false,
				"startAt": fmt.Sprintf("2026-11-%02dT10:00:00+09:00", i+1),
				"endAt":   fmt.Sprintf("2026-11-%02dT11:00:00+09:00", i+1),
			}, &created)
		eventIDs = append(eventIDs, internalEventID(t, created.ID))
	}
	seedEventChildRows(t, workspaceID, tenant.UserID, eventIDs, perEvent)

	// The thread is read from its newest end, which the index serves by being
	// walked backwards -- it must not fall back to sorting what it read.
	const comments = `SELECT ec.id FROM calendar_event_comments ec
		WHERE ec.workspace_id = ? AND ec.event_id = ? AND ec.enabled = TRUE AND ec.deleted_at IS NULL
		ORDER BY ec.created_at DESC, ec.id DESC LIMIT 51`
	key, rows, extra := explainOne(t, comments, workspaceID, eventIDs[0])
	require.Equal(t, "idx_calendar_event_comments_event", key,
		"an event's comments must come from the index that also holds their order")
	require.NotContains(t, extra, "filesort",
		"the thread is being sorted after the fact: %s", extra)
	require.Less(t, rows, int64(events*perEvent/2),
		"the plan still reads much of the table: %d rows for one event's comments", rows)

	const checklist = `SELECT id FROM calendar_event_checklist_items
		WHERE workspace_id = ? AND event_id = ? AND enabled = TRUE
		ORDER BY sort_weight, id LIMIT 500`
	key, rows, extra = explainOne(t, checklist, workspaceID, eventIDs[0])
	require.Equal(t, "idx_calendar_event_checklist_items_event", key,
		"a checklist must come from the index that also holds its order")
	require.NotContains(t, extra, "filesort",
		"the checklist is being sorted after the fact: %s", extra)
	require.Less(t, rows, int64(events*perEvent/2),
		"the plan still reads much of the table: %d rows for one event's checklist", rows)
}

// internalEventID resolves a public event id to the internal one the child
// tables point at.
func internalEventID(t *testing.T, publicID string) uint32 {
	t.Helper()
	parsed, err := uuid.Parse(publicID)
	require.NoError(t, err)
	var id uint32
	require.NoError(t, testDB.QueryRow(
		`SELECT id FROM calendar_events WHERE public_id = ?`, parsed[:]).Scan(&id))
	return id
}

// seedEventChildRows fills the two tables an event detail reads.
func seedEventChildRows(t *testing.T, workspaceID uint32, authorPublicID string, eventIDs []uint32, perEvent int) {
	t.Helper()
	parsed, err := uuid.Parse(authorPublicID)
	require.NoError(t, err)
	var authorID uint32
	require.NoError(t, testDB.QueryRow(
		`SELECT id FROM users WHERE public_id = ?`, parsed[:]).Scan(&authorID))

	comment, err := testDB.Prepare(
		`INSERT INTO calendar_event_comments (public_id, workspace_id, event_id, author_id, body)
		 VALUES (?, ?, ?, ?, 'seed')`)
	require.NoError(t, err)
	defer comment.Close()

	item, err := testDB.Prepare(
		`INSERT INTO calendar_event_checklist_items (public_id, workspace_id, event_id, created_by_user_id, title)
		 VALUES (?, ?, ?, ?, 'seed')`)
	require.NoError(t, err)
	defer item.Close()

	for _, eventID := range eventIDs {
		for range perEvent {
			c := uuid.New()
			_, err := comment.Exec(c[:], workspaceID, eventID, authorID)
			require.NoError(t, err)
			i := uuid.New()
			_, err = item.Exec(i[:], workspaceID, eventID, authorID)
			require.NoError(t, err)
		}
	}
	// The optimiser weighs the plan against its statistics, which are only
	// refreshed as a side effect of enough writes; asking for them makes the
	// choice reflect what was just inserted.
	_, err = testDB.Exec(`ANALYZE TABLE calendar_event_comments, calendar_event_checklist_items`)
	require.NoError(t, err)
}
