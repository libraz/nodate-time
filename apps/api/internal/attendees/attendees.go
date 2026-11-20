// Package attendees writes the participant rows of a calendar event.
package attendees

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

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
