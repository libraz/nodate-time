package eventexpand

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

type fakeOverrideLoader struct {
	rows []generated.CalendarEvent
}

func (f fakeOverrideLoader) ListRecurrenceOverridesByParent(context.Context, sql.NullInt32) ([]generated.CalendarEvent, error) {
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

func nullTime(t *testing.T, value string) sql.NullTime {
	t.Helper()
	return sql.NullTime{Time: mustTime(t, value), Valid: true}
}

func dailyRule(t *testing.T) *json.RawMessage {
	t.Helper()
	raw := json.RawMessage(`{"freq":"daily","interval":1}`)
	return &raw
}

// exceptions renders the stored exclusion list the way a writer would.
func exceptions(t *testing.T, starts ...string) *json.RawMessage {
	t.Helper()
	var list recurrence.Exceptions
	for _, s := range starts {
		list = list.With(mustTime(t, s))
	}
	column, err := list.MarshalColumn()
	if err != nil {
		t.Fatal(err)
	}
	return column
}

func master(t *testing.T) generated.CalendarEvent {
	t.Helper()
	return generated.CalendarEvent{
		ID:             1,
		Title:          "master",
		StartAt:        nullTime(t, "2026-04-01T10:00:00Z"),
		EndAt:          nullTime(t, "2026-04-01T11:00:00Z"),
		Timezone:       "UTC",
		RecurrenceRule: dailyRule(t),
	}
}

// A cancelled occurrence is an entry in the parent's exclusion list, and a
// changed one is a row. This checks both departures at once, because the
// contract's rule is that neither substitutes for the other.
func TestExpandDropsCancellationsAndSubstitutesOverrides(t *testing.T) {
	m := master(t)
	m.RecurrenceExceptions = exceptions(t, "2026-04-02T10:00:00Z")

	moved := generated.CalendarEvent{
		ID:                      3,
		Title:                   "moved",
		StartAt:                 nullTime(t, "2026-04-04T12:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-04T13:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-03T10:00:00Z"),
	}

	instances := ExpandRecurringEvent(
		context.Background(),
		fakeOverrideLoader{rows: []generated.CalendarEvent{moved}},
		m,
		mustTime(t, "2026-04-01T00:00:00Z"),
		mustTime(t, "2026-04-05T00:00:00Z"),
	)

	if len(instances) != 3 {
		t.Fatalf("got %d instances, want 3", len(instances))
	}
	if instances[0].Event.Title != "master" || instances[0].IsOverride {
		t.Fatalf("first instance = %#v, want master occurrence", instances[0])
	}
	if instances[1].Event.Title != "moved" || !instances[1].IsOverride {
		t.Fatalf("second instance = %#v, want the override", instances[1])
	}
	if got := instances[1].OriginalStart.Format(time.RFC3339); got != "2026-04-03T10:00:00Z" {
		t.Fatalf("override original start = %s, want 2026-04-03T10:00:00Z", got)
	}
	if instances[2].Event.Title != "master" || !instances[2].Occurrence.StartAt.Equal(mustTime(t, "2026-04-04T10:00:00Z")) {
		t.Fatalf("third instance = %#v, want the fourth-day occurrence", instances[2])
	}
}

// An override that moved out of the window still has to appear: the rule no
// longer produces a start inside it, but the row is here and dated.
func TestOverrideMovedOutsideItsOwnOccurrenceStillAppears(t *testing.T) {
	m := master(t)
	moved := generated.CalendarEvent{
		ID:                      3,
		Title:                   "moved-later",
		StartAt:                 nullTime(t, "2026-04-04T09:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-04T10:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-10T10:00:00Z"),
	}

	instances := ExpandRecurringEvent(
		context.Background(),
		fakeOverrideLoader{rows: []generated.CalendarEvent{moved}},
		m,
		mustTime(t, "2026-04-04T00:00:00Z"),
		mustTime(t, "2026-04-05T00:00:00Z"),
	)

	var found bool
	for _, inst := range instances {
		if inst.Event.Title == "moved-later" {
			found = true
			if !inst.IsOverride {
				t.Fatal("relocated override not flagged as one")
			}
		}
	}
	if !found {
		t.Fatalf("override moved into the window was dropped; got %d instances", len(instances))
	}
}

// Cancelling an occurrence that also has an override row must remove it.
// If the exclusion list did not win, an implementation that wrote both
// would show the occurrence twice over -- which is exactly the divergence
// the single-representation rule exists to prevent.
func TestCancellationWinsOverAnOverrideForTheSameOccurrence(t *testing.T) {
	m := master(t)
	m.RecurrenceExceptions = exceptions(t, "2026-04-02T10:00:00Z")

	stale := generated.CalendarEvent{
		ID:                      4,
		Title:                   "stale-override",
		StartAt:                 nullTime(t, "2026-04-02T15:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-02T16:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-02T10:00:00Z"),
	}

	instances := ExpandRecurringEvent(
		context.Background(),
		fakeOverrideLoader{rows: []generated.CalendarEvent{stale}},
		m,
		mustTime(t, "2026-04-02T00:00:00Z"),
		mustTime(t, "2026-04-03T00:00:00Z"),
	)

	for _, inst := range instances {
		if inst.Event.Title == "stale-override" {
			t.Fatal("a cancelled occurrence reappeared through its override row")
		}
	}
	if len(instances) != 0 {
		t.Fatalf("got %d instances, want none: the only occurrence was cancelled", len(instances))
	}
}

// An undated row cannot be expanded. The shared schema allows one; this
// product does not create them, but another writer on the same database
// might, and a zero time rendered as an occurrence would be worse than none.
func TestUndatedSeriesExpandsToNothing(t *testing.T) {
	m := master(t)
	m.StartAt = sql.NullTime{}

	instances := ExpandRecurringEvent(
		context.Background(),
		fakeOverrideLoader{},
		m,
		mustTime(t, "2026-04-01T00:00:00Z"),
		mustTime(t, "2026-04-05T00:00:00Z"),
	)
	if len(instances) != 0 {
		t.Fatalf("got %d instances from an undated series, want none", len(instances))
	}
}
