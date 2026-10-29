package events

import (
	"database/sql"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

func TestMapEventReportsTaskOwnershipAsABoolean(t *testing.T) {
	owned := generated.CalendarEvent{TaskID: sql.NullInt32{Int32: 7, Valid: true}}
	if !mapEvent(owned, nil).TaskOwned {
		t.Error("an event with a task_id must read as taskOwned")
	}
	if mapEvent(generated.CalendarEvent{}, nil).TaskOwned {
		t.Error("an event with no task_id must not read as taskOwned")
	}
}
