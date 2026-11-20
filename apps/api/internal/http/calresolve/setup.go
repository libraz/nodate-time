package calresolve

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
)

// DefaultCalendarColor is applied when a caller names no colour.
const DefaultCalendarColor = "#4CAF50"

// SetUp creates a calendar with ownerUserID as its owning member and records
// the event-log entry. It must run inside a transaction; an empty color means
// DefaultCalendarColor.
func SetUp(ctx context.Context, q *generated.Queries, workspaceID, ownerUserID uint32,
	name, color string) (generated.Calendar, error) {
	if color == "" {
		color = DefaultCalendarColor
	}
	pubID, err := uuid.NewV7()
	if err != nil {
		return generated.Calendar{}, err
	}
	memberPubID, err := uuid.NewV7()
	if err != nil {
		return generated.Calendar{}, err
	}

	// owner_user_id is deliberately left NULL. The column cascades on
	// delete, so naming an owner would mean removing that one person
	// deletes the calendar and every event in it -- which is the
	// opposite of what a calendar a group shares is for. Who may
	// administer it is a calendar_members role, not this column.
	result, err := q.CreateCalendar(ctx, generated.CreateCalendarParams{
		PublicID:    pubID[:],
		WorkspaceID: workspaceID,
		Name:        name,
		Color:       color,
	})
	if err != nil {
		return generated.Calendar{}, err
	}
	calID64, err := result.LastInsertId()
	if err != nil {
		return generated.Calendar{}, err
	}
	calID := uint32(calID64)

	// The creator becomes the owning member, which is what actually
	// grants them administration.
	if _, err := q.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
		PublicID:    memberPubID[:],
		WorkspaceID: workspaceID,
		CalendarID:  calID,
		UserID:      ownerUserID,
		Role:        generated.CalendarMembersRoleOwner,
		MemberColor: color,
	}); err != nil {
		return generated.Calendar{}, err
	}

	created, err := q.GetCalendarByID(ctx, calID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: workspaceID,
		CalendarID:  calID,
		ActorUserID: ownerUserID,
		Type:        eventlog.TypeCalendarSetUp,
		Summary:     name,
		Extra:       map[string]any{"calendarId": eventlog.UUID(pubID[:])},
	}); err != nil {
		return generated.Calendar{}, err
	}
	return created, nil
}

// AddMember grants a workspace member access to cal directly. It must run
// inside a transaction, and returns the colour the member was given. The role
// is not restricted here; a boundary that limits what may be granted does so
// itself.
func AddMember(ctx context.Context, q *generated.Queries, cal generated.Calendar,
	actorUserID uint32, target generated.User, role generated.CalendarMembersRole) (color string, err error) {
	members, err := LockMembers(ctx, q, cal.ID, actorUserID, target.ID)
	if err != nil {
		return "", err
	}
	actorMember, ok := members[actorUserID]
	if !ok {
		return "", apierrors.CalendarAccessDenied
	}
	if !CanManage(actorMember.Role) {
		return "", apierrors.CalendarRoleRequired
	}
	if _, ok := members[target.ID]; ok {
		return "", apierrors.MemberAlreadyAdded
	}
	// Only someone already in the workspace can be added this way; a
	// removed workspace member is not brought back through a calendar
	// (sql/core/PROTOCOL.md, Identity rule 8). Whether the person
	// exists at all is not disclosed separately.
	if _, err := q.GetWorkspaceMember(ctx, generated.GetWorkspaceMemberParams{
		WorkspaceID: cal.WorkspaceID,
		UserID:      target.ID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", apierrors.MemberNotFound
		}
		return "", err
	}
	return Grant(ctx, q, GrantInput{
		Calendar:  cal,
		User:      target,
		Role:      role,
		Actor:     actorUserID,
		InvitedBy: actorUserID,
	})
}
