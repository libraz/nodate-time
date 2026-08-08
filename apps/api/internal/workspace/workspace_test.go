package workspace

import (
	"context"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/stretchr/testify/require"
)

// Ensure has to be the thing that retries. Every caller of it -- the server
// at startup, the bootstrap CLI, a test suite's setup -- has the same nothing
// to do with a deadlock except fail, and a failure here reads as the whole
// suite or the whole deployment being broken.
func TestEnsureTriesTheUpsertAgainWhenItIsRolledBack(t *testing.T) {
	db := &deadlockingDB{}

	_, err := Ensure(context.Background(), generated.New(db), "test", "Test", "UTC", "JP")
	require.Error(t, err)
	require.ErrorContains(t, err, `ensure workspace "test"`,
		"the failure still names what was being done")
	require.True(t, isDeadlock(err), "and what MySQL said, rather than a summary of it")
	require.Equal(t, deadlockAttempts, db.execs,
		"the upsert is what gets tried again")
}

func TestEnsureRefusesAnEmptySlug(t *testing.T) {
	db := &deadlockingDB{}

	_, err := Ensure(context.Background(), generated.New(db), "", "Test", "UTC", "")
	require.Error(t, err)
	require.Zero(t, db.execs, "a slug that identifies nothing must not reach the database")
}
