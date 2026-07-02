package calresolve

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

type Querier interface {
	GetCalendarByPublicID(ctx context.Context, publicID []byte) (generated.Calendar, error)
	GetCalendarMember(ctx context.Context, arg generated.GetCalendarMemberParams) (generated.CalendarMember, error)
}

func PublicIDString(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

func Member(ctx context.Context, q Querier, calendarPublicID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	pubBytes, err := parseUUID(calendarPublicID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
	}
	cal, err := q.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.InternalUnexpected
	}
	member, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	})
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarAccessDenied
	}
	return cal, member, nil
}

func Read(ctx context.Context, q Querier, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, _, err := Member(ctx, q, calendarPublicID, userID)
	return cal, err
}

func Write(ctx context.Context, q Querier, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := Member(ctx, q, calendarPublicID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if member.Role == generated.CalendarMembersRoleViewer {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

func Admin(ctx context.Context, q Querier, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := Member(ctx, q, calendarPublicID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if member.Role != generated.CalendarMembersRoleAdmin {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}
