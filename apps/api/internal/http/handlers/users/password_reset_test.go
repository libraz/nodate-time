package users

import (
	"fmt"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/stretchr/testify/require"
)

func TestAllowPasswordResetEmailThrottlesByNormalizedAddress(t *testing.T) {
	resetEmailBudgets()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	require.True(t, allowPasswordResetEmail(" User@Example.com ", "203.0.113.5", now))
	require.True(t, allowPasswordResetEmail("user@example.com", "203.0.113.5", now))
	require.True(t, allowPasswordResetEmail("USER@example.com", "203.0.113.5", now))
	require.False(t, allowPasswordResetEmail("user@example.com", "203.0.113.5", now))

	require.True(t, allowPasswordResetEmail("user@example.com", "203.0.113.5", now.Add(passwordResetEmailWindow)))
}

// TestAllowPasswordResetEmailScopedByIP verifies that an attacker exhausting
// their own IP's budget against a victim's email does not consume the
// victim's own budget for that same mailbox.
func TestAllowPasswordResetEmailScopedByIP(t *testing.T) {
	resetEmailBudgets()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	attackerIP := "198.51.100.7"
	victimIP := "203.0.113.5"

	// Attacker exhausts the budget for the victim's email from their own IP.
	require.True(t, allowPasswordResetEmail("victim@example.com", attackerIP, now))
	require.True(t, allowPasswordResetEmail("victim@example.com", attackerIP, now))
	require.True(t, allowPasswordResetEmail("victim@example.com", attackerIP, now))
	require.False(t, allowPasswordResetEmail("victim@example.com", attackerIP, now))

	// The victim, requesting the same email from their own IP, is unaffected.
	require.True(t, allowPasswordResetEmail("victim@example.com", victimIP, now))
}

func resetEmailBudgets() {
	emailClientBudgets = middleware.NewWindowCounter(passwordResetEmailWindow, emailBudgetKeys)
	emailMailboxBudgets = middleware.NewWindowCounter(passwordResetEmailWindow, emailBudgetKeys)
}

// Requests for other mailboxes, however many, must not forget what one
// mailbox has already been sent inside its window.
func TestMailboxBudgetSurvivesAFloodOfOtherMailboxes(t *testing.T) {
	resetEmailBudgets()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for i := range passwordResetMailboxLimit {
		require.True(t, allowPasswordResetEmail("victim@example.com", fmt.Sprintf("198.51.100.%d", i), now))
	}
	for i := range emailBudgetKeys {
		allowPasswordResetEmail(fmt.Sprintf("other-%d@example.com", i), fmt.Sprintf("203.0.%d.%d", i/256, i%256), now)
	}

	require.False(t, allowPasswordResetEmail("victim@example.com", "192.0.2.1", now),
		"the victim's spent budget was forgotten while its window was still open")
	require.True(t, allowPasswordResetEmail("victim@example.com", "192.0.2.1", now.Add(passwordResetEmailWindow)),
		"the budget returns once the window has run out")
}

// Every address in one IPv6 /64 is one client, as it is for the request limiter.
func TestEmailClientBudgetSharesAnIPv6Slash64(t *testing.T) {
	resetEmailBudgets()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for i := range passwordResetEmailLimit {
		require.True(t, allowPasswordResetEmail("rotating@example.com", fmt.Sprintf("2001:db8:1:2::%x", i+1), now))
	}
	require.False(t, allowPasswordResetEmail("rotating@example.com", "2001:db8:1:2::ff", now))
}
