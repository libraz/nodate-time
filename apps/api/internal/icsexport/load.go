// Package icsexport reads a calendar's events as a given member sees them and
// hands them to the iCalendar codec. Each row, master or changed occurrence,
// is judged on its own: a series whose master the reader may not see still
// carries the changed occurrences they may.
package icsexport

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// MaxEntries bounds one load: VEVENTs, or occurrences when expanding. Nothing
// bounds how many series a calendar holds, so the bound is on the output.
const MaxEntries = 20000

// ErrTooLarge is returned when a load would hold more than MaxEntries.
var ErrTooLarge = errors.New("icsexport: too many entries")

// attendeeLoadChunk is how many events one attendee query names.
const attendeeLoadChunk = 5000

// LoadOptions shape a load. LoadCalendar reads Start, End and Expand;
// LoadSeries reads IncludeDisabled.
type LoadOptions struct {
	// Start and End bound the window; a zero bound is unbounded. With neither
	// set every dated series is read.
	Start, End time.Time
	// Expand turns each series into its occurrences inside the window, one
	// Series with a Master each. A window is then required.
	Expand bool
	// IncludeDisabled reads a series and its changed occurrences whether or
	// not they are deleted, and whatever calendar the series is now on.
	IncludeDisabled bool
}

// LoadCalendar reads what viewer sees of cal. Series are in the order the
// queries return them, each master before its changed occurrences.
func LoadCalendar(
	ctx context.Context, q *generated.Queries, viewer eventacl.Reader, cal generated.Calendar, opts LoadOptions,
) ([]ical.Series, error) {
	var heads []generated.CalendarEvent
	if opts.Expand || !opts.Start.IsZero() || !opts.End.IsZero() {
		// The selection the listing makes, so a series whose changed
		// occurrence sits in the window comes with it.
		rangeStart, rangeEnd := queryBounds(opts.Start, opts.End)
		plain, err := q.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   rangeEnd,
			RangeStart: rangeStart,
		})
		if err != nil {
			return nil, err
		}
		series, err := q.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   rangeEnd,
			RangeStart: rangeStart,
		})
		if err != nil {
			return nil, err
		}
		heads = append(plain, series...)
	} else {
		rows, err := q.ListCalendarEventsForExport(ctx, cal.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range rows {
			// The shared schema allows an undated row; there is nothing to
			// render as its DTSTART.
			if e.StartAt.Valid && e.EndAt.Valid {
				heads = append(heads, e)
			}
		}
	}

	var seriesKeys []sql.NullInt32
	for _, e := range heads {
		if e.RecurrenceRule != nil {
			seriesKeys = append(seriesKeys, sql.NullInt32{Int32: int32(e.ID), Valid: true})
		}
	}
	overrides := eventexpand.Overrides{}
	if len(seriesKeys) > 0 {
		rows, err := q.ListRecurrenceOverridesByParents(ctx, seriesKeys)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			parent := uint32(r.RecurrenceParentID.Int32)
			overrides[parent] = append(overrides[parent], r)
		}
	}

	ids := make([]uint32, 0, len(heads))
	for _, e := range heads {
		ids = append(ids, e.ID)
		for _, child := range overrides[e.ID] {
			ids = append(ids, child.ID)
		}
	}
	people, err := loadAttendees(ctx, q, ids)
	if err != nil {
		return nil, err
	}

	b := &builder{viewer: viewer, calendarDefault: eventacl.Visibility(cal.DefaultEventVisibility), attendees: people}
	var out []ical.Series
	for _, e := range heads {
		if !e.StartAt.Valid || !e.EndAt.Valid {
			continue
		}
		if opts.Expand && e.RecurrenceRule != nil {
			occurrences, err := b.expand(e, overrides[e.ID], opts.Start, opts.End)
			if err != nil {
				return nil, err
			}
			out = append(out, occurrences...)
			continue
		}
		s, ok, err := b.series(e, overrides[e.ID], !opts.Expand)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// LoadSeries reads what viewer sees of the series publicID names. ok is false
// when it names no dated series head, the series is not on cal, or viewer
// sees none of it. With IncludeDisabled the series is judged by cal's rules
// wherever it now is.
func LoadSeries(
	ctx context.Context, q *generated.Queries, viewer eventacl.Reader, cal generated.Calendar,
	publicID []byte, opts LoadOptions,
) (*ical.Series, bool, error) {
	var head generated.CalendarEvent
	var err error
	if opts.IncludeDisabled {
		head, err = q.GetCalendarEventByPublicIDAny(ctx, generated.GetCalendarEventByPublicIDAnyParams{
			WorkspaceID: cal.WorkspaceID, PublicID: publicID,
		})
	} else {
		head, err = q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
			WorkspaceID: cal.WorkspaceID, PublicID: publicID,
		})
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if head.RecurrenceParentID.Valid || !head.StartAt.Valid || !head.EndAt.Valid ||
		(!opts.IncludeDisabled && head.CalendarID != cal.ID) {
		return nil, false, nil
	}

	var overrides []generated.CalendarEvent
	if head.RecurrenceRule != nil {
		parent := sql.NullInt32{Int32: int32(head.ID), Valid: true}
		if opts.IncludeDisabled {
			overrides, err = q.ListRecurrenceOverridesByParentAny(ctx, parent)
		} else {
			overrides, err = q.ListRecurrenceOverridesByParent(ctx, parent)
		}
		if err != nil {
			return nil, false, err
		}
	}
	ids := []uint32{head.ID}
	for _, o := range overrides {
		ids = append(ids, o.ID)
	}
	people, err := loadAttendees(ctx, q, ids)
	if err != nil {
		return nil, false, err
	}
	b := &builder{viewer: viewer, calendarDefault: eventacl.Visibility(cal.DefaultEventVisibility), attendees: people}
	s, ok, err := b.series(head, overrides, true)
	if err != nil || !ok {
		return nil, false, err
	}
	return &s, true, nil
}

