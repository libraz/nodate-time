package events

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// SetRsvp records the caller's own answer to an invitation.
//
// Answering is not editing: a participant who may not touch the event still
// says whether they are coming, and nobody answers on somebody else's behalf,
// which is why this takes no target user.
func SetRsvp(deps Deps) func(context.Context, *SetRsvpInput) (*SetRsvpOutput, error) {
	return func(ctx context.Context, in *SetRsvpInput) (*SetRsvpOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, occurrenceDate, err := eventTarget(ctx, deps, in.CalendarID, in.EventID, userID, resolveCalendar)
		if err != nil {
			return nil, toAPIError(err)
		}
		actor, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var att generated.CalendarEventAttendee
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, occurrenceDate, attendeeScope(occurrenceDate))
			if err != nil {
				return err
			}
			att, err = lockAttendee(ctx, q, w.row.ID, userID)
			if err != nil {
				return err
			}
			att.Rsvp = generated.CalendarEventAttendeesRsvp(in.Body.Rsvp)
			if err := q.SetEventAttendeeRsvp(ctx, generated.SetEventAttendeeRsvpParams{
				Rsvp:    att.Rsvp,
				EventID: att.EventID,
				UserID:  userID,
			}); err != nil {
				return err
			}
			return logAttendeeChange(ctx, q, deps, userID, w, actor.PublicID, "rsvp", in.Body.Rsvp)
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &SetRsvpOutput{Body: AttendeeResponse{
			UserID:  pubIDToHex(actor.PublicID),
			Rsvp:    string(att.Rsvp),
			CanEdit: att.CanEdit,
		}}, nil
	}
}

// SetAttendeeCanEdit hands a participant the right to change the event.
//
// This is the delegation the schema describes: an appointment somebody made
// for a family member, where the person it concerns has to be able to move it
// without being handed the whole calendar. Only the event's owner and whoever
// runs the calendar may grant it.
func SetAttendeeCanEdit(deps Deps) func(context.Context, *SetAttendeeCanEditInput) (*SetAttendeeCanEditOutput, error) {
	return func(ctx context.Context, in *SetAttendeeCanEditInput) (*SetAttendeeCanEditOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, evt, occurrenceDate, err := eventTarget(ctx, deps, in.CalendarID, in.EventID, actorID, resolveCalendar)
		if err != nil {
			return nil, toAPIError(err)
		}
		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
		}

		var att generated.CalendarEventAttendee
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, actorID, occurrenceDate, attendeeScope(occurrenceDate))
			if err != nil {
				return err
			}
			if err := requireEventEdit(ctx, q, w, actorID, editGrants); err != nil {
				return err
			}
			att, err = lockAttendee(ctx, q, w.row.ID, target.ID)
			if err != nil {
				return err
			}
			att.CanEdit = in.Body.CanEdit
			if err := q.SetEventAttendeeCanEdit(ctx, generated.SetEventAttendeeCanEditParams{
				CanEdit: att.CanEdit,
				EventID: att.EventID,
				UserID:  target.ID,
			}); err != nil {
				return err
			}
			return logAttendeeChange(ctx, q, deps, actorID, w, target.PublicID, "canEdit", att.CanEdit)
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &SetAttendeeCanEditOutput{Body: AttendeeResponse{
			UserID:  pubIDToHex(target.PublicID),
			Rsvp:    string(att.Rsvp),
			CanEdit: att.CanEdit,
		}}, nil
	}
}

// attendeeScope is the scope an attendee write takes: an occurrence id answers
// for that occurrence's attendees, which are its override's once it has been
// edited, and any other id for the series'.
func attendeeScope(occurrenceDate string) string {
	if occurrenceDate != "" {
		return scopeThis
	}
	return scopeAll
}

// lockAttendee reads one live attendee row under its row lock.
func lockAttendee(ctx context.Context, q *generated.Queries, eventID, userID uint32) (generated.CalendarEventAttendee, error) {
	att, err := q.GetEventAttendeeForUpdate(ctx, generated.GetEventAttendeeForUpdateParams{
		EventID: sql.NullInt32{Int32: int32(eventID), Valid: true},
		UserID:  userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarEventAttendee{}, apierrors.EventNotAttendee
	}
	return att, err
}

// logAttendeeChange records an answer or an edit grant changing, and gives the
// series a new revision the way every other write to the event does: the
// attendee list is part of what a copy held for a later save says.
func logAttendeeChange(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess, userPubID []byte, field string, value any) error {
	if err := q.TouchCalendarEvent(ctx, w.evt.ID); err != nil {
		return err
	}
	extra := map[string]any{
		"event": pubIDToHex(w.evt.PublicID),
		"user":  pubIDToHex(userPubID),
		field:   value,
	}
	if w.scope.occurrence != nil {
		extra["originalStart"] = w.scope.occurrence.StartAt.Format(time.RFC3339)
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  w.cal.ID,
		ActorUserID: actorID,
		Type:        eventlog.TypeAttendeeUpdated,
		Summary:     w.evt.Title,
		Subject:     w.evt.PublicID,
		Extra:       extra,
	})
}
