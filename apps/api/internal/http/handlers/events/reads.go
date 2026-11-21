package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// attendeeRows is every attendee row of a set of events, disabled ones
// included, as the visibility decision reads them.
type attendeeRows map[uint32][]eventacl.Attendee

// loadAttendeeRows reads the rows of the given events in one query.
func loadAttendeeRows(ctx context.Context, q *generated.Queries, eventIDs []uint32) (attendeeRows, error) {
	rows := attendeeRows{}
	if len(eventIDs) == 0 {
		return rows, nil
	}
	keys := make([]sql.NullInt32, 0, len(eventIDs))
	for _, id := range eventIDs {
		keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	found, err := q.ListAttendeeRowsByEvents(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, r := range found {
		if r.EventID.Valid {
			id := uint32(r.EventID.Int32)
			rows[id] = append(rows[id], eventacl.Attendee{UserID: r.UserID, Enabled: r.Enabled})
		}
	}
	return rows, nil
}

// memberReader decides what one member of a calendar may see of its events.
type memberReader struct {
	userID     uint32
	calDefault eventacl.Visibility
}

func newMemberReader(cal generated.Calendar, userID uint32) memberReader {
	return memberReader{userID: userID, calDefault: eventacl.Visibility(cal.DefaultEventVisibility)}
}

// decide judges one event or occurrence. row is the row whose fields the reader
// would be shown; series and override are the attendee rows of the series and,
// for an edited occurrence, of its override.
func (m memberReader) decide(row generated.CalendarEvent, series, override []eventacl.Attendee) eventacl.Decision {
	return eventacl.Decide(
		eventacl.Event{
			Visibility:      eventacl.Visibility(row.Visibility),
			CalendarDefault: m.calDefault,
			OwnerUserID:     row.OwnerUserID,
			CreatedByUserID: row.CreatedByUserID,
		},
		eventacl.Reader{UserID: m.userID, IsMember: true},
		eventacl.OccurrenceAttendees(series, override),
	)
}

// decideForWrite judges the event or occurrence a write names, from the rows
// the write's transaction has locked.
func decideForWrite(ctx context.Context, q *generated.Queries, w eventAccess, userID uint32) (eventacl.Decision, error) {
	ids := []uint32{w.evt.ID}
	row := w.evt
	named := w.scope.occurrence != nil && w.scope.override != nil
	if named {
		row = *w.scope.override
		ids = append(ids, row.ID)
	}
	rows, err := loadAttendeeRows(ctx, q, ids)
	if err != nil {
		return eventacl.Decision{}, err
	}
	var override []eventacl.Attendee
	if named {
		override = rows[row.ID]
	}
	return newMemberReader(w.cal, userID).decide(row, rows[w.evt.ID], override), nil
}

// withhold removes from a response what the decision keeps from the reader.
func withhold(resp *EventResponse, d eventacl.Decision) {
	if !d.Title {
		resp.Title = ""
	}
	if !d.Location {
		resp.Location = ""
	}
	if !d.Memo {
		resp.Memo = ""
	}
	if !d.URL {
		resp.URL = ""
	}
	resp.DetailsHidden = d.DetailsHidden
}

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

		// Who attends decides who may see an occurrence, so every row the
		// window touches is read before anything is offered to the cap.
		decisionIDs := make([]uint32, 0, len(rows)+len(recurringRows))
		for _, e := range rows {
			decisionIDs = append(decisionIDs, e.ID)
		}
		for _, e := range recurringRows {
			decisionIDs = append(decisionIDs, e.ID)
			for _, o := range overrides[e.ID] {
				decisionIDs = append(decisionIDs, o.ID)
			}
		}
		rawAttendees, err := loadAttendeeRows(ctx, deps.Queries, decisionIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		reader := newMemberReader(cal, userID)

		type listed struct {
			series   bool
			row      generated.CalendarEvent
			expanded eventexpand.Instance
			decision eventacl.Decision
		}
		// One cap over everything the window holds, cut by date: a listing
		// past it loses the tail of the window, never a whole series or the
		// one-off events read before the series. What the reader cannot see is
		// dropped first, so it neither uses up the cap nor makes the listing
		// look cut.
		kept := daterange.NewEarliest[listed](in.Limit)
		for _, e := range rows {
			d := reader.decide(e, rawAttendees[e.ID], nil)
			if d.Exists {
				kept.Offer(nullTimeValue(e.StartAt), listed{row: e, decision: d})
			}
		}
		for _, e := range recurringRows {
			for _, expanded := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], startTime, endTime) {
				var overrideRows []eventacl.Attendee
				start := expanded.Occurrence.StartAt
				shown := e
				if expanded.IsOverride {
					start = nullTimeValue(expanded.Event.StartAt)
					shown = expanded.Event
					overrideRows = rawAttendees[expanded.Event.ID]
				}
				d := reader.decide(shown, rawAttendees[e.ID], overrideRows)
				if d.Exists {
					kept.Offer(start, listed{series: true, row: e, expanded: expanded, decision: d})
				}
			}
		}
		items := kept.Items()

		// Attendees and people are read for what is kept, each in one query.
		var eventIDs, userIDs []uint32
		seenEvent, seenUser := map[uint32]bool{}, map[uint32]bool{}
		sourceOf := make([]uint32, len(items))
		parentOf := map[uint32]uint32{}
		for i, it := range items {
			e := it.row
			sourceOf[i] = e.ID
			if it.series && it.expanded.IsOverride {
				e = it.expanded.Event
				sourceOf[i] = e.ID
				parentOf[e.ID] = it.row.ID
			}
			if !seenEvent[sourceOf[i]] && parentOf[sourceOf[i]] == 0 {
				seenEvent[sourceOf[i]] = true
				eventIDs = append(eventIDs, sourceOf[i])
			}
			for _, id := range []uint32{e.OwnerUserID, e.CreatedByUserID} {
				if !seenUser[id] {
					seenUser[id] = true
					userIDs = append(userIDs, id)
				}
			}
		}
		attendees, err := loadOccurrenceAttendees(ctx, deps, eventIDs, parentOf)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		who, err := loadPeople(ctx, deps, cal.ID, userIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		results := make([]EventResponse, 0, len(items))
		for i, it := range items {
			var ev EventResponse
			shown := it.row
			switch {
			case !it.series:
				ev = mapEvent(it.row, cal.PublicID)
			case it.expanded.IsOverride:
				ev = mapOverrideInstance(it.row, it.expanded.Event, cal.PublicID, it.expanded.OriginalStart)
				shown = it.expanded.Event
			default:
				ev = mapRecurringInstance(it.row, cal.PublicID, it.expanded.Occurrence)
			}
			attendees.apply(&ev, sourceOf[i])
			who.apply(&ev, shown)
			withhold(&ev, it.decision)
			results = append(results, ev)
		}

		return &ListEventsOutput{Body: ListEventsBody{Items: results, HasMore: kept.Truncated()}}, nil
	}
}

