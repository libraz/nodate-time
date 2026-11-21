// Package availability checks schedule-poll slots against participating members'
// events without returning any individual member or event information.
package availability

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

type Slot struct{ Start, End time.Time }

type State string

const (
	Open State = "open"
	Soft State = "soft"
	Hard State = "hard"
)

var (
	ErrInvalid    = errors.New("invalid availability slot")
	ErrLimit      = errors.New("availability search could not be fully checked")
	ErrMembership = errors.New("inactive availability calendar or member")
)

const (
	maxSlots     = 4096
	maxCalendars = 64
	maxMembers   = 1000
	maxRows      = 5000
	maxInstances = 50000
	maxWindow    = 366 * 24 * time.Hour
	// The final wall-clock day in a recurring all-day event may be longer
	// than the absolute duration used to compute its stored recurrence_end.
	wallClockMargin = 48 * time.Hour
	// dayMargin widens a search so an all-day row, stored as UTC midnights,
	// is found whichever zone its dates are read in (offsets reach 14 hours).
	dayMargin = 24 * time.Hour
)

// Check returns one aggregate state per slot in input order. A transaction's
// Queries must be passed when a decision based on these reads is persisted.
func Check(ctx context.Context, q *generated.Queries, workspaceID, calendarID uint32, memberIDs []uint32, slots []Slot) ([]State, error) {
	if len(slots) > maxSlots || len(memberIDs) > maxMembers {
		return nil, ErrLimit
	}
	if len(slots) == 0 {
		return []State{}, nil
	}
	window := slots[0]
	for _, slot := range slots {
		if slot.Start.IsZero() || !slot.End.After(slot.Start) {
			return nil, ErrInvalid
		}
		if slot.Start.Before(window.Start) {
			window.Start = slot.Start
		}
		if slot.End.After(window.End) {
			window.End = slot.End
		}
	}
	if window.End.Sub(window.Start) > maxWindow {
		return nil, ErrLimit
	}

	scan := calendarScan{q: q, workspaceID: workspaceID, window: window, remainingRows: maxRows, zones: map[uint32]*time.Location{}}
	if len(memberIDs) == 0 {
		return scan.busy.states(slots), nil
	}
	calendars, err := q.ListScheduleAvailabilityCalendars(ctx, generated.ListScheduleAvailabilityCalendarsParams{
		WorkspaceID: workspaceID, CalendarID: calendarID, Limit: maxCalendars + 1,
	})
	if err != nil {
		return nil, err
	}
	if len(calendars) > maxCalendars {
		return nil, ErrLimit
	}
	if !slices.Contains(calendars, calendarID) {
		return nil, ErrMembership
	}

	for _, id := range calendars {
		members, err := q.ListScheduleAvailabilityMembers(ctx, generated.ListScheduleAvailabilityMembersParams{
			WorkspaceID: workspaceID, CalendarID: id, MemberIds: memberIDs,
		})
		if err != nil {
			return nil, err
		}
		if id == calendarID && len(members) != len(memberIDs) {
			return nil, ErrMembership
		}
		if len(members) != 0 {
			if err = scan.calendar(ctx, id, members); err != nil {
				return nil, err
			}
		}
	}
	return scan.busy.states(slots), nil
}

type calendarScan struct {
	q             *generated.Queries
	workspaceID   uint32
	window        Slot
	remainingRows int
	instances     int
	busy          busyIntervals
	zones         map[uint32]*time.Location
	workspaceZone *time.Location
}

// zoneOf is the zone an all-day row of this owner is read in: the owner's own
// setting, then the workspace's, then UTC.
func (s *calendarScan) zoneOf(ctx context.Context, ownerID uint32) (*time.Location, error) {
	if loc, ok := s.zones[ownerID]; ok {
		return loc, nil
	}
	var loc *time.Location
	user, err := s.q.GetUserByID(ctx, ownerID)
	switch {
	case err == nil:
		loc = loadZone(user.Timezone)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, err
	}
	if loc == nil {
		if s.workspaceZone == nil {
			s.workspaceZone = time.UTC
			ws, err := s.q.GetWorkspaceByID(ctx, s.workspaceID)
			switch {
			case err == nil:
				if z := loadZone(ws.Timezone); z != nil {
					s.workspaceZone = z
				}
			case errors.Is(err, sql.ErrNoRows):
			default:
				return nil, err
			}
		}
		loc = s.workspaceZone
	}
	s.zones[ownerID] = loc
	return loc, nil
}

