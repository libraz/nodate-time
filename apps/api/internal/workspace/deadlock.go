package workspace

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/go-sql-driver/mysql"
)

// mysqlDeadlock is ER_LOCK_DEADLOCK: InnoDB found a cycle and rolled this
// transaction back to break it.
const mysqlDeadlock = 1213

// deadlockAttempts bounds how often a rolled-back statement is tried again.
//
// A deadlock is over the moment the victim is rolled back, so the next
// attempt starts from a clean state rather than waiting for load to fall.
// Three is therefore not a way of outlasting contention: if three
// consecutive attempts at one upsert all lose, what is holding that row is
// not the momentary overlap this handles, and stopping says so instead of
// retrying into a hang nobody can read.
const deadlockAttempts = 3

// deadlockBackoff is the base pause between attempts. It is jittered because
// whoever collides here started at the same moment -- several servers or
// several test packages booting together -- and a fixed pause would line the
// same set of callers up to collide again.
const deadlockBackoff = 10 * time.Millisecond

// retryOnDeadlock runs fn again when MySQL rolled it back to break a deadlock.
//
// This is sound only because fn is a single statement outside any explicit
// transaction: 1213 rolls back the whole transaction, so retrying one
// statement of a larger one would carry on inside a transaction that no
// longer exists. Every caller of Ensure passes a Queries built on the *sql.DB
// rather than on a transaction, which is what makes each statement its own.
//
// A lock wait timeout (1205) is deliberately not retried. In autocommit it is
// just as recoverable, but reaching one means the statement already waited out
// innodb_lock_wait_timeout -- fifty seconds by default -- for a lock somebody
// else is still holding. Trying again doubles a startup that has already
// stalled, and hides the holder behind it. A deadlock is instantaneous and
// caused by the callers overlapping each other, which is what makes it the one
// worth absorbing here.
func retryOnDeadlock(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 1; attempt <= deadlockAttempts; attempt++ {
		err = fn()
		if err == nil || !isDeadlock(err) || attempt == deadlockAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			// The caller stopped waiting. The deadlock is the useful half of
			// what happened, so it is what gets reported.
			return err
		case <-time.After(jitteredBackoff(attempt)):
		}
	}
	return err
}

func isDeadlock(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDeadlock
}

func jitteredBackoff(attempt int) time.Duration {
	base := time.Duration(attempt) * deadlockBackoff
	return base + time.Duration(rand.Int64N(int64(base)))
}
