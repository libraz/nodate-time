package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// --- DTOs ---

type AllowedEmail struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListAllowedEmailsInput struct{}

type ListAllowedEmailsOutput struct {
	Body struct {
		// AllowedDomains is the active domain restriction (read-only, from env).
		// Empty means sign-in is unrestricted and the per-email list is unused.
		AllowedDomains []string       `json:"allowedDomains"`
		Restricted     bool           `json:"restricted"`
		Emails         []AllowedEmail `json:"emails"`
	}
}

type CreateAllowedEmailInput struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"255"`
		// Named for the column and for the field the list returns: a value
		// stored under one name and read back under another is never shown.
		Reason string `json:"reason,omitempty" maxLength:"255" required:"false" doc:"why this address is allowed"`
	}
}

type CreateAllowedEmailOutput struct {
	Body AllowedEmail
}

type DeleteAllowedEmailInput struct {
	ID string `path:"id"`
}

type DeleteAllowedEmailOutput struct{}

// --- handlers ---

func ListAllowedEmails(deps Deps) func(context.Context, *ListAllowedEmailsInput) (*ListAllowedEmailsOutput, error) {
	return func(ctx context.Context, _ *ListAllowedEmailsInput) (*ListAllowedEmailsOutput, error) {
		rows, err := deps.Queries.ListAllowedEmails(ctx)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		out := &ListAllowedEmailsOutput{}
		out.Body.AllowedDomains = deps.AllowedDomains
		out.Body.Restricted = len(deps.AllowedDomains) > 0
		out.Body.Emails = make([]AllowedEmail, 0, len(rows))
		for _, r := range rows {
			out.Body.Emails = append(out.Body.Emails, AllowedEmail{
				ID:        calresolve.PublicIDString(r.PublicID),
				Email:     r.Email,
				Reason:    r.Reason,
				CreatedAt: r.CreatedAt,
			})
		}
		return out, nil
	}
}

func CreateAllowedEmail(deps Deps) func(context.Context, *CreateAllowedEmailInput) (*CreateAllowedEmailOutput, error) {
	return func(ctx context.Context, in *CreateAllowedEmailInput) (*CreateAllowedEmailOutput, error) {
		email := strings.ToLower(strings.TrimSpace(in.Body.Email))
		if email == "" {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		exists, err := deps.Queries.IsEmailAllowed(ctx, email)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if exists {
			return nil, apierrors.ToHuma(apierrors.Conflict)
		}

		userID, _ := middleware.ActorFromContext(ctx)

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if _, err := deps.Queries.CreateAllowedEmail(ctx, generated.CreateAllowedEmailParams{
			PublicID:        pubID[:],
			Email:           email,
			Reason:          reason,
			CreatedByUserID: sql.NullInt32{Int32: int32(userID), Valid: userID > 0},
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &CreateAllowedEmailOutput{Body: AllowedEmail{
			ID:        pubID.String(),
			Email:     email,
			Reason:    reason,
			CreatedAt: time.Now(),
		}}, nil
	}
}

func DeleteAllowedEmail(deps Deps) func(context.Context, *DeleteAllowedEmailInput) (*DeleteAllowedEmailOutput, error) {
	return func(ctx context.Context, in *DeleteAllowedEmailInput) (*DeleteAllowedEmailOutput, error) {
		pub, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.NotFound)
		}
		res, err := deps.Queries.WithdrawAllowedEmail(ctx, pub[:])
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// Report a miss rather than a silent success: an operator who thinks
		// they withdrew an exception and did not has a live one they believe
		// is gone.
		if affected, err := res.RowsAffected(); err != nil || affected == 0 {
			return nil, apierrors.ToHuma(apierrors.NotFound)
		}
		return &DeleteAllowedEmailOutput{}, nil
	}
}
