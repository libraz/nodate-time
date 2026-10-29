// Package calresolve turns a calendar's public id into an internal id and
// checks the caller's grant, in one step.
//
// The two halves stay in the same function on purpose. Splitting them --
// a lookup here, a permission check there -- is how an authorization check
// goes missing: not because someone decides to skip it, but because the
// call sites drift apart and one of them ends up doing only the lookup.
// Every handler resolves through these functions, so there is no path that
// reaches a calendar id without having proved access to it.
//
// It also owns the order a write locks a calendar's rows in: LockShared,
// LockExclusive and LockEvent are the first statement of every such transaction.
// A write that decides on the caller's grant re-reads it there, under lock,
// through LockMember and its siblings.
package calresolve

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"

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
// preference row grants nothing and is never consulted here. The grant
// counts only while the caller still belongs to the calendar's workspace,
// which GetCalendarMember checks in the same statement: a removal another
// application made ends access here too (sql/core/PROTOCOL.md, Identity
// rule 8).
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

// LockShared is the first statement of every transaction that writes under a
// calendar: its events, the rows hanging off them, its photos, memos and
// members. It fails with CalendarNotFound once the calendar is gone, so a write
// that lost the race with a delete adds nothing under it.
//
// Locks are taken top down -- calendar, event, the rows under the event,
// stored objects. The activity log's foreign key takes this same shared lock at
// the end of the transaction anyway; taking it first is what puts it in order.
func LockShared(ctx context.Context, q *generated.Queries, calendarID uint32) error {
	if _, err := q.LockCalendarForShare(ctx, calendarID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apierrors.CalendarNotFound
		}
		return err
	}
	return nil
}

// LockExclusive is the first statement of a transaction that writes the
// calendar row itself, such as deleting it.
func LockExclusive(ctx context.Context, q *generated.Queries, calendarID uint32) error {
	cal, err := q.GetCalendarByIDForUpdate(ctx, calendarID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !cal.Enabled) {
		return apierrors.CalendarNotFound
	}
	return err
}

// LockEvent takes the calendar's shared lock and then the event's exclusive
// one: the first step of every write to an event or a row hanging off it. It
// fails with EventNotFound once the event is gone.
func LockEvent(ctx context.Context, q *generated.Queries, calendarID, eventID uint32) (generated.CalendarEvent, error) {
	if err := LockShared(ctx, q, calendarID); err != nil {
		return generated.CalendarEvent{}, err
	}
	return lockEventRow(ctx, q, eventID)
}

func lockEventRow(ctx context.Context, q *generated.Queries, eventID uint32) (generated.CalendarEvent, error) {
	evt, err := q.GetCalendarEventByIDForUpdate(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarEvent{}, apierrors.EventNotFound
	}
	return evt, err
}

// LockMember is LockShared followed by the caller's membership, read under its
// row lock in the same transaction. A write decides the caller's role from
// this copy, never from the one Member read before the transaction began: a
// member removed or demoted in between would otherwise still write with the
// grant they no longer hold. It fails with CalendarAccessDenied once the
// membership is gone.
//
// The membership is locked after the calendar and before any event, which is
// the order a member's removal takes them in as well.
func LockMember(ctx context.Context, q *generated.Queries, calendarID, userID uint32) (generated.CalendarMember, error) {
	if err := LockShared(ctx, q, calendarID); err != nil {
		return generated.CalendarMember{}, err
	}
	member, err := q.GetCalendarMemberForUpdate(ctx, generated.GetCalendarMemberForUpdateParams{
		CalendarID: calendarID,
		UserID:     userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarMember{}, apierrors.CalendarAccessDenied
	}
	return member, err
}

// LockWrite is LockMember admitting the roles Write admits.
func LockWrite(ctx context.Context, q *generated.Queries, calendarID, userID uint32) (generated.CalendarMember, error) {
	member, err := LockMember(ctx, q, calendarID, userID)
	if err != nil {
		return generated.CalendarMember{}, err
	}
	if !CanWrite(member.Role) {
		return generated.CalendarMember{}, apierrors.CalendarRoleRequired
	}
	return member, nil
}

// LockManage is LockMember admitting the roles Manage admits.
func LockManage(ctx context.Context, q *generated.Queries, calendarID, userID uint32) (generated.CalendarMember, error) {
	member, err := LockMember(ctx, q, calendarID, userID)
	if err != nil {
		return generated.CalendarMember{}, err
	}
	if !CanManage(member.Role) {
		return generated.CalendarMember{}, apierrors.CalendarRoleRequired
	}
	return member, nil
}

// LockMembers is LockShared followed by several memberships under their row
// locks, for a write that decides on the caller's grant and another member's
// row at once. They are locked in ascending user id, so two writers naming
// the same pair the other way round queue instead of deadlocking. A user with
// no live membership is absent from the result; what that means is the
// caller's to say.
func LockMembers(ctx context.Context, q *generated.Queries, calendarID uint32, userIDs ...uint32) (map[uint32]generated.CalendarMember, error) {
	if err := LockShared(ctx, q, calendarID); err != nil {
		return nil, err
	}
	ordered := slices.Clone(userIDs)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	members := make(map[uint32]generated.CalendarMember, len(ordered))
	for _, userID := range ordered {
		member, err := q.GetCalendarMemberForUpdate(ctx, generated.GetCalendarMemberForUpdateParams{
			CalendarID: calendarID,
			UserID:     userID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		members[userID] = member
	}
	return members, nil
}

// LockEventMember is LockMember followed by the event's exclusive lock: the
// first step of a write to an event that decides on the caller's grant.
func LockEventMember(ctx context.Context, q *generated.Queries, calendarID, eventID, userID uint32) (generated.CalendarMember, generated.CalendarEvent, error) {
	member, err := LockMember(ctx, q, calendarID, userID)
	if err != nil {
		return generated.CalendarMember{}, generated.CalendarEvent{}, err
	}
	evt, err := lockEventRow(ctx, q, eventID)
	if err != nil {
		return generated.CalendarMember{}, generated.CalendarEvent{}, err
	}
	return member, evt, nil
}
