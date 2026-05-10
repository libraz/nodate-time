package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

type ResetDeps struct {
	DB      *sql.DB
	Queries *generated.Queries
	Mailer  mailer.Mailer
	WebURL  string
}

func RequestPasswordReset(deps ResetDeps) func(context.Context, *RequestResetInput) (*RequestResetOutput, error) {
	return func(ctx context.Context, in *RequestResetInput) (*RequestResetOutput, error) {
		out := &RequestResetOutput{}
		out.Body.OK = true

		user, err := deps.Queries.GetUserByEmail(ctx, in.Body.Email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return out, nil
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		token, hash, err := auth.GenerateResetToken()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		expiresAt := time.Now().Add(1 * time.Hour)

		if _, err := deps.Queries.CreatePasswordReset(ctx, generated.CreatePasswordResetParams{
			UserID:    user.ID,
			TokenHash: hash,
			ExpiresAt: expiresAt,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		resetURL := fmt.Sprintf("%s/reset-password?token=%s", deps.WebURL, token)
		body := fmt.Sprintf(
			"Hello %s,\n\nA password reset was requested for your account. "+
				"This link expires in 1 hour:\n\n%s\n\nIf you did not request this, ignore this email.",
			user.Name, resetURL,
		)
		_ = deps.Mailer.Send(ctx, mailer.Message{
			To:      user.Email,
			Subject: "Reset your Nodate Time password",
			Text:    body,
		})

		return out, nil
	}
}

func ConfirmPasswordReset(deps ResetDeps) func(context.Context, *ConfirmResetInput) (*ConfirmResetOutput, error) {
	return func(ctx context.Context, in *ConfirmResetInput) (*ConfirmResetOutput, error) {
		hash := auth.HashResetToken(in.Body.Token)
		newHash, err := auth.HashPassword(in.Body.NewPassword)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		q := generated.New(tx)

		row, err := q.GetPasswordResetByTokenHash(ctx, hash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.AuthResetInvalid)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if row.UsedAt.Valid || time.Now().After(row.ExpiresAt) {
			return nil, apierrors.ToHuma(apierrors.AuthResetInvalid)
		}

		if err := q.UpdateUserPassword(ctx, generated.UpdateUserPasswordParams{
			PasswordHash: newHash,
			ID:           row.UserID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := q.MarkPasswordResetUsed(ctx, row.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ConfirmResetOutput{}
		out.Body.OK = true
		return out, nil
	}
}
