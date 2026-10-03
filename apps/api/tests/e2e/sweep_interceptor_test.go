package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// farFuture is the horizon a test seeds its "expired" credential rows beyond
// and then makes its own sweep treat as past: no other test's sweep ever
// considers them expired, so nothing else can collect them first.
const farFuture = 60 * 24 * time.Hour

// sweepBoundShift is the cutoff a scoped sweep is given in place of the real
// one; it lies between the seeded rows and farFuture.
const sweepBoundShift = 90 * 24 * time.Hour

// execInterceptDB lets a test stand between the cleanup sweep and the shared
// database. The sweep walks global tables, so other tests' rows and other
// tests' sweeps are in the same statements; rewriting or refusing only the
// statements aimed at this test's rows keeps the assertion about its own data.
type execInterceptDB struct {
	*sql.DB
	// exec returns handled=true to answer in place of the real database.
	exec func(ctx context.Context, query string, args []any) (res sql.Result, err error, handled bool)
	// rewrite may change a statement and its arguments before they reach the
	// database.
	rewrite func(query string, args []any) (string, []any)
}

var _ generated.DBTX = (*execInterceptDB)(nil)

// scopeSweep rewrites the expiry delete that starts with sweep so it removes
// only this user's rows and sees them as expired, and counts each statement.
func scopeSweep(table, sweep string, userID uint32, statements *atomic.Int64) func(string, []any) (string, []any) {
	return func(query string, args []any) (string, []any) {
		if !strings.Contains(query, sweep) {
			return query, args
		}
		statements.Add(1)
		query = strings.Replace(query, sweep,
			fmt.Sprintf("DELETE FROM %s WHERE user_id = %d AND expires_at < ?", table, userID), 1)
		shifted := append([]any{time.Now().Add(sweepBoundShift)}, args[1:]...)
		return query, shifted
	}
}

func (d *execInterceptDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if d.exec != nil {
		if res, err, handled := d.exec(ctx, query, args); handled {
			return res, err
		}
	}
	if d.rewrite != nil {
		query, args = d.rewrite(query, args)
	}
	return d.DB.ExecContext(ctx, query, args...)
}

// affectedNothing is a statement result that reports no row changed.
type affectedNothing struct{}

func (affectedNothing) LastInsertId() (int64, error) { return 0, nil }
func (affectedNothing) RowsAffected() (int64, error) { return 0, nil }
