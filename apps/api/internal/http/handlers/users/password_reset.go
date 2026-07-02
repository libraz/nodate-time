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

func allowPasswordResetEmail(email string, now time.Time) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return false
	}

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

		if !allowPasswordResetEmail(in.Body.Email, time.Now()) {
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
