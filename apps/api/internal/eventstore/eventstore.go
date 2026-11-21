// Package eventstore holds the shared write paths that create a calendar
// event, delete one, and delete a series' override rows, used by the event
// handlers, schedule-poll finalisation and calendar import.
package eventstore

import (
	"context"
	"database/sql"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
)

// Insert persists a validated new event, its attendees, and its history entry
// using the caller's transaction, and returns the stored row. Callers resolve
// the calendar grant and attendee membership first. extra is merged into the
// history payload and may be nil.
func Insert(ctx context.Context, q *generated.Queries, params generated.CreateCalendarEventParams, attendeeUserIDs []uint32, extra map[string]any) (generated.CalendarEvent, error) {
	result, err := q.CreateCalendarEvent(ctx, params)
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	if err := attendees.AddToNew(ctx, q, params.WorkspaceID, uint32(eventID), attendeeUserIDs); err != nil {
		return generated.CalendarEvent{}, err
	}
	created, err := q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: params.WorkspaceID,
		PublicID:    params.PublicID,
	})
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: params.WorkspaceID,
		CalendarID:  params.CalendarID,
		ActorUserID: params.CreatedByUserID,
		Type:        eventlog.TypeEventCreated,
		Title:       params.Title,
		Subject:     params.PublicID,
		Extra:       extra,
	}); err != nil {
		return generated.CalendarEvent{}, err
	}
	return created, nil
}

// SoftDelete retires a one-off event or a whole series with its attachments
// and logs the deletion. A retired attachment keeps its reference on the blob
// until the cleanup sweep removes the row, so no count changes here. extra is
// merged into the history payload and may be nil.
func SoftDelete(ctx context.Context, q *generated.Queries, workspaceID, calendarID, actorUserID uint32, evt generated.CalendarEvent, extra map[string]any) error {
	eventRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
	if err := q.SoftDeleteAttachmentsByEvent(ctx, eventRef); err != nil {
		return err
	}
	// Soft delete keeps the row, so its override rows keep their parent and the
	// history stays readable.
	if err := q.SoftDeleteCalendarEvent(ctx, evt.ID); err != nil {
		return err
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: workspaceID,
		CalendarID:  calendarID,
		ActorUserID: actorUserID,
		Type:        eventlog.TypeEventDeleted,
		Title:       evt.Title,
		Subject:     evt.PublicID,
		Extra:       extra,
	})
}

// DeleteOverrides hard-deletes override rows together with their attendee
// rows. The attendee foreign key would otherwise null the event out of each
// row, and the unique key over IFNULL(event_id, 0) refuses a second such row
// for the same person.
func DeleteOverrides(ctx context.Context, q *generated.Queries, parentRef sql.NullInt32, ids []uint32) error {
	if len(ids) == 0 {
		return nil
	}
	refs := make([]sql.NullInt32, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	if err := q.DeleteAttendeesOfEvents(ctx, refs); err != nil {
		return err
	}
	return q.DeleteRecurrenceOverrides(ctx, generated.DeleteRecurrenceOverridesParams{
		ParentID: parentRef,
		Ids:      ids,
	})
}
