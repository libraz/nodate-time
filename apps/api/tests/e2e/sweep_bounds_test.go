package e2e

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestExpirySweepsAreFoundByIndexNotByReadingTheTable checks the plan, not the
// clock.
//
// The sweep asks which rows are past their expiry, a question that names no
// user. Both tables carried only a composite led by user_id, and a composite
// is no use from its second column, so the question was answered by reading
// everything -- on a table that grows with every sign-in the deployment ever
// serves. Nothing about that is visible from the outside: the rows do get
// collected, the tick just costs more every week.
//
// A timing assertion here would be a flake generator. The plan is the durable
// statement, and the tables are filled first because an empty one makes every
// plan look equally good.
func TestExpirySweepsAreFoundByIndexNotByReadingTheTable(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	userID := internalUserID(t, tenant.UserID)

	// Most rows are live. A table where everything matched would make reading
	// all of it the right answer.
	seedSessions(t, userID, 2000, 20)
	seedPasswordResets(t, userID, 2000, 20)

	cutoff := time.Now()

	const sessionSweep = `DELETE FROM sessions WHERE expires_at < ? ORDER BY expires_at LIMIT 500`
	key, rows, _ := explainOne(t, sessionSweep, cutoff)
	require.Contains(t, key, "expires",
		"the session sweep must enter an expiry index, got key=%q", key)
	require.Less(t, rows, countRows(t, "sessions")/2,
		"the session sweep still reads most of the table: %d rows", rows)

	const resetSweep = `DELETE FROM password_resets WHERE expires_at < ? ORDER BY expires_at LIMIT 500`
	key, rows, _ = explainOne(t, resetSweep, cutoff)
	require.Contains(t, key, "expires",
		"the password reset sweep must enter an expiry index, got key=%q", key)
	require.Less(t, rows, countRows(t, "password_resets")/2,
		"the password reset sweep still reads most of the table: %d rows", rows)

	// signin_states already carries idx_signin_states_expires. Confirmed here
	// rather than assumed, since the same sweep shape depends on it.
	const signinSweep = `DELETE FROM signin_states WHERE expires_at < ? ORDER BY expires_at LIMIT 500`
	key, _, _ = explainOne(t, signinSweep, cutoff)
	require.Contains(t, key, "expires",
		"the sign-in state sweep must enter an expiry index, got key=%q", key)
}

// TestExpirySweepDrainsPastOneBatch covers the other half of the bound. A
// statement that collects the whole backlog at once locks it all at once, so
// each one is capped -- and a cap with no loop behind it would leave the rest
// of the backlog for the next tick, fifteen minutes later, forever falling
// behind a table that keeps growing.
func TestExpirySweepDrainsPastOneBatch(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	userID := internalUserID(t, tenant.UserID)

	// More than one batch of expired rows, so a single statement cannot be the
	// whole answer.
	const expired = 620
	seedSessions(t, userID, expired, expired)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	var left int64
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND expires_at < NOW(3)`, userID).Scan(&left))
	require.Zero(t, left,
		"the sweep stopped at a batch boundary and left %d expired sessions behind", left)
}

// TestSpentPasswordResetSurvivesUntilItsOwnExpiry pins the behaviour change
// that came with the indexed sweep. A redeemed token used to be collected on
// the next tick through an `OR used_at IS NOT NULL` branch, which no expiry
// index can answer -- so the whole table was read to save an already inert row
// an hour of shelf life. It is inert because redemption is what
// GetPasswordResetByTokenHash refuses, not absence from the table.
func TestSpentPasswordResetSurvivesUntilItsOwnExpiry(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	userID := internalUserID(t, tenant.UserID)

	pub := uuid.New()
	hash := uuid.NewString() + uuid.NewString()
	_, err := testDB.Exec(
		`INSERT INTO password_resets (public_id, user_id, token_hash, expires_at, used_at)
		 VALUES (?, ?, ?, ?, NOW(3))`,
		pub[:], userID, hash[:64], time.Now().Add(time.Hour))
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM password_resets WHERE public_id = ?`, pub[:]).Scan(&rows))
	require.Equal(t, 1, rows,
		"a spent token is collected on its own expiry, not by a predicate no index can serve")

	// And it is refused meanwhile, which is what makes leaving it harmless.
	q := generated.New(testDB)
	_, err = q.GetPasswordResetByTokenHash(testCtx(), hash[:64])
	require.Error(t, err, "a redeemed token must not be usable while it sits there")
}

// internalUserID resolves a public user id to the internal one the auth tables
// point at.
func internalUserID(t *testing.T, publicID string) uint32 {
	t.Helper()
	parsed, err := uuid.Parse(publicID)
	require.NoError(t, err)
	var id uint32
	require.NoError(t, testDB.QueryRow(
		`SELECT id FROM users WHERE public_id = ?`, parsed[:]).Scan(&id))
	return id
}

func countRows(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

// seedSessions inserts total sessions for one user, the first expired of them
// already past their expiry.
func seedSessions(t *testing.T, userID uint32, total, expired int) {
	t.Helper()
	bulkInsert(t,
		`INSERT INTO sessions (public_id, user_id, refresh_hash, expires_at)`,
		"(?, ?, ?, ?)", expiringRows(userID, total, expired))
	analyze(t, "sessions")
}

func seedPasswordResets(t *testing.T, userID uint32, total, expired int) {
	t.Helper()
	bulkInsert(t,
		`INSERT INTO password_resets (public_id, user_id, token_hash, expires_at)`,
		"(?, ?, ?, ?)", expiringRows(userID, total, expired))
	analyze(t, "password_resets")
}

// expiringRows builds (public_id, user_id, hash, expires_at) tuples, which is
// the shape both credential tables happen to share.
func expiringRows(userID uint32, total, expired int) [][]any {
	now := time.Now()
	rows := make([][]any, 0, total)
	for i := range total {
		expiresAt := now.Add(time.Duration(i+1) * time.Hour)
		if i < expired {
			expiresAt = now.Add(-time.Duration(i+1) * time.Hour)
		}
		pub := uuid.New()
		hash := uuid.NewString() + uuid.NewString()
		rows = append(rows, []any{pub[:], userID, hash[:64], expiresAt})
	}
	return rows
}

// analyze refreshes the statistics the optimiser weighs a plan against; they
// are otherwise only refreshed as a side effect of enough writes.
func analyze(t *testing.T, table string) {
	t.Helper()
	_, err := testDB.Exec(`ANALYZE TABLE ` + table)
	require.NoError(t, err)
}
