package events

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// maxSearchCandidates is how many rows a search may match before it is refused
// as too broad.
const maxSearchCandidates = 2000

// searchSpans are the windows, counted from the start of the search, in which
// a series is looked at for its first upcoming occurrence. They widen only
// when a narrower one found nothing.
var searchSpans = []time.Duration{
	90 * 24 * time.Hour,
	366 * 24 * time.Hour,
	5 * 366 * 24 * time.Hour,
	20 * 366 * 24 * time.Hour,
}

type SearchEventsInput struct {
	Q     string `query:"q" required:"true" minLength:"1" maxLength:"100" doc:"Text to find in titles, and in locations and memos the caller may read"`
	From  string `query:"from" required:"false" doc:"RFC 3339 instant or YYYY-MM-DD date in the caller's zone; defaults to now. Events ending after it match"`
	Limit int    `query:"limit" default:"30" minimum:"1" maximum:"100" doc:"Most events to return"`
}

type SearchEventsOutput struct {
	Body ListEventsBody
}

// likePattern turns text into a LIKE pattern matching it literally.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// searchStart resolves from into the instant and the day (a UTC midnight)
// that timed and all-day events are compared against.
func searchStart(from string, loc *time.Location, now time.Time) (time.Time, time.Time, bool) {
	var at time.Time
	switch {
	case from == "":
		at = now
	default:
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			at = t
		} else if d, err := time.ParseInLocation(dateLayout, from, loc); err == nil {
			at = d
		} else {
			return time.Time{}, time.Time{}, false
		}
	}
	local := at.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return at.UTC(), day, true
}

// searchHit is one matching event or occurrence, before it is rendered.
type searchHit struct {
	cal      generated.Calendar
	series   bool
	row      generated.CalendarEvent
	inst     eventexpand.Instance
	start    time.Time
	sortID   string
	decision eventacl.Decision
}

