package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

func ListEvents(deps Deps) func(context.Context, *ListEventsInput) (*ListEventsOutput, error) {
	return func(ctx context.Context, in *ListEventsInput) (*ListEventsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		loc := daterange.Location(in.TZ, actorTimezone(ctx, deps, userID))
		window, err := daterange.Resolve(in.StartDate, in.EndDate, in.Days, loc)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		startTime, endTime := window.Start, window.End

		rows, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		recurringRows, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Everything the listing needs beyond the two range queries is read
		// once per listing. Doing it per event made a month view cost a round
		// trip per event on it, times however many calendars are shown at once.
		seriesIDs := make([]uint32, 0, len(recurringRows))
		for _, e := range recurringRows {
			seriesIDs = append(seriesIDs, e.ID)
		}
		overrides, err := eventexpand.LoadOverrides(ctx, deps.Queries, seriesIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// One cap over everything the window holds, cut by date: a listing
		// past it loses the tail of the window, never a whole series or the
		// one-off events read before the series.
		type listed struct {
			series   bool
			row      generated.CalendarEvent
			expanded eventexpand.Instance
		}
		kept := daterange.NewEarliest[listed](daterange.MaxInstances)
		for _, e := range rows {
			kept.Offer(nullTimeValue(e.StartAt), listed{row: e})
		}
		for _, e := range recurringRows {
			for _, expanded := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], startTime, endTime) {
				start := expanded.Occurrence.StartAt
				if expanded.IsOverride {
					start = nullTimeValue(expanded.Event.StartAt)
				}
				kept.Offer(start, listed{series: true, row: e, expanded: expanded})
			}
		}
		items := kept.Items()

		// Attendees and people are read for what is kept, each in one query.
		var eventIDs, userIDs []uint32
		seenEvent, seenUser := map[uint32]bool{}, map[uint32]bool{}
		for _, it := range items {
			e := it.row
			if it.series {
				e = it.expanded.Event
			}
			if !seenEvent[e.ID] {
				seenEvent[e.ID] = true
				eventIDs = append(eventIDs, e.ID)
			}
			for _, id := range []uint32{e.OwnerUserID, e.CreatedByUserID} {
				if !seenUser[id] {
					seenUser[id] = true
					userIDs = append(userIDs, id)
				}
			}
		}
		attendees, err := loadAttendees(ctx, deps, eventIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		who, err := loadPeople(ctx, deps, cal.ID, userIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		results := make([]EventResponse, 0, len(items))
		for _, it := range items {
			switch {
			case !it.series:
				ev := mapEvent(it.row, cal.PublicID)
				attendees.apply(&ev, it.row.ID)
				who.apply(&ev, it.row)
				results = append(results, ev)
			case it.expanded.IsOverride:
				inst := mapOverrideInstance(it.row, it.expanded.Event, cal.PublicID, it.expanded.OriginalStart)
				attendees.apply(&inst, it.expanded.Event.ID)
				who.apply(&inst, it.expanded.Event)
				results = append(results, inst)
			default:
				inst := mapRecurringInstance(it.row, cal.PublicID, it.expanded.Occurrence)
				attendees.apply(&inst, it.row.ID)
				who.apply(&inst, it.row)
				results = append(results, inst)
			}
		}

		out := &ListEventsOutput{Body: results}
		if kept.Truncated() {
			out.Truncated = "true"
		}
		return out, nil
	}
}

func GetEvent(deps Deps) func(context.Context, *GetEventInput) (*GetEventOutput, error) {
	return func(ctx context.Context, in *GetEventInput) (*GetEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// Check for composite ID (recurring instance)
		parentUUID, dateStr := parseCompositeID(in.EventID)
		if parentUUID != "" {
			evt, err := loadEventInCalendar(ctx, deps, cal.ID, parentUUID)
			if err != nil {
				return nil, toAPIError(err)
			}

			// Resolved by the occurrence it names, not by where it now sits:
			// an edited occurrence keeps its id wherever it was moved.
			if evt.RecurrenceRule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			rule, rerr := recurrence.ParseRule(*evt.RecurrenceRule)
			if rerr != nil || rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			occ, oerr := occurrenceForDate(rule, evt, dateStr)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			expanded, xerr := eventexpand.ResolveOccurrence(ctx, deps.Queries, evt, occ)
			if errors.Is(xerr, eventexpand.ErrCancelled) {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			if xerr != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			var resp EventResponse
			source := evt
			if expanded.IsOverride {
				resp = mapOverrideInstance(evt, expanded.Event, cal.PublicID, expanded.OriginalStart)
				source = expanded.Event
			} else {
				resp = mapRecurringInstance(evt, cal.PublicID, expanded.Occurrence)
			}
			if err := setAttendees(ctx, deps, &resp, source.ID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			decorate(ctx, deps, &resp, source, cal.ID)
			// The tag names the series row: an occurrence has no address of
			// its own until it is changed, so one tag has to cover the whole
			// series or a caller would have nothing to hold.
			return &GetEventOutput{ETag: eventETag(evt), Body: resp}, nil
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		resp := mapEvent(evt, cal.PublicID)
		if err := setAttendees(ctx, deps, &resp, evt.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, evt, cal.ID)
		return &GetEventOutput{ETag: eventETag(evt), Body: resp}, nil
	}
}
