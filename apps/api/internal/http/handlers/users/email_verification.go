package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

// emailVerificationTTL is deliberately long: unlike a password reset, nothing
// is compromised by an unredeemed confirmation link sitting in an inbox, and a
// short window would strand anyone who registers and reads their mail later.
const emailVerificationTTL = 7 * 24 * time.Hour

// errNoMailer reports a deployment with nothing to send a confirmation with.
var errNoMailer = errors.New("no mailer configured")

// errMailNotAccepted wraps a mailer's refusal, which a caller can answer as a
// busy service rather than a fault.
var errMailNotAccepted = errors.New("mail not accepted")

// sendEmailVerification stores a confirmation link for user's current address
// through q and hands it to the mailer. q decides the transaction the link is
// stored in, so a caller can undo the store when the mailer refuses.
func sendEmailVerification(ctx context.Context, deps Deps, q *generated.Queries, user generated.User) error {
	if deps.Mailer == nil {
		return errNoMailer
	}

	token, hash, err := auth.GenerateResetToken()
	if err != nil {
		return fmt.Errorf("mint email verification token: %w", err)
	}
	pubID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("mint email verification id: %w", err)
	}
	if _, err := q.CreateEmailVerification(ctx, generated.CreateEmailVerificationParams{
		PublicID:  pubID[:],
		UserID:    user.ID,
		Email:     user.Email,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(emailVerificationTTL),
	}); err != nil {
		return fmt.Errorf("store email verification: %w", err)
	}

	link := fmt.Sprintf("%s/verify-email?token=%s", deps.WebURL, token)
	body := fmt.Sprintf(
		"Hello %s,\n\nConfirm this address to finish setting up your %s account. "+
			"This link expires in 7 days:\n\n%s\n\nIf you did not create this account, ignore this email.",
		user.DisplayName, deps.AppName, link,
	)
	if err := deps.Mailer.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: fmt.Sprintf("Confirm your %s email address", deps.AppName),
		Text:    body,
	}); err != nil {
		return fmt.Errorf("%w: %w", errMailNotAccepted, err)
	}
	return nil
}

// ResendEmailVerification issues a fresh confirmation link for the signed-in
// user. Outstanding links are invalidated in the same transaction that stores
// the new one, and only once the mailer has accepted it: a failure leaves the
// previous link working and is reported rather than answered with ok.
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

		// The confirmation has a budget of its own: asking for password resets
		// must not silence the button the account offers for its own address.
		// And since the caller is signed in and asking about that address,
		// there is nobody to keep the answer uniform for -- being told it
		// worked when nothing was sent leaves them pressing it again.
		clientIP, _ := middleware.ClientIPFromContext(ctx)
		if !allowEmailSend(mailPurposeVerify, user.Email, clientIP, time.Now()) {
			slog.WarnContext(ctx, "email verification resend suppressed by rate limiter", "userID", user.ID)
			return nil, apierrors.ToHuma(apierrors.RateLimited)
		}
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.InvalidateUserEmailVerifications(ctx, user.ID); err != nil {
				return err
			}
			return sendEmailVerification(ctx, deps, q, user)
		})
		if err != nil {
			slog.ErrorContext(ctx, "email verification resend failed", "userID", user.ID, "error", err)
			if errors.Is(err, errMailNotAccepted) {
				return nil, apierrors.ToHuma(apierrors.RateLimited)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return out, nil
	}
}

// holdsSessionOf reports whether authorization is a bearer token for a live
// session of userID. Anything else, including no header, is not.
func holdsSessionOf(ctx context.Context, q *generated.Queries, jwtSecret, authorization string, userID uint32) bool {
	tok, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok {
		return false
	}
	claims, err := auth.ValidateToken(tok, jwtSecret)
	if err != nil {
		return false
	}
	sessionPub, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return false
	}
	session, err := q.GetLiveSession(ctx, sessionPub[:])
	return err == nil && session.UserID == userID
}

// revokePriorCredentials ends every session and refresh token of userID and
// clears its local password. The identity row stays, so a password reset can
// set a new one.
func revokePriorCredentials(ctx context.Context, q *generated.Queries, userID uint32) error {
	if err := q.UpdatePasswordHash(ctx, generated.UpdatePasswordHashParams{UserID: userID}); err != nil {
		return err
	}
	return q.RevokeAllUserSessions(ctx, userID)
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
		verified, err := q.MarkUserEmailVerified(ctx, generated.MarkUserEmailVerifiedParams{
			ID:    row.UserID,
			Email: row.Email,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// Whoever registered the address may not be whoever reads it. Proof
		// from outside the account's own sessions hands the account to the
		// mailbox: the registrant's password and sessions end here.
		if n, err := verified.RowsAffected(); err == nil && n == 1 &&
			!holdsSessionOf(ctx, q, deps.JWTSecret, in.Authorization, row.UserID) {
			if err := revokePriorCredentials(ctx, q, row.UserID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
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
