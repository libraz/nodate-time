package admin

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// --- DTOs ---

// InstanceAdmin is one live administrator grant.
//
// The generated row carries three internal ids -- the grant's, the user's and
// the granter's -- and none of them are here. UserID is the user's public id
// because that is what the revoke route takes; the grant's own public id is
// left out rather than served as a second id no operation is keyed on.
//
// Who granted the rights is not reported: the query returns only the granter's
// internal id, and answering with that would break the same rule this type
// exists to keep.
type InstanceAdmin struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	GrantedAt   time.Time `json:"grantedAt"`
}

type ListInstanceAdminsInput struct{}

type ListInstanceAdminsOutput struct {
	Body struct {
		Admins []InstanceAdmin `json:"admins"`
	}
}

// RevokeInstanceAdminInput names the user losing the rights by their public id,
// which is what the listing serves.
type RevokeInstanceAdminInput struct {
	UserID string `path:"userId"`
}

type RevokeInstanceAdminOutput struct{}

// --- handlers ---

// ListInstanceAdmins answers who can currently administer this instance.
//
// The grant table is deliberately not workspace-scoped, so this is every
// administrator on the deployment rather than every administrator of one
// workspace.
func ListInstanceAdmins(deps Deps) func(context.Context, *ListInstanceAdminsInput) (*ListInstanceAdminsOutput, error) {
	return func(ctx context.Context, _ *ListInstanceAdminsInput) (*ListInstanceAdminsOutput, error) {
		rows, err := deps.Queries.ListInstanceAdmins(ctx)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		out := &ListInstanceAdminsOutput{}
		out.Body.Admins = make([]InstanceAdmin, 0, len(rows))
		for _, r := range rows {
			out.Body.Admins = append(out.Body.Admins, InstanceAdmin{
				UserID:      calresolve.PublicIDString(r.UserPublicID),
				Email:       r.Email,
				DisplayName: r.DisplayName,
				GrantedAt:   r.GrantedAt,
			})
		}
		return out, nil
	}
}

// RevokeInstanceAdmin takes an administrator's rights away.
//
// Two guards keep the instance administrable, because neither is sufficient on
// its own. Refusing self-revocation catches the accident anyone would actually
// have -- an administrator clicking remove on their own row -- but two
// administrators removing each other at the same moment are each removing
// somebody else, and between them they remove everybody. So the count is also
// locked and re-read inside the transaction that does the write: the second of
// those two revocations then sees one administrator left and is refused.
//
// The instance mattering here is the whole deployment, not a workspace: the
// grant table is deliberately not workspace-scoped.
func RevokeInstanceAdmin(deps Deps) func(context.Context, *RevokeInstanceAdminInput) (*RevokeInstanceAdminOutput, error) {
	return func(ctx context.Context, in *RevokeInstanceAdminInput) (*RevokeInstanceAdminOutput, error) {
		pub, err := uuid.Parse(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.NotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, pub[:])
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.NotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		actorID, _ := middleware.ActorFromContext(ctx)
		if actorID == target.ID {
			return nil, apierrors.ToHuma(apierrors.AdminSelfRevoke)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Lock first, then read: the count and the decision it feeds have
			// to describe the same moment as the write.
			remaining, err := q.CountInstanceAdminsForUpdate(ctx)
			if err != nil {
				return err
			}
			isAdmin, err := q.IsInstanceAdmin(ctx, target.ID)
			if err != nil {
				return err
			}
			// Report a miss rather than a silent success: an operator who
			// believes they removed somebody's access and did not has live
			// rights everyone thinks are gone.
			if !isAdmin {
				return apierrors.NotFound
			}
			if remaining <= 1 {
				return apierrors.AdminLastInstanceAdmin
			}
			return q.RevokeInstanceAdmin(ctx, target.ID)
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &RevokeInstanceAdminOutput{}, nil
	}
}