// queryBounds is the window as the range queries take it, an open end standing
// at the first or last instant the DATETIME columns hold. The floor is a second
// past year 1's first instant because the driver writes that instant as a zero
// date.
func queryBounds(start, end time.Time) (sql.NullTime, sql.NullTime) {
	lo := sql.NullTime{Time: time.Date(1, 1, 1, 0, 0, 1, 0, time.UTC), Valid: true}
	hi := sql.NullTime{Time: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), Valid: true}
	if !start.IsZero() {
		lo.Time = start
	}
	if !end.IsZero() {
		hi.Time = end
	}
	return lo, hi
}

// builder judges rows for one reader and counts what it admits.
type builder struct {
	viewer          eventacl.Reader
	calendarDefault eventacl.Visibility
	attendees       attendeeIndex
	entries         int
}

func (b *builder) decide(e generated.CalendarEvent, attendees []uint32) eventacl.Decision {
	return eventacl.Decide(eventacl.Event{
		Visibility:      eventacl.Visibility(e.Visibility),
		CalendarDefault: b.calendarDefault,
		OwnerUserID:     e.OwnerUserID,
		CreatedByUserID: e.CreatedByUserID,
	}, b.viewer, attendees)
}

// admit reports whether a row with decision d is rendered, and refuses once
// MaxEntries rows have been.
func (b *builder) admit(d eventacl.Decision) (bool, error) {
	if !d.Exists {
		return false, nil
	}
	if b.entries >= MaxEntries {
		return false, ErrTooLarge
	}
	b.entries++
	return true, nil
}

// series renders one head as a series: its master if visible and, when
// withRule, its rule, cancellations and visible changed occurrences.
func (b *builder) series(e generated.CalendarEvent, overrides []generated.CalendarEvent, withRule bool) (ical.Series, bool, error) {
	s := ical.Series{PublicID: e.PublicID, AllDay: e.AllDay, Timezone: e.Timezone}
	if withRule && e.RecurrenceRule != nil {
		rule, err := recurrence.ParseRule(*e.RecurrenceRule)
		if err != nil {
			return s, false, err
		}
		s.Rule = rule
		s.Exdates = recurrence.ParseExceptionList(e.RecurrenceExceptions)
	}
	d := b.decide(e, b.attendees.of(e.ID, nil))
	ok, err := b.admit(d)
	if err != nil {
		return s, false, err
	}
	if ok {
		m := event(e, e.StartAt.Time, e.EndAt.Time, d.DetailsHidden)
		s.Master = &m
	}
	if s.Rule != nil {
		// A changed occurrence is its own VEVENT under the series' UID, so a
		// client can put it back where it belongs.
		for _, child := range overrides {
			if !child.StartAt.Valid || !child.EndAt.Valid || !child.RecurrenceOriginalStart.Valid {
				continue
			}
			if s.Exdates.Cancels(child.RecurrenceOriginalStart.Time, e.AllDay, e.Timezone) {
				continue
			}
			d := b.decide(child, b.attendees.of(e.ID, &child))
			ok, err := b.admit(d)
			if err != nil {
				return s, false, err
			}
			if ok {
				o := event(child, child.StartAt.Time, child.EndAt.Time, d.DetailsHidden)
				o.OriginalStart = child.RecurrenceOriginalStart.Time
				s.Overrides = append(s.Overrides, o)
			}
		}
	}
	return s, s.Master != nil || len(s.Overrides) > 0, nil
}

