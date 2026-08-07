// Package calresolve turns a calendar's public id into an internal id and
// checks the caller's grant, in one step.
//
// The two halves stay in the same function on purpose. Splitting them --
// a lookup here, a permission check there -- is how an authorization check
// goes missing: not because someone decides to skip it, but because the
// call sites drift apart and one of them ends up doing only the lookup.
// Every handler resolves through these functions, so there is no path that
// reaches a calendar id without having proved access to it.
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
	GetCalendarByPublicID(ctx context.Context, arg generated.GetCalendarByPublicIDParams) (generated.Calendar, error)
	GetCalendarMember(ctx context.Context, arg generated.GetCalendarMemberParams) (generated.CalendarMember, error)
}

func PublicIDString(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

// Member resolves the calendar and the caller's grant on it. Membership is
// read from calendar_members, which is the access axis; a display
// preference row grants nothing and is never consulted here.
func Member(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	pubBytes, err := parseUUID(calendarPublicID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
	}
	cal, err := q.GetCalendarByPublicID(ctx, generated.GetCalendarByPublicIDParams{
		WorkspaceID: workspaceID,
		PublicID:    pubBytes,
	})
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
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarAccessDenied
		}
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.InternalUnexpected
	}
	return cal, member, nil
}

// Read admits any member.
func Read(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, _, err := Member(ctx, q, workspaceID, calendarPublicID, userID)
	return cal, err
}

// Write admits everyone above viewer: editor, manager and owner may change
// calendar contents.
func Write(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := Member(ctx, q, workspaceID, calendarPublicID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if !CanWrite(member.Role) {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

// Manage admits manager and owner, who may change membership and calendar
// settings rather than just its contents.
func Manage(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, _, err := ManageMember(ctx, q, workspaceID, calendarPublicID, userID)
	return cal, err
}

// ManageMember is Manage, handing back the caller's membership as well, for
// handlers that report the caller's own standing on the calendar in their
// response. The check stays here rather than at the call site so asking for
// the role does not cost the caller the gate that comes with it.
func ManageMember(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	cal, member, err := Member(ctx, q, workspaceID, calendarPublicID, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, err
	}
	if !CanManage(member.Role) {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarRoleRequired
	}
	return cal, member, nil
}

// Own admits only the owner. Managing a calendar and owning it are not the
// same power: a manager runs the membership list, but handing out ownership
// or destroying the calendar for everyone on it is the owner's alone.
func Own(ctx context.Context, q Querier, workspaceID uint32, calendarPublicID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := Member(ctx, q, workspaceID, calendarPublicID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if !CanOwn(member.Role) {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

// CanWrite reports whether a role may change calendar contents.
func CanWrite(role generated.CalendarMembersRole) bool {
	switch role {
	case generated.CalendarMembersRoleOwner,
		generated.CalendarMembersRoleManager,
		generated.CalendarMembersRoleEditor:
		return true
	default:
		return false
	}
}

// CanManage reports whether a role may change membership and settings.
func CanManage(role generated.CalendarMembersRole) bool {
	switch role {
	case generated.CalendarMembersRoleOwner, generated.CalendarMembersRoleManager:
		return true
	default:
		return false
	}
}

// CanOwn reports whether a role may grant or revoke ownership and delete the
// calendar. Only the owner qualifies -- a manager that could promote itself,
// or a second account it controls, to owner would make the distinction
// between the two roles decorative.
func CanOwn(role generated.CalendarMembersRole) bool {
	return role == generated.CalendarMembersRoleOwner
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

// SplitCompositeID splits a recurring-instance id ("uuid_YYYYMMDD") into its
// parent event UUID and occurrence date. Returns an empty parentUUID if id is
// not in that composite form (a plain UUID, or a foreign string that will
// simply fail uuid.Parse downstream).
func SplitCompositeID(id string) (parentUUID string, dateStr string) {
	// UUID is 36 chars, separator is "_", date is 8 chars = 45 total.
	if len(id) == 45 && id[36] == '_' {
		return id[:36], id[37:]
	}
	return "", ""
}
