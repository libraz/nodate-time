package workspace

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// What these can and cannot show. A deadlock between concurrent upserts is
// load-dependent -- it appeared in one full suite run out of four and did not
// reproduce under a targeted loop -- so nothing here provokes a real one.
// Injecting the error MySQL raises proves the statement is tried again and
// that only that error is treated this way. It does not prove deadlocks stop
// happening, and it is not offered as that.

func deadlockErr() error { return &mysql.MySQLError{Number: 1213, Message: "Deadlock found"} }

func TestRetryOnDeadlockTriesAgainAndStopsWhenItSucceeds(t *testing.T) {
	calls := 0
	err := retryOnDeadlock(context.Background(), func() error {
		calls++
		if calls < 3 {
			return deadlockErr()
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls, "the attempt that succeeds is the last one made")
}

func TestRetryOnDeadlockGivesUpRatherThanWaitingForever(t *testing.T) {
	calls := 0
	err := retryOnDeadlock(context.Background(), func() error {
		calls++
		return deadlockErr()
	})
	require.Error(t, err)
	require.True(t, isDeadlock(err), "the caller is told what actually failed")
	require.Equal(t, deadlockAttempts, calls)
}

// Retrying anything else would turn a failure that will not change into a
// slower version of itself -- and a duplicate key or a bad statement says
// something the caller needs to see the first time.
func TestRetryOnDeadlockLeavesEveryOtherFailureAlone(t *testing.T) {
	for name, failure := range map[string]error{
		"duplicate key":     &mysql.MySQLError{Number: 1062},
		"lock wait timeout": &mysql.MySQLError{Number: 1205},
		"not a mysql error": errors.New("connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			err := retryOnDeadlock(context.Background(), func() error {
				calls++
				return failure
			})
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, calls, "tried once and reported")
		})
	}
}

// A cancelled context is not a reason to keep trying, and the error the
// caller gets should still be the one that stopped the work.
func TestRetryOnDeadlockStopsWhenTheCallerHasGoneAway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := retryOnDeadlock(ctx, func() error {
		calls++
		return deadlockErr()
	})
	require.True(t, isDeadlock(err))
	require.Equal(t, 1, calls)
}

// deadlockingDB is a DBTX whose writes always lose the deadlock. Only
// ExecContext is reached: Ensure gives up on the upsert before it reads the
// row back.
type deadlockingDB struct{ execs int }

func (d *deadlockingDB) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	d.execs++
	return nil, deadlockErr()
}

func (d *deadlockingDB) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unused")
}

func (d *deadlockingDB) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unused")
}

func (d *deadlockingDB) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	panic("Ensure must not read the row back after failing to write it")
}
