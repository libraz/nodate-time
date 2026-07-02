package eventexpand

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

type fakeExceptionLoader struct {
	rows []generated.Event
}

func (f fakeExceptionLoader) ListRecurrenceExceptionsByParent(context.Context, sql.NullInt32) ([]generated.Event, error) {
	return f.rows, nil
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func dailyRule(t *testing.T) *json.RawMessage {
	t.Helper()
	raw := json.RawMessage(`{"freq":"daily","interval":1}`)
	return &raw
}

func TestExpandRecurringEventAppliesExceptions(t *testing.T) {
	master := generated.Event{
		ID:             1,
		Title:          "master",
		StartAt:        mustTime(t, "2026-04-01T10:00:00Z"),
		EndAt:          mustTime(t, "2026-04-01T11:00:00Z"),
		Timezone:       "UTC",
		RecurrenceRule: dailyRule(t),
	}
	cancelled := generated.Event{
		ID:                      2,
		Title:                   "cancelled",
		StartAt:                 mustTime(t, "2026-04-02T10:00:00Z"),
		EndAt:                   mustTime(t, "2026-04-02T11:00:00Z"),
		RecurrenceOriginalStart: sql.NullTime{Time: mustTime(t, "2026-04-02T10:00:00Z"), Valid: true},
		RecurrenceCancelled:     true,
	}
	moved := generated.Event{
		ID:                      3,
		Title:                   "moved",
		StartAt:                 mustTime(t, "2026-04-04T12:00:00Z"),
		EndAt:                   mustTime(t, "2026-04-04T13:00:00Z"),
		RecurrenceOriginalStart: sql.NullTime{Time: mustTime(t, "2026-04-03T10:00:00Z"), Valid: true},
	}

	instances := ExpandRecurringEvent(
		context.Background(),
		fakeExceptionLoader{rows: []generated.Event{cancelled, moved}},
		master,
		mustTime(t, "2026-04-01T00:00:00Z"),
		mustTime(t, "2026-04-05T00:00:00Z"),
	)

	if len(instances) != 3 {
		t.Fatalf("got %d instances, want 3", len(instances))
	}
	if instances[0].Event.Title != "master" || instances[0].IsException {
		t.Fatalf("first instance = %#v, want master occurrence", instances[0])
	}
	if instances[1].Event.Title != "moved" || !instances[1].IsException {
		t.Fatalf("second instance = %#v, want moved exception", instances[1])
	}
	if got := instances[1].OriginalStart.Format(time.RFC3339); got != "2026-04-03T10:00:00Z" {
		t.Fatalf("moved original start = %s", got)
	}
	if instances[2].Event.Title != "master" || !instances[2].Occurrence.StartAt.Equal(mustTime(t, "2026-04-04T10:00:00Z")) {
		t.Fatalf("third instance = %#v, want fourth-day master occurrence", instances[2])
	}
}
