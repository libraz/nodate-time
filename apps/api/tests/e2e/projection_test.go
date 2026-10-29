package e2e

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// projectOntoTask links an event to a task the way another layer's projection
// engine would, through the session flag the projection guard honours, and
// unlinks it when the test ends so no projected row outlives it.
func projectOntoTask(t *testing.T, eventPublicID string) {
	t.Helper()
	conn, err := testDB.Conn(testCtx())
	require.NoError(t, err)
	defer conn.Close()
	link := func(taskID any, role any) {
		_, err := conn.ExecContext(testCtx(), "SET @nf_item_projection_engine = 1")
		require.NoError(t, err)
		_, err = conn.ExecContext(testCtx(),
			"UPDATE calendar_events SET task_id = ?, task_role = ? WHERE public_id = UUID_TO_BIN(?)",
			taskID, role, eventPublicID)
		require.NoError(t, err)
		_, err = conn.ExecContext(testCtx(), "SET @nf_item_projection_engine = NULL")
		require.NoError(t, err)
	}
	link(2_000_000_000+rand.Int32N(100_000_000), "scheduled")
	t.Cleanup(func() {
		conn, err := testDB.Conn(testCtx())
		require.NoError(t, err)
		defer conn.Close()
		_, _ = conn.ExecContext(testCtx(), "SET @nf_item_projection_engine = 1")
		_, _ = conn.ExecContext(testCtx(),
			"UPDATE calendar_events SET task_id = NULL, task_role = NULL WHERE public_id = UUID_TO_BIN(?)",
			eventPublicID)
		_, _ = conn.ExecContext(testCtx(), "SET @nf_item_projection_engine = NULL")
	})
}

// An event a task projects belongs to the task for its title, dates and
// existence (PROTOCOL.md obligation 3). Changing those here is refused with
// EVENT.TASK_OWNED rather than reaching the database guard and failing as a
// server error; the columns with no task-side counterpart stay editable.
func TestProjectionRefusesSyncedChangesAndAllowsTheRest(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	const start, end = "2026-06-01T09:00:00+09:00", "2026-06-01T10:00:00+09:00"
	var evt struct {
		ID string `json:"id"`
	}
	create := eventBody("Write the report", start, end)
	create["timezone"] = "Asia/Tokyo"
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, create, &evt)
	projectOntoTask(t, evt.ID)
	eventURL := calURL + "/events/" + evt.ID

	requireTaskOwned := func(status int, raw []byte, what string) {
		t.Helper()
		require.Equal(t, http.StatusConflict, status, "%s: %s", what, raw)
		var body struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, "EVENT.TASK_OWNED", body.Code, what)
	}

	for what, body := range map[string]map[string]any{
		"title": eventBody("Renamed", start, end),
		"start": eventBody("Write the report", "2026-06-01T08:00:00+09:00", end),
		"end":   eventBody("Write the report", start, "2026-06-01T11:00:00+09:00"),
	} {
		body["timezone"] = "Asia/Tokyo"
		status, raw := helpers.DoJSONStatus(t, http.MethodPut, eventURL, tt.AccessToken, body)
		requireTaskOwned(status, raw, what)
	}

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, eventURL, tt.AccessToken, nil)
	requireTaskOwned(status, raw, "delete")

	edit := eventBody("Write the report", start, end)
	edit["timezone"] = "Asia/Tokyo"
	edit["memo"] = "Outline first"
	edit["location"] = "Library"
	var updated struct {
		Title    string `json:"title"`
		Memo     string `json:"memo"`
		Location string `json:"location"`
	}
	helpers.DoJSON(t, http.MethodPut, eventURL, tt.AccessToken, edit, &updated)
	require.Equal(t, "Outline first", updated.Memo)
	require.Equal(t, "Library", updated.Location)
	require.Equal(t, "Write the report", updated.Title)

	var enabled bool
	require.NoError(t, testDB.QueryRow(
		"SELECT enabled FROM calendar_events WHERE public_id = UUID_TO_BIN(?)", evt.ID).Scan(&enabled))
	require.True(t, enabled, "the refused delete left the event in place")
}
