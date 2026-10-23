package e2e

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func insertEmailVerification(t *testing.T, userID uint32, expiresAt time.Time, used bool) []byte {
	t.Helper()
	pub := uuid.New()
	hash := uuid.NewString() + uuid.NewString()
	usedAt := "NULL"
	if used {
		usedAt = "NOW(3)"
	}
	_, err := testDB.Exec(
		`INSERT INTO email_verifications (public_id, user_id, email, token_hash, expires_at, used_at)
		 VALUES (?, ?, 'sweep@example.test', ?, ?, `+usedAt+`)`,
		pub[:], userID, hash[:64], expiresAt)
	require.NoError(t, err)
	return pub[:]
}

func emailVerificationExists(t *testing.T, publicID []byte) bool {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM email_verifications WHERE public_id = ?`, publicID).Scan(&n))
	return n == 1
}

// The cleanup pass collects email verification tokens past their expiry, in
// bounded batches, and leaves every token that can still be redeemed. Without
// the sweep the table grows with every sign-up and address change for as long
// as the deployment runs.
//
// Other tests sweep concurrently, so the delete is scoped to this user's rows
// and counted: one pass must keep deleting until a batch comes back short.
func TestCleanupCollectsExpiredEmailVerificationsInBatches(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	userID := internalUserID(t, tenant.UserID)

	// More than one batch of expired rows, so a single statement cannot be the
	// whole answer. They are seeded beyond any real expiry and the delete is
	// shifted to see them as expired, so no other test's sweep can take them.
	const expired = 620
	rows := make([][]any, 0, expired)
	for i := range expired {
		pub := uuid.New()
		hash := uuid.NewString() + uuid.NewString()
		rows = append(rows, []any{pub[:], userID, "sweep@example.test", hash[:64],
			time.Now().Add(farFuture + time.Duration(i)*time.Hour)})
	}
	bulkInsert(t,
		`INSERT INTO email_verifications (public_id, user_id, email, token_hash, expires_at)`,
		"(?, ?, ?, ?, ?)", rows)

	live := insertEmailVerification(t, userID, time.Now().Add(2*sweepBoundShift), false)
	spentButUnexpired := insertEmailVerification(t, userID, time.Now().Add(2*sweepBoundShift), true)

	var statements atomic.Int64
	db := &execInterceptDB{DB: testDB}
	db.rewrite = scopeSweep("email_verifications",
		"DELETE FROM email_verifications WHERE expires_at < ?", userID, &statements)

	cleanup.RunOnce(testCtx(), db, getTestStorage())

	require.GreaterOrEqual(t, statements.Load(), int64(2),
		"one pass must keep deleting until a batch comes back short")
	var left int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM email_verifications WHERE user_id = ? AND expires_at < ?`,
		userID, time.Now().Add(sweepBoundShift)).Scan(&left))
	require.Zero(t, left, "expired tokens left behind: %d", left)
	require.True(t, emailVerificationExists(t, live), "a token that can still be redeemed must survive")
	require.True(t, emailVerificationExists(t, spentButUnexpired),
		"a spent token is collected on its own expiry, like the other credential tables")
}

// The sweep asks which tokens are past their expiry, a question that names no
// user, so it has to be answered from the expiry index.
func TestEmailVerificationSweepEntersTheExpiryIndex(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	userID := internalUserID(t, tenant.UserID)

	rows := make([][]any, 0, 2000)
	for i := range 2000 {
		pub := uuid.New()
		hash := uuid.NewString() + uuid.NewString()
		expiresAt := time.Now().Add(time.Duration(i+1) * time.Hour)
		if i < 20 {
			expiresAt = time.Now().Add(-time.Duration(i+1) * time.Hour)
		}
		rows = append(rows, []any{pub[:], userID, "plan@example.test", hash[:64], expiresAt})
	}
	bulkInsert(t,
		`INSERT INTO email_verifications (public_id, user_id, email, token_hash, expires_at)`,
		"(?, ?, ?, ?, ?)", rows)
	analyze(t, "email_verifications")

	key, examined, _ := explainOne(t,
		`DELETE FROM email_verifications WHERE expires_at < ? ORDER BY expires_at LIMIT 500`, time.Now())
	require.Contains(t, key, "expir", "the sweep must enter an expiry index, got key=%q", key)
	require.Less(t, examined, countRows(t, "email_verifications")/2,
		"the sweep still reads most of the table: %d rows", examined)
}
