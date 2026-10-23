package e2e

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The events table is shared with writers that do not know this product lifts
// `$.id` out of the payload. Whatever they put there must still insert: the
// generated subject column yields a UUID or NULL, never an error that rolls
// back the other writer's state change.
func TestEventsAcceptAnyPayloadIDWithoutBreakingTheSubjectColumn(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)
	tenant := helpers.NewTenant(t, srv.BaseURL)
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)

	valid := uuid.NewString()
	cases := []struct {
		name    string
		payload string
		want    sql.NullString
	}{
		{"uuid", `{"id": "` + valid + `"}`, sql.NullString{String: valid, Valid: true}},
		{"longer than the column", `{"id": "` + strings.Repeat("a", 200) + `"}`, sql.NullString{}},
		{"not latin1", `{"id": "` + strings.Repeat("日", 36) + `"}`, sql.NullString{}},
		{"not a uuid", `{"id": "` + strings.Repeat("z", 36) + `"}`, sql.NullString{}},
		{"nested object", `{"id": {"task": 7, "pad": "` + strings.Repeat("x", 40) + `"}}`, sql.NullString{}},
		{"no id", `{"other": 1}`, sql.NullString{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publicID := uuid.New()
			_, err := testDB.Exec(
				`INSERT INTO events (public_id, workspace_id, calendar_id, type, payload_json, occurred_at)
				 VALUES (?, ?, ?, 'event.updated', ?, NOW(3))`,
				publicID[:], workspaceID, calendarID, tc.payload)
			require.NoError(t, err, "a payload the core DDL accepts must insert")

			var got sql.NullString
			require.NoError(t, testDB.QueryRow(
				`SELECT subject_public_id FROM events WHERE public_id = ?`, publicID[:]).Scan(&got))
			require.Equal(t, tc.want, got)
		})
	}
}
