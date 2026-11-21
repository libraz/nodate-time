// Package attendees writes the participant rows of a calendar event.
package attendees

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
)

// Person is one participant to write: the row key, and the public id and name
// the history entries carry.
type Person struct {
	UserID   uint32
	PublicID string
	Name     string
}

// Log is what the attendee rows of one participant edit are recorded against.
type Log struct {
	ActorUserID uint32
	CalendarID  uint32
	// Subject is the public id of the event the log rows name, which for an
	// occurrence is its series.
	Subject []byte
	Title   string
	Extra   map[string]any
}

// AddToNew adds attendees to an event that cannot already have any: one
// created by the same transaction that is now adding them. The caller
// guarantees each user is a member of the event's calendar.
//
// It only inserts. Reading and disabling existing rows first, to share code
// with replacing a participant list, would restore an intermittent failure on
// event creation. An empty UPDATE is not free. Attendee rows are indexed on
// (event_id, user_id) and a brand-new event id sorts above every value in that
// index, so the scan for rows to disable finds nothing and still takes a
// next-key lock running to supremum. Every concurrent creation lands on that
// same gap; gap locks are shared, so all of them get it, and then each needs
// to insert into the gap the others hold. Two creators are enough for MySQL to
// roll one of them back. The inserts on their own do not collide: an
// insert-intention lock conflicts with another transaction's gap lock, not
// with another insert intention.
func AddToNew(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, userIDs []uint32) error {
	for _, userID := range userIDs {
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

// LogDiff records one attendee.added row for everyone in wanted who was not in
// before and one attendee.removed row per person in before who is not in
// wanted. before is what the reader showed ahead of the edit, so rows the
// reader hides are never reported.
func LogDiff(ctx context.Context, q *generated.Queries, workspaceID uint32, log Log, before []generated.ListEventAttendeesRow, wanted []Person) error {
	shown := make(map[uint32]struct{}, len(before))
	for _, b := range before {
		shown[b.UserID] = struct{}{}
	}
	keep := make(map[uint32]struct{}, len(wanted))
	var addedIDs, addedNames []string
	for _, p := range wanted {
		keep[p.UserID] = struct{}{}
		if _, ok := shown[p.UserID]; !ok {
			addedIDs = append(addedIDs, p.PublicID)
			addedNames = append(addedNames, p.Name)
		}
	}
	entry := func(kind string, extra map[string]any, summary string) eventlog.Entry {
		row := make(map[string]any, len(log.Extra)+len(extra))
		for k, v := range log.Extra {
			row[k] = v
		}
		for k, v := range extra {
			row[k] = v
		}
		return eventlog.Entry{
			WorkspaceID: workspaceID,
			CalendarID:  log.CalendarID,
			ActorUserID: log.ActorUserID,
			Type:        kind,
			Title:       log.Title,
			Summary:     summary,
			Subject:     log.Subject,
			Extra:       row,
		}
	}
	if len(addedIDs) > 0 {
		if err := eventlog.Append(ctx, q, entry(eventlog.TypeAttendeeAdded,
			map[string]any{"count": len(addedIDs), "userIds": addedIDs}, strings.Join(addedNames, ", "))); err != nil {
			return err
		}
	}
	for _, b := range before {
		if _, ok := keep[b.UserID]; ok {
			continue
		}
		if err := eventlog.Append(ctx, q, entry(eventlog.TypeAttendeeRemoved,
			map[string]any{"userId": eventlog.UUID(b.UserPublicID)}, b.DisplayName)); err != nil {
			return err
		}
	}
	return nil
}

// Replace makes people the participants of an event that may already have
// some, and logs the difference.
func Replace(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, people []Person, log Log) error {
	// Only the people leaving are disabled, and only the people arriving are
	// added. Somebody kept keeps their row untouched -- answer and edit grant
	// both -- while a removed row loses its grant, so re-adding a participant
	// cannot hand back an edit right the owner's side did not give again.
	//
	// Only for an event that can already have attendees. Creation calls
	// AddToNew directly, and the difference is not tidiness -- see there
	// before routing the two back through one function.
	eventRef := sql.NullInt32{Int32: int32(eventID), Valid: true}
	before, err := q.ListEventAttendees(ctx, eventRef)
	if err != nil {
		return err
	}
	current, err := q.ListEnabledEventAttendeeIDs(ctx, eventRef)
	if err != nil {
		return err
	}
	wanted := make(map[uint32]struct{}, len(people))
	for _, p := range people {
		wanted[p.UserID] = struct{}{}
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
	arriving := make([]uint32, 0, len(people))
	for _, p := range people {
		if _, ok := present[p.UserID]; !ok {
			arriving = append(arriving, p.UserID)
		}
	}
	if err := AddToNew(ctx, q, workspaceID, eventID, arriving); err != nil {
		return err
	}
	return LogDiff(ctx, q, workspaceID, log, before, people)
}
