package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

// emailVerificationTTL is deliberately long: unlike a password reset, nothing
// is compromised by an unredeemed confirmation link sitting in an inbox, and a
// short window would strand anyone who registers and reads their mail later.
const emailVerificationTTL = 7 * 24 * time.Hour

// sendEmailVerification issues a confirmation link for a user's current
// address. Failures are logged and swallowed: the caller is in the middle of a
// registration that has already succeeded, and an undelivered confirmation is
// recoverable by asking for another one.
func sendEmailVerification(ctx context.Context, deps Deps, user generated.User) {
	if deps.Mailer == nil {
		return
	}
	if user.EmailVerifiedAt.Valid {
		return
	}

	token, hash, err := auth.GenerateResetToken()
	if err != nil {
		slog.ErrorContext(ctx, "failed to mint email verification token", "userID", user.ID, "error", err)
		return
	}
	pubID, err := uuid.NewV7()
	if err != nil {
		slog.ErrorContext(ctx, "failed to mint email verification id", "userID", user.ID, "error", err)
		return
	}
	if _, err := deps.Queries.CreateEmailVerification(ctx, generated.CreateEmailVerificationParams{
		PublicID:  pubID[:],
		UserID:    user.ID,
		Email:     user.Email,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(emailVerificationTTL),
	}); err != nil {
		slog.ErrorContext(ctx, "failed to store email verification", "userID", user.ID, "error", err)
		return
	}

	link := fmt.Sprintf("%s/verify-email?token=%s", deps.WebURL, token)
	body := fmt.Sprintf(
		"Hello %s,\n\nConfirm this address to finish setting up your Nodate Time account. "+
			"This link expires in 7 days:\n\n%s\n\nIf you did not create this account, ignore this email.",
		user.DisplayName, link,
	)
	if err := deps.Mailer.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: "Confirm your Nodate Time email address",
		Text:    body,
	}); err != nil {
		slog.ErrorContext(ctx, "failed to send email verification", "userID", user.ID, "error", err)
	}
}

// ResendEmailVerification issues a fresh confirmation link for the signed-in
// user. Outstanding links are invalidated first so only the newest one works.
func ResendEmailVerification(deps Deps) func(context.Context, *ResendVerificationInput) (*ResendVerificationOutput, error) {
	return func(ctx context.Context, _ *ResendVerificationInput) (*ResendVerificationOutput, error) {
		actorUserID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenMissing)
		}
		user, err := deps.Queries.GetUserByID(ctx, actorUserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ResendVerificationOutput{}
		out.Body.OK = true
		if user.EmailVerifiedAt.Valid {
			return out, nil
		}

		clientIP, _ := middleware.ClientIPFromContext(ctx)
		if !allowPasswordResetEmail(user.Email, clientIP, time.Now()) {
			slog.WarnContext(ctx, "email verification resend suppressed by rate limiter", "userID", user.ID)
			return out, nil
		}
		if err := deps.Queries.InvalidateUserEmailVerifications(ctx, user.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		sendEmailVerification(ctx, deps, user)
		return out, nil
	}
}

// ConfirmEmailVerification redeems a confirmation link.
func ConfirmEmailVerification(deps Deps) func(context.Context, *ConfirmVerificationInput) (*ConfirmVerificationOutput, error) {
	return func(ctx context.Context, in *ConfirmVerificationInput) (*ConfirmVerificationOutput, error) {
		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		q := generated.New(tx)

		row, err := q.GetEmailVerificationByTokenHash(ctx, auth.HashResetToken(in.Body.Token))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.AuthVerificationInvalid)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// The address is matched as well as the user: a token minted for an
		// address the account has since moved away from proves nothing about
		// the address it holds now.
		if _, err := q.MarkUserEmailVerified(ctx, generated.MarkUserEmailVerifiedParams{
			ID:    row.UserID,
			Email: row.Email,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		consumed, err := q.MarkEmailVerificationUsed(ctx, row.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if n, err := consumed.RowsAffected(); err != nil || n != 1 {
			return nil, apierrors.ToHuma(apierrors.AuthVerificationInvalid)
		}
		if err := q.InvalidateUserEmailVerifications(ctx, row.UserID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ConfirmVerificationOutput{}
		out.Body.OK = true
		return out, nil
	}
}