func GetEvent(deps Deps) func(context.Context, *GetEventInput) (*GetEventOutput, error) {
	return func(ctx context.Context, in *GetEventInput) (*GetEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		reader := newMemberReader(cal, userID)

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
			var override *generated.CalendarEvent
			source := evt
			if expanded.IsOverride {
				resp = mapOverrideInstance(evt, expanded.Event, cal.PublicID, expanded.OriginalStart)
				source = expanded.Event
				override = &expanded.Event
			} else {
				resp = mapRecurringInstance(evt, cal.PublicID, expanded.Occurrence)
			}
			ids := []uint32{evt.ID}
			if override != nil {
				ids = append(ids, override.ID)
			}
			rows, err := loadAttendeeRows(ctx, deps.Queries, ids)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			var overrideRows []eventacl.Attendee
			if override != nil {
				overrideRows = rows[override.ID]
			}
			d := reader.decide(source, rows[evt.ID], overrideRows)
			if !d.Exists {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			var attErr error
			if override != nil {
				attErr = setOccurrenceAttendees(ctx, deps, &resp, evt.ID, override.ID)
			} else {
				attErr = setAttendees(ctx, deps, &resp, evt.ID)
			}
			if attErr != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			decorate(ctx, deps, &resp, source, cal.ID)
			withhold(&resp, d)
			return &GetEventOutput{ETag: resp.Etag, Body: resp}, nil
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}
		rows, err := loadAttendeeRows(ctx, deps.Queries, []uint32{evt.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		d := reader.decide(evt, rows[evt.ID], nil)
		if !d.Exists {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		resp := mapEvent(evt, cal.PublicID)
		if err := setAttendees(ctx, deps, &resp, evt.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, evt, cal.ID)
		withhold(&resp, d)
		return &GetEventOutput{ETag: resp.Etag, Body: resp}, nil
	}
}
