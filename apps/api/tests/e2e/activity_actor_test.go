package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func TestActivityDistinguishesSystemAndMissingActors(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calendarURL := testServerURL + "/calendars/" + owner.CalendarID
	var event struct{ ID string }
	helpers.DoJSON(t, http.MethodPost, calendarURL+"/events", owner.AccessToken,
		eventBody("Owner created this event", "2026-11-22T10:00:00+09:00", "2026-11-22T11:00:00+09:00"), &event)
	for _, entry := range []struct {
		summary string
		source  any
	}{
		{"System setup", "operator:calendar-setup-test"},
		{"Unknown historical actor", nil},
	} {
		id, err := uuid.NewV7()
		require.NoError(t, err)
		_, err = testDB.ExecContext(t.Context(), `INSERT INTO events
			(public_id, workspace_id, calendar_id, actor_system_source, subject_public_id, type, payload_json, occurred_at)
			SELECT ?, workspace_id, id, ?, UUID_TO_BIN(?), 'calendar.event.updated', JSON_OBJECT('eventId', ?, 'title', ?), UTC_TIMESTAMP(3)
			FROM calendars WHERE public_id = UUID_TO_BIN(?)`,
			id[:], entry.source, event.ID, event.ID, entry.summary, owner.CalendarID)
		require.NoError(t, err)
	}

	type item struct {
		Summary     string `json:"summary"`
		SystemActor bool   `json:"systemActor"`
		ActorID     string `json:"actorId"`
	}
	checkActors := func(t *testing.T, list []item) {
		t.Helper()
		items := make(map[string]item, len(list))
		for _, entry := range list {
			items[entry.Summary] = entry
		}
		require.True(t, items["System setup"].SystemActor)
		require.Empty(t, items["System setup"].ActorID)
		require.Contains(t, items, "Unknown historical actor")
		require.False(t, items["Unknown historical actor"].SystemActor)
		require.Empty(t, items["Unknown historical actor"].ActorID)
		require.Equal(t, owner.UserID, items["Owner created this event"].ActorID)
		require.False(t, items["Owner created this event"].SystemActor)
	}
	t.Run("activity", func(t *testing.T) {
		var feed struct{ Items []item }
		helpers.DoJSON(t, http.MethodGet, calendarURL+"/activity", owner.AccessToken, nil, &feed)
		checkActors(t, feed.Items)
	})
	t.Run("history", func(t *testing.T) {
		var history []item
		helpers.DoJSON(t, http.MethodGet, calendarURL+"/events/"+event.ID+"/history", owner.AccessToken, nil, &history)
		checkActors(t, history)
	})
}
