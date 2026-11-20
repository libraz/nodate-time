// Package eventstore holds the one write path that creates a calendar event,
// shared by the event handlers, schedule-poll finalisation and calendar import.
package eventstore

import (
	"context"

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