// expand renders each occurrence of a series in [start, end) as a series of
// its own. Expansion honours both departures from the rule: cancelled
// occurrences stay out and changed ones carry their override's values.
func (b *builder) expand(e generated.CalendarEvent, overrides []generated.CalendarEvent, start, end time.Time) ([]ical.Series, error) {
	var out []ical.Series
	for _, inst := range eventexpand.ExpandWithOverrides(e, overrides, start, end) {
		row, from, to := e, inst.Occurrence.StartAt, inst.Occurrence.EndAt
		var override *generated.CalendarEvent
		if inst.IsOverride {
			if !inst.Event.StartAt.Valid || !inst.Event.EndAt.Valid {
				continue
			}
			row, from, to = inst.Event, inst.Event.StartAt.Time, inst.Event.EndAt.Time
			override = &inst.Event
		}
		d := b.decide(row, b.attendees.of(e.ID, override))
		ok, err := b.admit(d)
		if err != nil {
			return nil, err
		}
		if ok {
			m := event(row, from, to, d.DetailsHidden)
			out = append(out, ical.Series{PublicID: row.PublicID, AllDay: row.AllDay, Timezone: row.Timezone, Master: &m})
		}
	}
	return out, nil
}

// event is the codec's view of one row over [start, end).
func event(e generated.CalendarEvent, start, end time.Time, masked bool) ical.Event {
	out := ical.Event{
		Title:       e.Title,
		Location:    e.Location.String,
		Description: e.Memo.String,
		URL:         e.URL.String,
		Start:       start,
		End:         end,
		AllDay:      e.AllDay,
		Timezone:    e.Timezone,
		Class:       class(e.Visibility),
		// iCalendar tells only taken from free, so tentative and out of
		// office read as taken: the answer that does not offer time its owner
		// has not.
		Transparent: e.ShowAs == generated.CalendarEventsShowAsFree,
		Masked:      masked,
		UpdatedAt:   e.UpdatedAt.Time,
	}
	if e.NotificationOffset.Valid {
		m := e.NotificationOffset.Int32
		out.AlarmMinutes = &m
	}
	return out
}

// class renders the visibility axis as CLASS. The calendar default writes
// nothing, which is what an absent CLASS means.
func class(v generated.CalendarEventsVisibility) string {
	switch v {
	case generated.CalendarEventsVisibilityPublic:
		return "PUBLIC"
	case generated.CalendarEventsVisibilityPrivate:
		return "PRIVATE"
	case generated.CalendarEventsVisibilityConfidential:
		return "CONFIDENTIAL"
	default:
		return ""
	}
}

// attendeeIndex holds the attendees of the rows a load reads, so every
// occurrence is judged without a query of its own.
type attendeeIndex struct {
	byEvent  map[uint32][]eventacl.Attendee
	withRows map[uint32]bool
}

func loadAttendees(ctx context.Context, q *generated.Queries, ids []uint32) (attendeeIndex, error) {
	out := attendeeIndex{byEvent: map[uint32][]eventacl.Attendee{}, withRows: map[uint32]bool{}}
	for lo := 0; lo < len(ids); lo += attendeeLoadChunk {
		keys := make([]sql.NullInt32, 0, attendeeLoadChunk)
		for _, id := range ids[lo:min(lo+attendeeLoadChunk, len(ids))] {
			keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
		}
		rows, err := q.ListEventAttendeesByEvents(ctx, keys)
		if err != nil {
			return out, err
		}
		for _, r := range rows {
			out.byEvent[uint32(r.EventID.Int32)] = append(out.byEvent[uint32(r.EventID.Int32)],
				eventacl.Attendee{UserID: r.UserID, Enabled: true})
		}
		held, err := q.ListEventIDsWithAttendeeRows(ctx, keys)
		if err != nil {
			return out, err
		}
		for _, id := range held {
			out.withRows[uint32(id.Int32)] = true
		}
	}
	return out, nil
}

// of returns who attends an occurrence: an override with attendee rows of its
// own replaces the series' attendees, one with none inherits them.
func (a attendeeIndex) of(series uint32, override *generated.CalendarEvent) []uint32 {
	var replacement []eventacl.Attendee
	if override != nil && a.withRows[override.ID] {
		replacement = a.byEvent[override.ID]
		if len(replacement) == 0 {
			replacement = []eventacl.Attendee{{}}
		}
	}
	return eventacl.OccurrenceAttendees(a.byEvent[series], replacement)
}
