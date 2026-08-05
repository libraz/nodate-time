package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		eventRef := sql.NullInt32{Int32: int32(seriesID(evt)), Valid: true}
		if _, err := deps.Queries.GetEventAttendee(ctx, generated.GetEventAttendeeParams{
			EventID: eventRef,
			UserID:  userID,
		}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := deps.Queries.SetEventAttendeeRsvp(ctx, generated.SetEventAttendeeRsvpParams{
			Rsvp:    generated.CalendarEventAttendeesRsvp(in.Body.Rsvp),
			EventID: eventRef,
			UserID:  userID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &SetRsvpOutput{Body: AttendeeResponse{
			UserID: pubIDToHex(actorPublicID(ctx, deps, userID)),
			Rsvp:   in.Body.Rsvp,
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
		cal, member, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, toAPIError(err)
		}
		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}
		// Delegating is the owner's call, not a delegate's: an attendee that
		// could pass the grant on would make revoking it meaningless.
		if !calresolve.CanManage(member.Role) && evt.OwnerUserID != actorID {
			return nil, apierrors.ToHuma(apierrors.EventEditDenied)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
		}

		eventRef := sql.NullInt32{Int32: int32(seriesID(evt)), Valid: true}
		att, err := deps.Queries.GetEventAttendee(ctx, generated.GetEventAttendeeParams{
			EventID: eventRef,
			UserID:  target.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.EventNotAttendee)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := deps.Queries.SetEventAttendeeCanEdit(ctx, generated.SetEventAttendeeCanEditParams{
			CanEdit: in.Body.CanEdit,
			EventID: eventRef,
			UserID:  target.ID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &SetAttendeeCanEditOutput{Body: AttendeeResponse{
			UserID:  pubIDToHex(target.PublicID),
			Rsvp:    string(att.Rsvp),
			CanEdit: in.Body.CanEdit,
		}}, nil
	}
}

// seriesID returns the row attendees hang off: a changed occurrence keeps its
// participants on the series it belongs to.
func seriesID(evt generated.CalendarEvent) uint32 {
	if evt.RecurrenceParentID.Valid {
		return uint32(evt.RecurrenceParentID.Int32)
	}
	return evt.ID
}

// actorPublicID looks up the caller's public id for echoing back in a
// response. A failed lookup yields an empty id rather than an error: the write
// has already happened, and the caller knows who they are.
func actorPublicID(ctx context.Context, deps Deps, userID uint32) []byte {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return nil
	}
	return u.PublicID
}