func SearchEvents(deps Deps) func(context.Context, *SearchEventsInput) (*SearchEventsOutput, error) {
	return func(ctx context.Context, in *SearchEventsInput) (*SearchEventsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		loc := daterange.Location("", actorTimezone(ctx, deps, userID))
		fromAt, fromDay, ok := searchStart(in.From, loc, time.Now())
		if !ok {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}

		candidates, err := deps.Queries.SearchEventCandidates(ctx, generated.SearchEventCandidatesParams{
			WorkspaceID: deps.WorkspaceID,
			UserID:      userID,
			FromAt:      fromAt,
			FromDay:     fromDay,
			Pattern:     likePattern(in.Q),
			Limit:       maxSearchCandidates + 1,
		})
		if err != nil {
			slog.ErrorContext(ctx, "search candidates failed", "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if len(candidates) > maxSearchCandidates {
			return nil, apierrors.ToHuma(apierrors.SearchTooBroad)
		}

		var masters, overrides, plain []generated.CalendarEvent
		known := map[uint32]generated.CalendarEvent{}
		for _, e := range candidates {
			switch {
			case e.RecurrenceParentID.Valid:
				overrides = append(overrides, e)
			case e.RecurrenceRule != nil:
				masters = append(masters, e)
				known[e.ID] = e
			default:
				plain = append(plain, e)
			}
		}

		// Series that matched through an override only are needed to place it.
		var missing []uint32
		for _, o := range overrides {
			if _, ok := known[uint32(o.RecurrenceParentID.Int32)]; !ok {
				missing = append(missing, uint32(o.RecurrenceParentID.Int32))
			}
		}
		if len(missing) > 0 {
			parents, err := deps.Queries.ListSearchSeriesByIDs(ctx, missing)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			for _, p := range parents {
				known[p.ID] = p
			}
		}

		masterIDs := make([]uint32, 0, len(masters))
		for _, m := range masters {
			masterIDs = append(masterIDs, m.ID)
		}
		changed, err := eventexpand.LoadOverrides(ctx, deps.Queries, masterIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		decisionIDs := make([]uint32, 0, len(candidates)*2)
		for _, e := range candidates {
			decisionIDs = append(decisionIDs, e.ID)
		}
		for id := range known {
			decisionIDs = append(decisionIDs, id)
		}
		for _, list := range changed {
			for _, o := range list {
				decisionIDs = append(decisionIDs, o.ID)
			}
		}
		rawAttendees, err := loadAttendeeRows(ctx, deps.Queries, decisionIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		cals := map[uint32]generated.Calendar{}
		readers := map[uint32]memberReader{}
		calendarOf := func(id uint32) (generated.Calendar, memberReader, bool) {
			if c, ok := cals[id]; ok {
				return c, readers[id], true
			}
			c, err := deps.Queries.GetCalendarByID(ctx, id)
			if err != nil {
				return generated.Calendar{}, memberReader{}, false
			}
			cals[id] = c
			readers[id] = newMemberReader(c, userID)
			return c, readers[id], true
		}

		var hits []searchHit
		add := func(row, shown generated.CalendarEvent, series bool, inst eventexpand.Instance, start time.Time, sortID string, override []eventacl.Attendee) {
			cal, reader, ok := calendarOf(row.CalendarID)
			if !ok {
				return
			}
			d := reader.decide(shown, rawAttendees[row.ID], override)
			if d.Exists {
				hits = append(hits, searchHit{cal: cal, series: series, row: row, inst: inst, start: start, sortID: sortID, decision: d})
			}
		}

		for _, e := range plain {
			add(e, e, false, eventexpand.Instance{}, nullTimeValue(e.StartAt), pubIDToHex(e.PublicID), nil)
		}
		for _, m := range masters {
			thr := fromAt
			if m.AllDay {
				thr = fromDay
			}
			inst, found := firstOwnOccurrence(m, changed[m.ID], thr)
			if found {
				add(m, m, true, inst, inst.Occurrence.StartAt, pubIDToHex(m.PublicID), nil)
			}
		}
		for _, o := range overrides {
			parent, ok := known[uint32(o.RecurrenceParentID.Int32)]
			if !ok || !o.RecurrenceOriginalStart.Valid || !standsForOccurrence(parent, o.RecurrenceOriginalStart.Time) {
				continue
			}
			inst := eventexpand.Instance{Event: o, OriginalStart: o.RecurrenceOriginalStart.Time.UTC(), IsOverride: true}
			add(parent, o, true, inst, nullTimeValue(o.StartAt), pubIDToHex(parent.PublicID), rawAttendees[o.ID])
		}

		sort.SliceStable(hits, func(i, j int) bool {
			if !hits[i].start.Equal(hits[j].start) {
				return hits[i].start.Before(hits[j].start)
			}
			return hits[i].sortID < hits[j].sortID
		})
		hasMore := len(hits) > in.Limit
		if hasMore {
			hits = hits[:in.Limit]
		}

		items, err := renderHits(ctx, deps, hits)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &SearchEventsOutput{Body: ListEventsBody{Items: items, HasMore: hasMore}}, nil
	}
}

// firstOwnOccurrence finds the first occurrence of a series ending after thr
// that is neither cancelled nor replaced by an override; overrides are
// candidates in their own right.
func firstOwnOccurrence(m generated.CalendarEvent, changed []generated.CalendarEvent, thr time.Time) (eventexpand.Instance, bool) {
	for _, span := range searchSpans {
		for _, inst := range eventexpand.ExpandWithOverrides(m, changed, thr, thr.Add(span)) {
			if !inst.IsOverride {
				return inst, true
			}
		}
	}
	return eventexpand.Instance{}, false
}

// standsForOccurrence reports whether an override still stands for an
// occurrence its series produces and has not cancelled.
func standsForOccurrence(parent generated.CalendarEvent, original time.Time) bool {
	if parent.RecurrenceRule == nil || !parent.StartAt.Valid || !parent.EndAt.Valid {
		return false
	}
	rule, err := recurrence.ParseRule(*parent.RecurrenceRule)
	if err != nil || rule == nil {
		return false
	}
	if recurrence.ParseExceptionList(parent.RecurrenceExceptions).Cancels(original, parent.AllDay, parent.Timezone) {
		return false
	}
	zone := recurrence.ExpansionZone(parent.AllDay, parent.Timezone)
	return recurrence.OccursAt(rule, parent.StartAt.Time, parent.EndAt.Time, zone, original.UTC())
}

// renderHits maps hits the way a listing does.
func renderHits(ctx context.Context, deps Deps, hits []searchHit) ([]EventResponse, error) {
	var eventIDs []uint32
	seenEvent := map[uint32]bool{}
	sourceOf := make([]uint32, len(hits))
	parentOf := map[uint32]uint32{}
	shownOf := make([]generated.CalendarEvent, len(hits))
	userIDs := map[uint32][]uint32{}
	seenUser := map[[2]uint32]bool{}
	for i, h := range hits {
		e := h.row
		if h.series && h.inst.IsOverride {
			e = h.inst.Event
			parentOf[e.ID] = h.row.ID
		}
		shownOf[i], sourceOf[i] = e, e.ID
		if !seenEvent[e.ID] && parentOf[e.ID] == 0 {
			seenEvent[e.ID] = true
			eventIDs = append(eventIDs, e.ID)
		}
		for _, id := range []uint32{e.OwnerUserID, e.CreatedByUserID} {
			if k := [2]uint32{h.cal.ID, id}; !seenUser[k] {
				seenUser[k] = true
				userIDs[h.cal.ID] = append(userIDs[h.cal.ID], id)
			}
		}
	}
	attendees, err := loadOccurrenceAttendees(ctx, deps, eventIDs, parentOf)
	if err != nil {
		return nil, err
	}
	who := map[uint32]people{}
	for calID, ids := range userIDs {
		if who[calID], err = loadPeople(ctx, deps, calID, ids); err != nil {
			return nil, err
		}
	}

	out := make([]EventResponse, 0, len(hits))
	for i, h := range hits {
		var ev EventResponse
		switch {
		case !h.series:
			ev = mapEvent(h.row, h.cal.PublicID)
		case h.inst.IsOverride:
			ev = mapOverrideInstance(h.row, h.inst.Event, h.cal.PublicID, h.inst.OriginalStart)
		default:
			ev = mapRecurringInstance(h.row, h.cal.PublicID, h.inst.Occurrence)
		}
		attendees.apply(&ev, sourceOf[i])
		who[h.cal.ID].apply(&ev, shownOf[i])
		withhold(&ev, h.decision)
		out = append(out, ev)
	}
	return out, nil
}
