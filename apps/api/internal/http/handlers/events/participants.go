package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
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
	attendees := make([]AttendeeResponse, 0, len(rows))
	for _, p := range rows {
		id := pubIDToHex(p.UserPublicID)
		ids = append(ids, id)
		attendees = append(attendees, AttendeeResponse{
			UserID:  id,
			Rsvp:    string(p.Rsvp),
			CanEdit: p.CanEdit,
		})
	}
	return ids, attendees, nil
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
		resp.Participants, resp.Attendees = []string{}, []AttendeeResponse{}
		return
	}
	resp.Participants, resp.Attendees = entry.ids, entry.attendees
}

// setAttendees fills both attendee-derived fields of a response from one read.
func setAttendees(ctx context.Context, deps Deps, resp *EventResponse, eventID uint32) error {
	participants, attendees, err := eventAttendees(ctx, deps, eventID)
	if err != nil {
		return err
	}
	resp.Participants, resp.Attendees = participants, attendees
	return nil
}

type eventParticipant struct {
	publicID string
	userID   uint32
}

func validateEventParticipants(ctx context.Context, q *generated.Queries, calID uint32, participantIDs []string) ([]eventParticipant, *apierrors.Spec) {
	participants := make([]eventParticipant, 0, len(participantIDs))
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
			return nil, apierrors.InternalUnexpected
		}
		// Only calendar members may be added as participants.
		if _, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: calID, UserID: u.ID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.BadRequest
			}
			return nil, apierrors.InternalUnexpected
		}
		if _, duplicate := seen[u.ID]; duplicate {
			continue
		}
		seen[u.ID] = struct{}{}
		participants = append(participants, eventParticipant{publicID: pubIDToHex(u.PublicID), userID: u.ID})
	}
	return participants, nil
}

func replaceEventParticipants(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, participants []eventParticipant) error {
	// Only the people leaving are disabled, and only the people arriving are
	// added. Somebody kept keeps their row untouched -- answer and edit grant
	// both -- while a removed row loses its grant, so re-adding a participant
	// cannot hand back an edit right the owner's side did not give again.
	//
	// Only for an event that can already have attendees. Creation calls
	// addEventParticipants directly, and the difference is not tidiness --
	// see there before routing the two back through one function.
	eventRef := sql.NullInt32{Int32: int32(eventID), Valid: true}
	current, err := q.ListEnabledEventAttendeeIDs(ctx, eventRef)
	if err != nil {
		return err
	}
	wanted := make(map[uint32]struct{}, len(participants))
	for _, p := range participants {
		wanted[p.userID] = struct{}{}
	}
	present := make(map[uint32]struct{}, len(current))
	for _, userID := range current {
		present[userID] = struct{}{}
		if _, keep := wanted[userID]; keep {
			continue
		}
		if err := q.DisableEventAttendee(ctx, generated.DisableEventAttendeeParams{EventID: eventRef, UserID: userID}); err != nil {
			return err
		}
	}
	arriving := make([]uint32, 0, len(participants))
	for _, p := range participants {
		if _, ok := present[p.userID]; !ok {
			arriving = append(arriving, p.userID)
		}
	}
	return addEventParticipants(ctx, q, workspaceID, eventID, arriving)
}

// addEventParticipants adds attendees to an event that cannot already have
// any: one created by the same transaction that is now adding them.
//
// The read and disable steps of replaceEventParticipants are deliberately
// absent here, and restoring them to make the two symmetrical would restore an
// intermittent 500 on event creation. An empty UPDATE is not free. Attendee rows are
// indexed on (event_id, user_id) and a brand-new event id sorts above every
// value in that index, so the scan for rows to disable finds nothing and
// still takes a next-key lock running to supremum. Every concurrent creation
// lands on that same gap; gap locks are shared, so all of them get it, and
// then each needs to insert into the gap the others hold. Two creators are
// enough for MySQL to roll one of them back, and the caller sees a request
// that failed for no reason it could act on.
//
// The inserts on their own do not collide: an insert-intention lock conflicts
// with another transaction's gap lock, not with another insert intention.
func addEventParticipants(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, attendeeUserIDs []uint32) error {
	for _, userID := range attendeeUserIDs {
		pubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.AddEventAttendee(ctx, generated.AddEventAttendeeParams{
			PublicID:    pubID[:],
			WorkspaceID: workspaceID,
			EventID:     sql.NullInt32{Int32: int32(eventID), Valid: true},
			UserID:      userID,
		}); err != nil {
			return err
		}
	}
	return nil
}
