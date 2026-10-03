package eventexpand

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// An edited occurrence that starts before a window and runs into it belongs
// to that window, exactly as an unedited occurrence of the same span would.
func TestOverrideSpanningIntoTheWindowIsIncluded(t *testing.T) {
	m := master(t)
	// The 3rd's occurrence stretched into a three-day block from the 3rd.
	stretched := generated.CalendarEvent{
		ID:                      3,
		Title:                   "stretched",
		StartAt:                 nullTime(t, "2026-04-03T10:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-06T10:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-03T10:00:00Z"),
	}
	// A window opening on the 5th: the original occurrence is outside it,
	// the override's span is not.
	got := ExpandWithOverrides(m, []generated.CalendarEvent{stretched},
		mustTime(t, "2026-04-05T00:00:00Z"), mustTime(t, "2026-04-06T00:00:00Z"))

	var overrides int
	for _, inst := range got {
		if inst.IsOverride {
			overrides++
			if inst.Event.ID != stretched.ID {
				t.Fatalf("unexpected override %d", inst.Event.ID)
			}
		}
	}
	if overrides != 1 {
		t.Fatalf("override spanning into the window appeared %d times, want once (%+v)", overrides, got)
	}
}

// The same override is never emitted twice when both its original occurrence
// and its own span fall in the window.
func TestOverrideInsideItsOwnWindowAppearsOnce(t *testing.T) {
	m := master(t)
	stretched := generated.CalendarEvent{
		ID:                      3,
		StartAt:                 nullTime(t, "2026-04-03T10:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-06T10:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-03T10:00:00Z"),
	}
	got := ExpandWithOverrides(m, []generated.CalendarEvent{stretched},
		mustTime(t, "2026-04-01T00:00:00Z"), mustTime(t, "2026-04-08T00:00:00Z"))
	count := 0
	for _, inst := range got {
		if inst.IsOverride {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("override emitted %d times, want 1", count)
	}
}

// A row naming an instant the current rule does not produce is not an
// occurrence of the series, and the window does not show it.
func TestOverrideOffTheRuleGridIsNotShown(t *testing.T) {
	m := master(t)
	weekly := json.RawMessage(`{"freq":"weekly","interval":1}`)
	m.RecurrenceRule = &weekly // Wednesdays from 2026-04-01
	orphan := generated.CalendarEvent{
		ID:                      4,
		StartAt:                 nullTime(t, "2026-04-20T10:00:00Z"),
		EndAt:                   nullTime(t, "2026-04-20T11:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-02T10:00:00Z"), // a Thursday
	}
	got := ExpandWithOverrides(m, []generated.CalendarEvent{orphan},
		mustTime(t, "2026-04-19T00:00:00Z"), mustTime(t, "2026-04-26T00:00:00Z"))
	for _, inst := range got {
		if inst.IsOverride {
			t.Fatalf("override off the rule grid was shown: %+v", inst)
		}
	}
}

type fakeOccurrenceLoader struct {
	rows []generated.CalendarEvent
}

func (f fakeOccurrenceLoader) GetRecurrenceOverride(_ context.Context, arg generated.GetRecurrenceOverrideParams) (generated.CalendarEvent, error) {
	for _, r := range f.rows {
		if r.RecurrenceOriginalStart.Time.Equal(arg.RecurrenceOriginalStart.Time) {
			return r, nil
		}
	}
	return generated.CalendarEvent{}, sql.ErrNoRows
}

// An occurrence resolves to its override wherever the override was moved, to
// the rule's own instance when it was never edited, and to nothing once
// cancelled.
func TestResolveOccurrence(t *testing.T) {
	m := master(t)
	m.RecurrenceExceptions = exceptions(t, "2026-04-02T10:00:00Z")
	moved := generated.CalendarEvent{
		ID:                      3,
		StartAt:                 nullTime(t, "2026-05-20T10:00:00Z"),
		EndAt:                   nullTime(t, "2026-05-20T11:00:00Z"),
		RecurrenceOriginalStart: nullTime(t, "2026-04-03T10:00:00Z"),
	}
	loader := fakeOccurrenceLoader{rows: []generated.CalendarEvent{moved}}
	occ := func(s string) recurrence.Occurrence {
		start := mustTime(t, s)
		return recurrence.Occurrence{StartAt: start, EndAt: start.Add(3600e9)}
	}

	inst, err := ResolveOccurrence(context.Background(), loader, m, occ("2026-04-03T10:00:00Z"))
	if err != nil || !inst.IsOverride || inst.Event.ID != moved.ID {
		t.Fatalf("moved occurrence = %+v, %v; want its override", inst, err)
	}
	inst, err = ResolveOccurrence(context.Background(), loader, m, occ("2026-04-04T10:00:00Z"))
	if err != nil || inst.IsOverride || inst.Event.ID != m.ID {
		t.Fatalf("unedited occurrence = %+v, %v; want the series", inst, err)
	}
	if _, err = ResolveOccurrence(context.Background(), loader, m, occ("2026-04-02T10:00:00Z")); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled occurrence err = %v, want ErrCancelled", err)
	}
}
