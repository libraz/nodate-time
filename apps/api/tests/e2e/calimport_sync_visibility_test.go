package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A sync run by a member neither rewrites nor deletes a synced event that
// member cannot see.
func TestSyncLeavesAnInvisibleEventAlone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	importer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, importer, "editor")
	require.Equal(t, http.StatusOK, promote(t, calURL, owner.AccessToken, importer, "manager"))

	ctx := t.Context()
	q := generated.New(testDB)
	var workspaceID, ownerID, importerID uint32
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT c.workspace_id, u.id FROM calendars c
		JOIN users u ON u.public_id = UUID_TO_BIN(?) WHERE c.public_id = UUID_TO_BIN(?)`,
		owner.UserID, owner.CalendarID).Scan(&workspaceID, &ownerID))
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT id FROM users WHERE public_id = UUID_TO_BIN(?)`,
		importer.UserID).Scan(&importerID))
	pub := uuid.MustParse(owner.CalendarID)
	cal, err := q.GetCalendarByPublicID(ctx, generated.GetCalendarByPublicIDParams{WorkspaceID: workspaceID, PublicID: pub[:]})
	require.NoError(t, err)

	start := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	event := calimport.Event{
		SourceUID: "sealed-sync@example.test", Title: "Sealed", Start: start, End: start.Add(time.Hour),
		Timezone: "UTC", Visibility: generated.CalendarEventsVisibilityConfidential,
		ShowAs: generated.CalendarEventsShowAsBusy,
	}
	const source = "sync-visibility-test"
	made := calimport.Run(ctx, testDB, workspaceID, cal, ownerID, []calimport.Event{event}, source)
	require.Equal(t, 1, made.Imported)
	var rowID uint32
	require.NoError(t, testDB.QueryRowContext(ctx,
		`SELECT id FROM calendar_events WHERE calendar_id = ? AND source_uid = ?`, cal.ID, event.SourceUID).Scan(&rowID))

	event.Title = "Overwritten"
	res := calimport.Sync(ctx, testDB, workspaceID, cal, importerID, []calimport.Event{event},
		map[string]bool{event.SourceUID: true}, nil, source)
	require.Zero(t, res.Updated)
	require.Equal(t, 1, res.Kept)

	res = calimport.Sync(ctx, testDB, workspaceID, cal, importerID, nil, map[string]bool{}, []uint32{rowID}, source)
	require.Zero(t, res.Deleted)

	var title string
	var enabled bool
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT title, enabled FROM calendar_events WHERE id = ?`, rowID).
		Scan(&title, &enabled))
	require.Equal(t, "Sealed", title)
	require.True(t, enabled)
}
