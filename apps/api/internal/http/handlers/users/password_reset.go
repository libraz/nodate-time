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

// The budget every automated mail answers to. The per-client half stops an
// attacker from spending the owner's allowance for their own mailbox; the
// per-mailbox half is what bounds the flood at all, since the number of
// clients asking is the attacker's to choose and each of them would otherwise
// buy another three.
const (
	passwordResetEmailLimit   = 3
	passwordResetMailboxLimit = 6
	passwordResetEmailWindow  = time.Hour
)

// mailPurpose keeps budgets that must not spend one another apart. A password
// reset is asked for anonymously and a confirmation link by the account
// itself; counted together, either one silences the other.
type mailPurpose string

const (
	mailPurposeReset  mailPurpose = "reset"
	mailPurposeVerify mailPurpose = "verify"
)

type resetEmailBucket struct {
	count       int
	windowStart time.Time
}

var resetEmailLimiter = struct {
	sync.Mutex
	buckets map[string]*resetEmailBucket
}{buckets: map[string]*resetEmailBucket{}}

// allowEmailSend takes one unit from the (address, client) budget and one from
// the address's own total, both scoped to a single purpose. A request the
// per-client half turns away costs the address nothing, so an attacker who has
// exhausted their own bucket has not eaten into what the owner can ask for.
func allowEmailSend(purpose mailPurpose, email, clientIP string, now time.Time) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	mailbox := string(purpose) + "|" + email
	if !takeEmailBudget(mailbox+"|"+clientIP, passwordResetEmailLimit, now) {
		return false
	}
	return takeEmailBudget(mailbox, passwordResetMailboxLimit, now)
}

// allowPasswordResetEmail is the reset half of that budget.
func allowPasswordResetEmail(email, clientIP string, now time.Time) bool {
	return allowEmailSend(mailPurposeReset, email, clientIP, now)
}

func takeEmailBudget(key string, limit int, now time.Time) bool {
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
	return b.count <= limit
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
		// endpoint cannot be used to probe which emails exist. Delivery can
		// finish after this handler returns, so an error here means the
		// message was never accepted; either way it is logged (without the
		// token) so operators can still notice.
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
