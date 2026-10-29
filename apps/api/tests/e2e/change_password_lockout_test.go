package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Changing the password checks the current one, so it counts wrong guesses on
// the same identity row as sign-in does. Otherwise a stolen session could keep
// guessing the password it does not know, unbounded, and then change it.
func TestChangePasswordGuessesShareTheLoginLockout(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("change-lock-%d@test.local", time.Now().UnixNano())
	const password = "correcthorsebattery"
	token := registerFor(t, email, password)

	change := func(current string) int {
		status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/password", token,
			map[string]any{"currentPassword": current, "newPassword": "another-long-password"})
		return status
	}

	for range lockoutGuesses {
		require.Equal(t, http.StatusBadRequest, change("definitely-not-it"))
	}
	attempts, locked := identityLockState(t, email)
	require.Equal(t, lockoutGuesses, attempts, "each wrong current password is counted")
	require.True(t, locked.Valid, "and the fifth locks the identity")

	require.Equal(t, http.StatusTooManyRequests, change(password),
		"the right password is refused while the lock stands")
	status, _ := loginStatus(t, email, password)
	require.Equal(t, http.StatusUnauthorized, status, "sign-in shares the same lock")
}
