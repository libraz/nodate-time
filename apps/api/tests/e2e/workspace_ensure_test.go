package e2e

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// countingExecDB reports how many statements were sent as writes.
//
// The distinction is the whole point here, so the reads are deliberately not
// counted: what has to be true is that resolving an existing workspace sends
// no write at all, not that it sends few statements.
type countingExecDB struct {
	inner generated.DBTX
	execs atomic.Int64
}

func (c *countingExecDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.execs.Add(1)
	return c.inner.ExecContext(ctx, q, args...)
}

func (c *countingExecDB) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return c.inner.PrepareContext(ctx, q)
}

func (c *countingExecDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return c.inner.QueryContext(ctx, q, args...)
}

func (c *countingExecDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return c.inner.QueryRowContext(ctx, q, args...)
}

// TestEnsureDoesNotWriteToAWorkspaceThatIsAlreadyThere pins the reason Ensure
// reads before it writes.
//
// The upsert it falls back to writes id = id when the row exists: it changes
// nothing and still takes an exclusive lock on the one row every insert in
// this schema touches, because every table is scoped by workspace and every
// insert checks that foreign key. A shared lock requested after that exclusive
// one queues behind it, which is enough to close a cycle between two
// transactions that otherwise share only compatible locks -- and the
// transaction rolled back is one of theirs, not this one.
//
// Nothing observable changes, which is why this is measured as a write count
// rather than as behaviour. Asserting the returned scope would pass either
// way.
func TestEnsureDoesNotWriteToAWorkspaceThatIsAlreadyThere(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	counter := &countingExecDB{inner: testDB}
	scope, err := workspace.Ensure(context.Background(), generated.New(counter),
		helpers.TestWorkspaceSlug, "Nodate Time (test)", "Asia/Tokyo", "JP")
	require.NoError(t, err)
	require.NotZero(t, scope.ID, "the workspace still resolves")
	require.Equal(t, helpers.TestWorkspaceSlug, scope.Slug)
	require.Zero(t, counter.execs.Load(),
		"a workspace that is already there is read, never written")
}
