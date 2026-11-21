package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// eventAttendees reads the participant rows once and returns both shapes the
// response carries: the id-only list, which is also the write format, and the
// per-participant state that only a read can tell you.
//
// A failed read is reported rather than rendered as "nobody is attending". The
// participant list is also the write format, so a client that receives an
// empty one and later saves the event sends that emptiness back as the
// authoritative list and every attendee is removed.
func eventAttendees(ctx context.Context, deps Deps, eventID uint32) ([]string, []AttendeeResponse, error) {
	rows, err := deps.Queries.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(eventID), Valid: true})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return []string{}, []AttendeeResponse{}, nil
	}
	ids := make([]string, 0, len(rows))
	states := make([]AttendeeResponse, 0, len(rows))
	for _, p := range rows {
		id := pubIDToHex(p.UserPublicID)
		ids = append(ids, id)
		states = append(states, AttendeeResponse{
			UserID:  id,
			Rsvp:    string(p.Rsvp),
			CanEdit: p.CanEdit,
		})
	}
	return ids, states, nil
}

// attendeeSet is every event's participants in a listing, read together.
type attendeeSet map[uint32]struct {
	ids       []string
	attendees []AttendeeResponse
}

// loadAttendees reads the participants of a whole listing in one query.
//
// Asking per event made rendering a month cost one round trip per event on
// it, and a month view fetches every calendar in parallel, so the count
// multiplied by however many calendars the person had.
func loadAttendees(ctx context.Context, deps Deps, eventIDs []uint32) (attendeeSet, error) {
	set := attendeeSet{}
	if len(eventIDs) == 0 {
		return set, nil
	}
	keys := make([]sql.NullInt32, 0, len(eventIDs))
	for _, id := range eventIDs {
		keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	rows, err := deps.Queries.ListEventAttendeesByEvents(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		if !p.EventID.Valid {
			continue
		}
		key := uint32(p.EventID.Int32)
		entry := set[key]
		id := pubIDToHex(p.UserPublicID)
		entry.ids = append(entry.ids, id)
		entry.attendees = append(entry.attendees, AttendeeResponse{
			UserID:  id,
			Rsvp:    string(p.Rsvp),
			CanEdit: p.CanEdit,
		})
		set[key] = entry
	}
	return set, nil
}

// apply fills a response from the batch, which reports an empty list for an
// event with no participants -- a distinction the batch can make because the
// query it came from covered every event asked about.
func (s attendeeSet) apply(resp *EventResponse, eventID uint32) {
	entry, ok := s[eventID]
	if !ok || len(entry.ids) == 0 {
		resp.ParticipantIDs, resp.Attendees = []string{}, []AttendeeResponse{}
		return
	}
	resp.ParticipantIDs, resp.Attendees = entry.ids, entry.attendees
}

// setOccurrenceAttendees is setAttendees for an override, which shows the
// series' participants until it has attendee rows of its own.
func setOccurrenceAttendees(ctx context.Context, deps Deps, resp *EventResponse, seriesID, overrideID uint32) error {
	set, err := loadOccurrenceAttendees(ctx, deps, []uint32{overrideID}, map[uint32]uint32{overrideID: seriesID})
	if err != nil {
		return err
	}
	set.apply(resp, overrideID)
	return nil
}

// loadOccurrenceAttendees is loadAttendees for a listing that includes
// overrides. parentOf maps each override's id to its series' id; an override
// with no attendee row of its own is answered with its series' participants.
func loadOccurrenceAttendees(ctx context.Context, deps Deps, eventIDs []uint32, parentOf map[uint32]uint32) (attendeeSet, error) {
	if len(parentOf) == 0 {
		return loadAttendees(ctx, deps, eventIDs)
	}
	all := slices.Clone(eventIDs)
	overrides := make([]sql.NullInt32, 0, len(parentOf))
	for override, series := range parentOf {
		all = append(all, override, series)
		overrides = append(overrides, sql.NullInt32{Int32: int32(override), Valid: true})
	}
	set, err := loadAttendees(ctx, deps, all)
	if err != nil {
		return nil, err
	}
	withRows, err := deps.Queries.ListEventIDsWithAttendeeRows(ctx, overrides)
	if err != nil {
		return nil, err
	}
	own := make(map[uint32]struct{}, len(withRows))
	for _, id := range withRows {
		own[uint32(id.Int32)] = struct{}{}
	}
	for override, series := range parentOf {
		if _, ok := own[override]; !ok {
			set[override] = set[series]
		}
	}
	return set, nil
}

// setAttendees fills both attendee-derived fields of a response from one read.
func setAttendees(ctx context.Context, deps Deps, resp *EventResponse, eventID uint32) error {
	participants, attendees, err := eventAttendees(ctx, deps, eventID)
	if err != nil {
		return err
	}
	resp.ParticipantIDs, resp.Attendees = participants, attendees
	return nil
}

func validateEventParticipants(ctx context.Context, q *generated.Queries, calID uint32, participantIDs []string) ([]attendees.Person, error) {
	participants := make([]attendees.Person, 0, len(participantIDs))
	seen := make(map[uint32]struct{}, len(participantIDs))
	for _, pUUID := range participantIDs {
		pPub, err := parseUUID(pUUID)
		if err != nil {
			return nil, apierrors.BadRequest
		}
		u, err := q.GetUserByPublicID(ctx, pPub)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.BadRequest
			}
			return nil, fmt.Errorf("look up participant: %w", err)
		}
		// Only calendar members may be added as participants.
		if _, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: calID, UserID: u.ID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.BadRequest
			}
			return nil, fmt.Errorf("check participant membership: %w", err)
		}
		if _, duplicate := seen[u.ID]; duplicate {
			continue
		}
		seen[u.ID] = struct{}{}
		participants = append(participants, attendees.Person{PublicID: pubIDToHex(u.PublicID), UserID: u.ID, Name: u.DisplayName})
	}
	return participants, nil
}

// participantLogExtra is the payload every attendee row of an edit shares: an
// edit scoped to an occurrence says so and names the occurrence, as
// logAttendeeChange does.
func participantLogExtra(w eventAccess) map[string]any {
	extra := map[string]any{}
	if w.scope.occurrence != nil {
		extra["scope"] = "occurrence"
		extra["originalStart"] = w.scope.occurrence.StartAt.Format(time.RFC3339)
	}
	return extra
}
