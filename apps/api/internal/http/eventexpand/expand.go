// Package eventexpand turns a stored recurring series into the occurrences
// that fall inside a window.
//
// A series is one row carrying the rule, and the contract allows exactly
// two ways to depart from it:
//
//   - a cancelled occurrence is listed in the parent's recurrence_exceptions
//   - a changed occurrence is a second row naming the parent in
//     recurrence_parent_id and the occurrence it replaces in
//     recurrence_original_start
//
// Neither substitutes for the other. In particular a cancellation is never
// a row: a tombstone row would make "does this occurrence happen" a
// question with two sources, and the answers diverge the moment a writer
// updates one and not the other.
package eventexpand

import (
	"context"
	"database/sql"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// OverrideLoader reads the rows that replace individual occurrences.
type OverrideLoader interface {
	ListRecurrenceOverridesByParent(ctx context.Context, recurrenceParentID sql.NullInt32) ([]generated.CalendarEvent, error)
}

// BatchOverrideLoader reads them for several series at once.
type BatchOverrideLoader interface {
	ListRecurrenceOverridesByParents(ctx context.Context, parentIDs []sql.NullInt32) ([]generated.CalendarEvent, error)
}

// Overrides holds the changed occurrences of a set of series, keyed by the
// series they belong to.
type Overrides map[uint32][]generated.CalendarEvent

// LoadOverrides reads every series' overrides in one round trip.
//
// A listing expands each series in the window, so loading per series made the
// query count a function of how many recurring events a calendar holds. A
// failure yields an empty set rather than an error: the caller falls back to
// showing the series as the rule describes it, which is what it did before
// any override existed.
func LoadOverrides(ctx context.Context, loader BatchOverrideLoader, seriesIDs []uint32) Overrides {
	if len(seriesIDs) == 0 {
		return Overrides{}
	}
	keys := make([]sql.NullInt32, 0, len(seriesIDs))
	for _, id := range seriesIDs {
		keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	rows, err := loader.ListRecurrenceOverridesByParents(ctx, keys)
	if err != nil {
		return Overrides{}
	}
	out := make(Overrides, len(seriesIDs))
	for _, r := range rows {
		if !r.RecurrenceParentID.Valid {
			continue
		}
		parent := uint32(r.RecurrenceParentID.Int32)
		out[parent] = append(out[parent], r)
	}
	return out
}

type Instance struct {
	Event      generated.CalendarEvent
	Occurrence recurrence.Occurrence
	// OriginalStart is the start the occurrence has under the parent rule.
	// It is what the composite id is built from, so an override stays
	// addressable at the same id after it has been moved.
	OriginalStart time.Time
	IsOverride    bool
}

type overrideSet struct {
	byInstant map[int64]generated.CalendarEvent
}

func loadOverrides(ctx context.Context, loader OverrideLoader, parentID uint32) overrideSet {
	rows, err := loader.ListRecurrenceOverridesByParent(ctx, sql.NullInt32{Int32: int32(parentID), Valid: true})
	if err != nil {
		return overrideSet{}
	}
	return indexOverrides(rows)
}

func indexOverrides(rows []generated.CalendarEvent) overrideSet {
	if len(rows) == 0 {
		return overrideSet{}
	}
	set := overrideSet{byInstant: make(map[int64]generated.CalendarEvent, len(rows))}
	for _, r := range rows {
		if !r.RecurrenceOriginalStart.Valid {
			continue
		}
		set.byInstant[r.RecurrenceOriginalStart.Time.UTC().UnixMilli()] = r
	}
	return set
}

// inWindow reports whether a start lands in [windowStart, windowEnd).
func inWindow(start, windowStart, windowEnd time.Time) bool {
	return !start.Before(windowStart) && start.Before(windowEnd)
}

// ExpandRecurringEvent returns the occurrences of event between windowStart
// and windowEnd, with cancellations removed and overrides substituted in.
//
// It reads this one series' overrides itself. A caller expanding several
// series should load them together and use ExpandWithOverrides instead.
func ExpandRecurringEvent(
	ctx context.Context,
	loader OverrideLoader,
	event generated.CalendarEvent,
	windowStart time.Time,
	windowEnd time.Time,
) []Instance {
	if event.RecurrenceRule == nil || !event.StartAt.Valid || !event.EndAt.Valid {
		return nil
	}
	return expand(event, loadOverrides(ctx, loader, event.ID), windowStart, windowEnd)
}

// ExpandWithOverrides is ExpandRecurringEvent over overrides already in hand,
// so a listing reads them once for every series it is about to expand rather
// than once per series.
func ExpandWithOverrides(
	event generated.CalendarEvent,
	overrides []generated.CalendarEvent,
	windowStart time.Time,
	windowEnd time.Time,
) []Instance {
	if event.RecurrenceRule == nil || !event.StartAt.Valid || !event.EndAt.Valid {
		return nil
	}
	return expand(event, indexOverrides(overrides), windowStart, windowEnd)
}

func expand(
	event generated.CalendarEvent,
	overrides overrideSet,
	windowStart time.Time,
	windowEnd time.Time,
) []Instance {
	rule := recurrence.ParseRule(*event.RecurrenceRule)
	if rule == nil {
		return nil
	}

	cancelled := recurrence.ParseExceptions(event.RecurrenceExceptions)
	consumed := make(map[int64]bool, len(overrides.byInstant))

	occurrences := recurrence.ExpandInZone(rule, event.StartAt.Time, event.EndAt.Time, windowStart, windowEnd, event.Timezone)
	instances := make([]Instance, 0, len(occurrences))

	for _, occ := range occurrences {
		key := occ.StartAt.UTC().UnixMilli()
		if cancelled.Contains(occ.StartAt) {
			// A cancelled occurrence is gone even if an override row still
			// names it. Marking it consumed stops the pass below from
			// resurrecting that row as a free-standing instance.
			consumed[key] = true
			continue
		}
		if child, ok := overrides.byInstant[key]; ok {
			consumed[key] = true
			// An override replaces the occurrence, so what the window sees is
			// the override's own dates. Emitting it here because the
			// occurrence it replaces falls in the window would show it on the
			// day it was moved off as well as the day it was moved to.
			if child.StartAt.Valid && !inWindow(child.StartAt.Time, windowStart, windowEnd) {
				continue
			}
			instances = append(instances, Instance{
				Event:         child,
				Occurrence:    occ,
				OriginalStart: occ.StartAt.UTC(),
				IsOverride:    true,
			})
			continue
		}
		instances = append(instances, Instance{
			Event:         event,
			Occurrence:    occ,
			OriginalStart: occ.StartAt.UTC(),
		})
	}

	// An override moved outside the window its original occurrence falls in
	// would otherwise vanish: the rule no longer produces a start inside the
	// window, but the row itself is here and dated.
	for key, child := range overrides.byInstant {
		if consumed[key] || !child.StartAt.Valid {
			continue
		}
		original := time.UnixMilli(key).UTC()
		if cancelled.Contains(original) {
			continue
		}
		if !inWindow(child.StartAt.Time, windowStart, windowEnd) {
			continue
		}
		instances = append(instances, Instance{
			Event:         child,
			OriginalStart: original,
			IsOverride:    true,
		})
	}
	return instances
}
