package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
)

// personalCalendarColor is the colour a member's personal calendar, their
// grant on it and their subscription to it start with, so the three rows that
// describe one calendar agree.
const personalCalendarColor = "#4285F4"

// personalCalendarFallbackName names a personal calendar whose owner has no
// display name.
const personalCalendarFallbackName = "My Calendar"

// mysqlDuplicateKey is ER_DUP_ENTRY.
const mysqlDuplicateKey = 1062

// Membership is what Join found or wrote.
type Membership struct {
	// Joined reports that this call added the user.
	Joined bool
	// Enabled reports whether the user belongs to the workspace now. False
	// means an administrator removed them and Join left the removal alone.
	Enabled bool
	// Role is the role a joining user was given; empty when Joined is false.
	Role generated.WorkspaceMembersRole
}

// Join adds userID to the workspace the way every product on this database
// does (sql/core/PROTOCOL.md, Identity rule 9): the membership, the member's
// personal calendar with its owner grant and subscription, and the
// workspace.member.added event, all through q, which must be bound to the
// caller's transaction. The first user to join a workspace nobody owns becomes
// its owner.
//
// A membership row that already exists is never touched. An enabled one needs
// nothing; a disabled one is a removal, and only an explicit addition may undo
// it -- joining by signing in is not one (rule 8).
//
// Join must come before the transaction's first write that references the
// workspace: on an ownerless workspace it takes the workspace row's exclusive
// lock, and asking for it while already holding the shared lock a foreign-key
// check takes is how two such transactions deadlock.
func Join(ctx context.Context, q *generated.Queries, workspaceID, userID uint32) (Membership, error) {
	existing, err := q.GetWorkspaceMembership(ctx, generated.GetWorkspaceMembershipParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	switch {
	case err == nil:
		return Membership{Enabled: existing.Enabled}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Membership{}, fmt.Errorf("workspace: find membership: %w", err)
	}

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return Membership{}, fmt.Errorf("workspace: load user: %w", err)
	}
	role, err := joiningRole(ctx, q, workspaceID)
	if err != nil {
		return Membership{}, err
	}

	memberPubID, err := uuid.NewV7()
	if err != nil {
		return Membership{}, fmt.Errorf("workspace: member id: %w", err)
	}
	if err := q.InsertWorkspaceMember(ctx, generated.InsertWorkspaceMemberParams{
		PublicID:    memberPubID[:],
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
	}); err != nil {
		// A concurrent sign-in of the same user joined first. Whatever it
		// wrote is the membership; this call has nothing left to add.
		if isDuplicateKey(err) {
			return Membership{Enabled: true}, nil
		}
		return Membership{}, fmt.Errorf("workspace: insert member: %w", err)
	}

	createdCalendar, err := ensurePersonalCalendar(ctx, q, workspaceID, user)
	if err != nil {
		return Membership{}, err
	}

	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: workspaceID,
		ActorUserID: userID,
		Type:        eventlog.TypeWorkspaceMemberAdded,
		Extra: map[string]any{
			"userId":          eventlog.UUID(user.PublicID),
			"role":            string(role),
			"reenabled":       false,
			"createdCalendar": createdCalendar,
		},
	}); err != nil {
		return Membership{}, err
	}
	return Membership{Joined: true, Enabled: true, Role: role}, nil
}

// joiningRole is owner for the first member of a workspace nobody owns and
// member otherwise. The unlocked count answers almost every join; only an
// ownerless workspace is locked and asked again, so two first joins cannot
// both become its owner.
func joiningRole(ctx context.Context, q *generated.Queries, workspaceID uint32) (generated.WorkspaceMembersRole, error) {
	owners, err := q.CountWorkspaceOwners(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("workspace: count owners: %w", err)
	}
	if owners > 0 {
		return generated.WorkspaceMembersRoleMember, nil
	}
	if _, err := q.LockWorkspace(ctx, workspaceID); err != nil {
		return "", fmt.Errorf("workspace: lock workspace: %w", err)
	}
	owners, err = q.CountWorkspaceOwnersForUpdate(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("workspace: recount owners: %w", err)
	}
	if owners > 0 {
		return generated.WorkspaceMembersRoleMember, nil
	}
	return generated.WorkspaceMembersRoleOwner, nil
}

// ensurePersonalCalendar gives the member their personal calendar, creating it
// and its subscription when they have none, and the owner grant either way: a
// calendar without the grant is one nobody can reach.
func ensurePersonalCalendar(ctx context.Context, q *generated.Queries, workspaceID uint32, user generated.User) (bool, error) {
	userID := user.ID
	owner := sql.NullInt32{Int32: int32(userID), Valid: true} //#nosec G115 -- users.id is INT UNSIGNED and fits the generated column type
	created := false
	calID, err := q.GetPersonalCalendar(ctx, generated.GetPersonalCalendarParams{
		WorkspaceID: workspaceID,
		OwnerUserID: owner,
	})
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		name := user.DisplayName
		if name == "" {
			name = personalCalendarFallbackName
		}
		calPubID, err := uuid.NewV7()
		if err != nil {
			return false, fmt.Errorf("workspace: calendar id: %w", err)
		}
		res, err := q.CreatePersonalCalendar(ctx, generated.CreatePersonalCalendarParams{
			PublicID:    calPubID[:],
			WorkspaceID: workspaceID,
			Name:        name,
			Color:       personalCalendarColor,
			OwnerUserID: owner,
		})
		if err != nil {
			return false, fmt.Errorf("workspace: create personal calendar: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return false, fmt.Errorf("workspace: personal calendar id: %w", err)
		}
		calID = uint32(id) //#nosec G115 -- calendars.id is INT UNSIGNED
		subPubID, err := uuid.NewV7()
		if err != nil {
			return false, fmt.Errorf("workspace: subscription id: %w", err)
		}
		if err := q.CreateCalendarSubscription(ctx, generated.CreateCalendarSubscriptionParams{
			PublicID:     subPubID[:],
			WorkspaceID:  workspaceID,
			CalendarID:   calID,
			UserID:       userID,
			DisplayColor: personalCalendarColor,
		}); err != nil {
			return false, fmt.Errorf("workspace: create subscription: %w", err)
		}
		created = true
	default:
		return false, fmt.Errorf("workspace: find personal calendar: %w", err)
	}

	grantPubID, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("workspace: grant id: %w", err)
	}
	if err := q.GrantPersonalCalendarOwnership(ctx, generated.GrantPersonalCalendarOwnershipParams{
		PublicID:    grantPubID[:],
		WorkspaceID: workspaceID,
		CalendarID:  calID,
		UserID:      userID,
		MemberColor: personalCalendarColor,
	}); err != nil {
		return false, fmt.Errorf("workspace: grant personal calendar: %w", err)
	}
	return created, nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateKey
}
