package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The server computes expiry timestamps for sessions, invites and password
// resets with NOW(3), which is evaluated in the connection's session zone,
// while the driver reads them back in its own location. If the two disagree,
// a one-hour reset link is already dead when it arrives and every stored
// timestamp is off by the host's offset — with no error anywhere.
func TestConnectionPinsTheSessionTimezoneToUTC(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	var zone string
	require.NoError(t, testDB.QueryRow("SELECT @@session.time_zone").Scan(&zone))
	require.Equal(t, "+00:00", zone)
}

func TestServerClockAgreesWithGoClock(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	var dbNow time.Time
	require.NoError(t, testDB.QueryRow("SELECT NOW(3)").Scan(&dbNow))

	// A mismatch between the two clocks shows up as a whole-hour offset; a
	// generous window keeps this from tripping on a slow test machine.
	require.WithinDuration(t, time.Now().UTC(), dbNow, time.Minute)
}
