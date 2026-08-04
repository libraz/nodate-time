package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

type ResetDeps struct {
	DB      *sql.DB
	Queries *generated.Queries
	Mailer  mailer.Mailer
	WebURL  string
}

const (
	passwordResetEmailLimit  = 3
	passwordResetEmailWindow = time.Hour
)

type resetEmailBucket struct {
	count       int
	windowStart time.Time
}

var resetEmailLimiter = struct {
	sync.Mutex
	buckets map[string]*resetEmailBucket
}{buckets: map[string]*resetEmailBucket{}}

// allowPasswordResetEmail scopes the send budget to (email, IP) rather than
// email alone: an attacker hammering one IP against a victim's address can no
// longer silently exhaust the victim's own quota for that mailbox.
func allowPasswordResetEmail(email, clientIP string, now time.Time) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	key := email + "|" + clientIP

	resetEmailLimiter.Lock()
	defer resetEmailLimiter.Unlock()

	b, exists := resetEmailLimiter.buckets[key]
	if !exists || now.Sub(b.windowStart) >= passwordResetEmailWindow {
		b = &resetEmailBucket{windowStart: now}
		resetEmailLimiter.buckets[key] = b
		if len(resetEmailLimiter.buckets) > 10000 {
			for k, bb := range resetEmailLimiter.buckets {
				if now.Sub(bb.windowStart) >= passwordResetEmailWindow {
					delete(resetEmailLimiter.buckets, k)
				}
			}
		}
	}
	b.count++
	return b.count <= passwordResetEmailLimit
}

func RequestPasswordReset(deps ResetDeps) func(context.Context, *RequestResetInput) (*RequestResetOutput, error) {
	return func(ctx context.Context, in *RequestResetInput) (*RequestResetOutput, error) {
		out := &RequestResetOutput{}
		out.Body.OK = true

		clientIP, _ := middleware.ClientIPFromContext(ctx)
		if !allowPasswordResetEmail(in.Body.Email, clientIP, time.Now()) {
			// The response stays {ok:true} regardless so this endpoint cannot be
			// used to probe whether an email exists; the suppression itself is
			// still logged so a mail-bombing attempt against a real mailbox is
			// visible to operators even though no mail was sent.
			slog.WarnContext(ctx, "password reset request suppressed by rate limiter", "clientIP", clientIP)
			return out, nil
		}

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

		resetPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if _, err := deps.Queries.CreatePasswordReset(ctx, generated.CreatePasswordResetParams{
			PublicID:  resetPubID[:],
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
			user.DisplayName, resetURL,
		)
		// The response is always {ok:true} regardless of delivery so the
		// endpoint cannot be used to probe which emails exist. A delivery
		// failure is logged (without the token) so operators can still notice.
		if err := deps.Mailer.Send(ctx, mailer.Message{
			To:      user.Email,
			Subject: "Reset your Nodate Time password",
			Text:    body,
		}); err != nil {
			slog.ErrorContext(ctx, "failed to send password reset email", "userID", user.ID, "error", err)
		}

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
		if err := q.UpdatePasswordHash(ctx, generated.UpdatePasswordHashParams{
			PasswordHash: sql.NullString{String: newHash, Valid: true},
			UserID:       row.UserID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// Completing a reset ends every session opened with the old password.
		// Whoever knew it -- which is the reason a reset was requested --
		// must not keep the access it already bought them.
		if err := q.RevokeAllUserSessions(ctx, row.UserID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		consumeResult, err := q.MarkPasswordResetUsed(ctx, row.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		consumed, err := consumeResult.RowsAffected()
		if err != nil || consumed != 1 {
			return nil, apierrors.ToHuma(apierrors.AuthResetInvalid)
		}
		// Invalidate every other outstanding reset for this user so a second
		// stolen/leaked token cannot be used after a successful reset.
		if err := q.InvalidateUserPasswordResets(ctx, row.UserID); err != nil {
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
