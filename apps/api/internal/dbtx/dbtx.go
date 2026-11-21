// Package dbtx runs a unit of work in one transaction.
//
// It exists because the shared contract requires every state change to
// append its event row in the same transaction as the change itself. A
// change that commits without its event is invisible to every other
// process on the database, and a helper that makes the transaction the
// default shape is the difference between that being hard to get wrong and
// easy to forget.
package dbtx

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Beginner opens transactions. *sql.DB is the one in production.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type ambientKey struct{}

// WithTx returns a context in which Run joins tx instead of opening a
// transaction of its own, leaving the commit to whoever opened tx. It exists
// for a test that serves the real handlers on a transaction it rolls back, so
// state that is global to the database never becomes visible to other tests.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, ambientKey{}, tx)
}

// maxAttempts bounds how often a transaction is rerun after a deadlock or
// lock-wait timeout.
const maxAttempts = 3

// Run opens a transaction, hands fn a Queries bound to it, and commits if
// fn returns nil. Any error -- including one from appending the event row
// -- rolls the whole thing back.
//
// A MySQL deadlock (1213) or lock-wait timeout (1205) reruns the whole
// transaction, so fn must have no effect outside it. A transaction joined
// through WithTx is never rerun.
func Run(ctx context.Context, db Beginner, fn func(q *generated.Queries) error) error {
	return run(ctx, db, nil, fn)
}

// RunSerializable is Run at the SERIALIZABLE isolation level.
func RunSerializable(ctx context.Context, db Beginner, fn func(q *generated.Queries) error) error {
	return run(ctx, db, &sql.TxOptions{Isolation: sql.LevelSerializable}, fn)
}

func run(ctx context.Context, db Beginner, opts *sql.TxOptions, fn func(q *generated.Queries) error) error {
	if tx, ok := ctx.Value(ambientKey{}).(*sql.Tx); ok {
		return fn(generated.New(tx))
	}
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err = once(ctx, db, opts, fn); !retryable(err) {
			return err
		}
	}
	return err
}

// once runs one attempt. The deferred rollback is a no-op after a successful
// commit, so it covers an early return from anywhere in fn.
func once(ctx context.Context, db Beginner, opts *sql.TxOptions, fn func(q *generated.Queries) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(generated.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func retryable(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == 1205 || mysqlErr.Number == 1213)
}
