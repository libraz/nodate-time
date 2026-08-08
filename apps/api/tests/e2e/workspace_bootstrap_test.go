package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
	"github.com/stretchr/testify/require"
)

// Everything that starts against this database resolves the workspace first,
// and several of them start at once: the servers a test package builds for
// itself all aim the same upsert at the same row. What that has to produce is
// one workspace that every starter agrees on.
//
// This exercises the contended path rather than proving anything about the
// deadlock it can raise. A deadlock there is load-dependent -- one full suite
// run in four -- so a green run here is not evidence that the retry works;
// that is covered by injecting the error in internal/workspace. What this
// does hold is the invariant the upsert exists for, which no retry would fix
// if it broke.
func TestConcurrentStartsResolveOneWorkspace(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	slug := fmt.Sprintf("concurrent-start-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DELETE FROM workspaces WHERE slug = ?`, slug)
	})

	const starters = 8
	ids := make([]uint32, starters)
	errs := make([]error, starters)

	var wg sync.WaitGroup
	for i := range starters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, err := workspace.Ensure(context.Background(), generated.New(testDB),
				slug, "Concurrent start", "Asia/Tokyo", "JP")
			ids[i], errs[i] = ws.ID, err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "starter %d failed to resolve the workspace", i)
	}
	for i, id := range ids {
		require.Equal(t, ids[0], id, "starter %d resolved a different workspace", i)
	}

	var rows int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM workspaces WHERE slug = ?`, slug).Scan(&rows))
	require.Equal(t, 1, rows, "one slug is one workspace, however many starters raced for it")
}
