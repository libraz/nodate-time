package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// --- DTOs ---

type AllowedEmail struct {
	ID        uint32    `json:"id"`
	Email     string    `json:"email"`
	Note      string    `json:"note"`
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
		Note  string `json:"note" maxLength:"255"`
	}
}

type CreateAllowedEmailOutput struct {
	Body AllowedEmail
}

type DeleteAllowedEmailInput struct {
	ID uint32 `path:"id"`
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
				ID:        r.ID,
				Email:     r.Email,
				Note:      r.Note,
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

		res, err := deps.Queries.CreateAllowedEmail(ctx, generated.CreateAllowedEmailParams{
			Email:     email,
			Note:      strings.TrimSpace(in.Body.Note),
			CreatedBy: sql.NullInt32{Int32: int32(userID), Valid: userID > 0},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		id, _ := res.LastInsertId()
		return &CreateAllowedEmailOutput{Body: AllowedEmail{
			ID:        uint32(id),
			Email:     email,
			Note:      strings.TrimSpace(in.Body.Note),
			CreatedAt: time.Now(),
		}}, nil
	}
}

func DeleteAllowedEmail(deps Deps) func(context.Context, *DeleteAllowedEmailInput) (*DeleteAllowedEmailOutput, error) {
	return func(ctx context.Context, in *DeleteAllowedEmailInput) (*DeleteAllowedEmailOutput, error) {
		if err := deps.Queries.DeleteAllowedEmail(ctx, in.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteAllowedEmailOutput{}, nil
	}
}
