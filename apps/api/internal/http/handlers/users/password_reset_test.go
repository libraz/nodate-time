package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAllowPasswordResetEmailThrottlesByNormalizedAddress(t *testing.T) {
	resetEmailLimiter.Lock()
	resetEmailLimiter.buckets = map[string]*resetEmailBucket{}
	resetEmailLimiter.Unlock()

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
	resetEmailLimiter.Lock()
	resetEmailLimiter.buckets = map[string]*resetEmailBucket{}
	resetEmailLimiter.Unlock()

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
