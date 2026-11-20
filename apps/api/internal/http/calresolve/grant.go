package calresolve

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/memberpalette"
)

// colorSampleSize bounds how many current members are read to pick a new
// member's colour; past it every palette colour is long taken and the pick
// only evens out.
const colorSampleSize = 500

// GrantInput describes one new calendar membership.
type GrantInput struct {
	Calendar generated.Calendar
	User     generated.User
	Role     generated.CalendarMembersRole
	// Actor is who performed the change: the joiner for a link, the manager
	// for a direct addition.
	Actor uint32
	// InvitedBy is who extended the access: the link's creator, or the
	// manager who added the member.
	InvitedBy uint32
}

// Grant adds a member inside the caller's transaction and records it. The
// caller holds the calendar lock, has confirmed the user is in the calendar's
// workspace (sql/core/PROTOCOL.md, Identity rule 8) and is not already an
// enabled member. A revoked grant is revived rather than duplicated.
func Grant(ctx context.Context, q *generated.Queries, in GrantInput) (string, error) {
	// A new member starts on the colour fewest members carry, so a calendar
	// filled one person at a time does not draw everyone alike.
	current, err := q.ListCalendarMembers(ctx, generated.ListCalendarMembersParams{
		CalendarID: in.Calendar.ID,
		Limit:      colorSampleSize,
	})
	if err != nil {
		return "", err
	}
	used := make([]string, len(current))
	for i, m := range current {
		used[i] = m.MemberColor
	}
	color := memberpalette.Pick(used)

	pubID, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	if _, err := q.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
		PublicID:        pubID[:],
		WorkspaceID:     in.Calendar.WorkspaceID,
		CalendarID:      in.Calendar.ID,
		UserID:          in.User.ID,
		Role:            in.Role,
		MemberColor:     color,
		InvitedByUserID: sql.NullInt32{Int32: int32(in.InvitedBy), Valid: true},
	}); err != nil {
		return "", err
	}

	summary := string(in.Role)
	if in.User.DisplayName != "" {
		summary = in.User.DisplayName + " -> " + string(in.Role)
	}
	err = eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: in.Calendar.WorkspaceID,
		CalendarID:  in.Calendar.ID,
		ActorUserID: in.Actor,
		Type:        eventlog.TypeMemberAdded,
		Summary:     summary,
		Extra: map[string]any{
			"calendarId": eventlog.UUID(in.Calendar.PublicID),
			"userId":     eventlog.UUID(in.User.PublicID),
			"role":       string(in.Role),
		},
	})
	return color, err
}
