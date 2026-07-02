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
	require.True(t, allowPasswordResetEmail(" User@Example.com ", now))
	require.True(t, allowPasswordResetEmail("user@example.com", now))
	require.True(t, allowPasswordResetEmail("USER@example.com", now))
	require.False(t, allowPasswordResetEmail("user@example.com", now))

	require.True(t, allowPasswordResetEmail("user@example.com", now.Add(passwordResetEmailWindow)))
}
