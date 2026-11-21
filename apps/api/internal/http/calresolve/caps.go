package calresolve

import (
	"context"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// MaxMembers bounds the people a calendar holds at once, and MaxCalendars the
// calendars one person belongs to. Every path that creates a membership checks
// both under the calendar's exclusive lock, so the lists they bound are
// returned whole.
const (
	MaxMembers   = 500
	MaxCalendars = 500
)

// RequireRoomForMember refuses a membership past [MaxMembers].
func RequireRoomForMember(ctx context.Context, q *generated.Queries, calendarID uint32) error {
	held, err := q.CountCalendarMembers(ctx, calendarID)
	if err != nil {
		return err
	}
	if held >= MaxMembers {
		return apierrors.MemberLimitReached
	}
	return nil
}

// RequireRoomForCalendar refuses a membership past [MaxCalendars] for the user.
func RequireRoomForCalendar(ctx context.Context, q *generated.Queries, workspaceID, userID uint32) error {
	held, err := q.CountUserCalendars(ctx, generated.CountUserCalendarsParams{UserID: userID, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if held >= MaxCalendars {
		return apierrors.CalendarLimitReached
	}
	return nil
}
