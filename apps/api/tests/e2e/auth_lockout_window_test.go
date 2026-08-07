package e2e

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What a lockout costs the owner of the account, once the window it was set
// for has passed.

// The counter belongs to a window, not to the identity's lifetime. Ten wrong
// passwords lock the account for fifteen minutes; once those are over, the
// guesses that earned the lock are spent and the next one is the first of a
// new count. Otherwise a single request every quarter of an hour keeps any
// account whose address is known locked for good, and the owner can only sign
// in during a gap the attacker chooses.
func TestTheFailedLoginCounterStartsOverOnceTheWindowHasPassed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("lock-window-%d@test.local", time.Now().UnixNano())
	const password = "correcthorsebattery"
	registerFor(t, email, password)

	for range 10 {
		loginStatus(t, email, "definitely-not-it")
	}
	attempts, locked := identityLockState(t, email)
	require.Equal(t, 10, attempts)
	require.True(t, locked.Valid)

	expireLock(t, email)

	status, _ := loginStatus(t, email, "definitely-not-it")
	require.Equal(t, http.StatusUnauthorized, status)
	attempts, locked = identityLockState(t, email)
	require.Equal(t, 1, attempts,
		"a guess after the window is the first of a new count, not the eleventh")
	require.False(t, locked.Valid,
		"so it cannot re-lock the account by itself")

	status, _ = loginStatus(t, email, password)
	require.Equal(t, http.StatusOK, status,
		"and the owner gets in without waiting for a gap someone else chooses")
}

// Decaying the counter must not decay the lockout itself: a new window still
// takes a full ten guesses, counted from the one that opened it.
func TestANewWindowStillTakesTenGuessesToLock(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("lock-refill-%d@test.local", time.Now().UnixNano())
	const password = "correcthorsebattery"
	registerFor(t, email, password)

	for range 10 {
		loginStatus(t, email, "definitely-not-it")
	}
	expireLock(t, email)

	// The first guess of the new window, plus eight more, leave one to spare.
	for i := range 9 {
		require.Equal(t, http.StatusUnauthorized, mustGuess(t, email), "guess %d", i+1)
		attempts, locked := identityLockState(t, email)
		require.Equal(t, i+1, attempts, "guess %d of the new window", i+1)
		require.False(t, locked.Valid, "still unlocked after %d guesses", i+1)
	}

	require.Equal(t, http.StatusUnauthorized, mustGuess(t, email))
	attempts, locked := identityLockState(t, email)
	require.Equal(t, 10, attempts)
	require.True(t, locked.Valid, "the tenth guess of the new window locks it again")
}

func mustGuess(t *testing.T, email string) int {
	t.Helper()
	status, _ := loginStatus(t, email, "definitely-not-it")
	return status
}

// A locked account and an address nobody registered must be indistinguishable,
// and an equal response body is not enough to make them so: the lockout branch
// has to hash a password it has already decided to refuse, because every other
// path runs Argon2id first and answering without it is measurable. Refusing a
// locked account without the hash medians 1.6 ms against 47.5 ms for an unknown
// address; with it, 37.5 ms against 37.4 ms. A gap of roughly 25x is readable
// across a network, and it says both that the address has an account and that
// it is currently locked.
//
// The gap is therefore not something the equality assertion below can catch on
// its own. It is still not asserted: a wall-clock assertion in CI would be
// flaky, and one loose enough not to be would no longer be measuring anything.
// The medians are logged instead, so a regression is visible to whoever reads
// the run.
func TestALockedAccountCostsTheSameToRefuseAsAnUnknownAddress(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("lock-timing-%d@test.local", time.Now().UnixNano())
	registerFor(t, email, "correcthorsebattery")
	for range 10 {
		loginStatus(t, email, "definitely-not-it")
	}
	_, locked := identityLockState(t, email)
	require.True(t, locked.Valid)

	unknown := fmt.Sprintf("nobody-%d@test.local", time.Now().UnixNano())

	lockedStatus, lockedBody := loginStatus(t, email, "definitely-not-it")
	unknownStatus, unknownBody := loginStatus(t, unknown, "definitely-not-it")
	require.Equal(t, unknownStatus, lockedStatus)
	require.Equal(t, string(unknownBody), string(lockedBody),
		"the response must not say whether the address has an account, or whether it is locked")

	lockedMedian := medianLoginTime(t, email)
	unknownMedian := medianLoginTime(t, unknown)
	t.Logf("median refusal: locked %v, unknown address %v", lockedMedian, unknownMedian)
}

// medianLoginTime times a handful of refusals and returns the middle one, so a
// single scheduling hiccup does not stand for the whole measurement.
func medianLoginTime(t *testing.T, email string) time.Duration {
	t.Helper()
	const rounds = 5
	samples := make([]time.Duration, 0, rounds)
	for range rounds {
		start := time.Now()
		status, _ := loginStatus(t, email, "definitely-not-it")
		samples = append(samples, time.Since(start))
		require.Equal(t, http.StatusUnauthorized, status)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[rounds/2]
}