func loadZone(name string) *time.Location {
	if name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// slotOf is the span a row occupies. An all-day row names dates by its UTC
// midnights and occupies each of those days from midnight to midnight in the
// owner's zone; any other row occupies its instants.
func (s *calendarScan) slotOf(ctx context.Context, e generated.CalendarEvent, start, end time.Time) (Slot, error) {
	if !e.AllDay {
		return Slot{start, end}, nil
	}
	loc, err := s.zoneOf(ctx, e.OwnerUserID)
	if err != nil {
		return Slot{}, err
	}
	sy, sm, sd := start.UTC().Date()
	ey, em, ed := end.UTC().Date()
	return Slot{time.Date(sy, sm, sd, 0, 0, 0, 0, loc), time.Date(ey, em, ed, 0, 0, 0, 0, loc)}, nil
}

func (s *calendarScan) countRows(n int) error {
	s.remainingRows -= n
	if s.remainingRows < 0 {
		return ErrLimit
	}
	return nil
}

type eventRow = generated.ListScheduleAvailabilityEventsRow

func eventForExpansion(r eventRow) generated.CalendarEvent {
	return generated.CalendarEvent{
		ID: r.ID, AllDay: r.AllDay, OwnerUserID: r.OwnerUserID, ShowAs: r.ShowAs, Flexibility: r.Flexibility, StartAt: r.StartAt, EndAt: r.EndAt,
		Timezone: r.Timezone, RecurrenceRule: r.RecurrenceRule, RecurrenceExceptions: r.RecurrenceExceptions,
		RecurrenceParentID: r.RecurrenceParentID, RecurrenceOriginalStart: r.RecurrenceOriginalStart,
	}
}

func (s *calendarScan) calendar(ctx context.Context, calendarID uint32, members []uint32) error {
	rows, err := s.q.ListScheduleAvailabilityEvents(ctx, generated.ListScheduleAvailabilityEventsParams{
		WorkspaceID: s.workspaceID, CalendarID: calendarID, OwnerIds: members, AttendeeIds: members,
		RangeStart:      sql.NullTime{Time: s.window.Start.UTC().Add(-dayMargin), Valid: true},
		RecurrenceFloor: sql.NullTime{Time: s.window.Start.UTC().Add(-wallClockMargin - dayMargin), Valid: true},
		RangeEnd:        sql.NullTime{Time: s.window.End.UTC().Add(dayMargin), Valid: true}, Limit: int32(s.remainingRows + 1),
	})
	if err != nil {
		return err
	}
	if err = s.countRows(len(rows)); err != nil {
		return err
	}
	eligible := make(map[uint32]bool)
	parents := make(map[uint32]generated.CalendarEvent)
	children := make(map[uint32]bool)
	for _, row := range rows {
		switch {
		case row.RecurrenceParentID.Valid:
			children[uint32(row.RecurrenceParentID.Int32)] = true
			eligible[row.ID] = true
		case row.RecurrenceRule != nil:
			parents[row.ID] = eventForExpansion(row)
			eligible[row.ID] = true
		default:
			slot, err := s.slotOf(ctx, eventForExpansion(row), row.StartAt.Time, row.EndAt.Time)
			if err != nil {
				return err
			}
			s.busy.add(row.ShowAs, row.Flexibility, slot)
		}
	}
	// A moved occurrence or an occurrence inviting a different member can
	// be the only matching row. Its parent still decides whether it exists.
	var missing []uint32
	for id := range children {
		if _, ok := parents[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) != 0 {
		found, err := s.q.ListScheduleAvailabilityParents(ctx, generated.ListScheduleAvailabilityParentsParams{
			WorkspaceID: s.workspaceID, CalendarID: calendarID, ParentIds: missing,
		})
		if err != nil {
			return err
		}
		if err = s.countRows(len(found)); err != nil {
			return err
		}
		for _, parent := range found {
			parents[parent.ID] = eventForExpansion(eventRow(parent))
		}
	}
	if len(parents) == 0 {
		return nil
	}
	ids := make([]sql.NullInt32, 0, len(parents))
	for id := range parents {
		ids = append(ids, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	overrides, err := s.q.ListScheduleAvailabilityOverrides(ctx, generated.ListScheduleAvailabilityOverridesParams{
		WorkspaceID: s.workspaceID, CalendarID: calendarID, ParentIds: ids, Limit: int32(s.remainingRows + 1),
	})
	if err != nil {
		return err
	}
	if err = s.countRows(len(overrides)); err != nil {
		return err
	}
	byParent := make(eventexpand.Overrides)
	for _, row := range overrides {
		id := uint32(row.RecurrenceParentID.Int32)
		byParent[id] = append(byParent[id], eventForExpansion(eventRow(row)))
	}
	for id, parent := range parents {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.series(ctx, parent, byParent[id], eligible); err != nil {
			return err
		}
	}
	return nil
}

func (s *calendarScan) series(ctx context.Context, parent generated.CalendarEvent, overrides []generated.CalendarEvent, eligible map[uint32]bool) error {
	rule, err := recurrence.ParseRule(*parent.RecurrenceRule)
	if err != nil || rule == nil || rule.Validate(parent.StartAt.Time.In(recurrence.LoadLocation(recurrence.ExpansionZone(parent.AllDay, parent.Timezone)))) != nil {
		return ErrLimit
	}
	// The existing expander limits each call to 10,000 occurrences. A
	// validated rule produces at most one per local day. Bounding the event
	// duration plus the requested window keeps the input below that cap,
	// including long occurrences which started before the first slot.
	if parent.EndAt.Time.Sub(parent.StartAt.Time) > 9000*24*time.Hour ||
		(parent.RecurrenceExceptions != nil && len(*parent.RecurrenceExceptions) > 128*1024) {
		return ErrLimit
	}
	instances := eventexpand.ExpandWithOverrides(parent, overrides, s.window.Start.Add(-wallClockMargin-dayMargin), s.window.End.Add(dayMargin))
	s.instances += len(instances)
	if s.instances > maxInstances {
		return ErrLimit
	}
	for _, instance := range instances {
		event := instance.Event
		if !eligible[event.ID] {
			continue
		}
		start, end := instance.Occurrence.StartAt, instance.Occurrence.EndAt
		if instance.IsOverride {
			start, end = event.StartAt.Time, event.EndAt.Time
		}
		slot, err := s.slotOf(ctx, event, start, end)
		if err != nil {
			return err
		}
		s.busy.add(event.ShowAs, event.Flexibility, slot)
	}
	return nil
}

type busyIntervals struct{ hard, soft []Slot }

func (b *busyIntervals) add(showAs generated.CalendarEventsShowAs, flexibility generated.CalendarEventsFlexibility, slot Slot) {
	if !slot.End.After(slot.Start) || showAs == generated.CalendarEventsShowAsFree {
		return
	}
	if showAs == generated.CalendarEventsShowAsTentative ||
		flexibility == generated.CalendarEventsFlexibilityNegotiable ||
		flexibility == generated.CalendarEventsFlexibilityConditional {
		b.soft = append(b.soft, slot)
	} else {
		b.hard = append(b.hard, slot)
	}
}

func (b *busyIntervals) states(slots []Slot) []State {
	hard, soft := mergeIntervals(b.hard), mergeIntervals(b.soft)
	out := make([]State, len(slots))
	for i, slot := range slots {
		switch {
		case overlaps(hard, slot):
			out[i] = Hard
		case overlaps(soft, slot):
			out[i] = Soft
		default:
			out[i] = Open
		}
	}
	return out
}

func mergeIntervals(slots []Slot) []Slot {
	slices.SortFunc(slots, func(a, b Slot) int { return a.Start.Compare(b.Start) })
	merged := slots[:0]
	for _, slot := range slots {
		n := len(merged)
		if n == 0 || slot.Start.After(merged[n-1].End) {
			merged = append(merged, slot)
		} else if slot.End.After(merged[n-1].End) {
			merged[n-1].End = slot.End
		}
	}
	return merged
}

func overlaps(sorted []Slot, slot Slot) bool {
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i].End.After(slot.Start) })
	return i < len(sorted) && sorted[i].Start.Before(slot.End)
}
