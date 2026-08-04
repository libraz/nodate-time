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

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Run opens a transaction, hands fn a Queries bound to it, and commits if
// fn returns nil. Any error -- including one from appending the event row
// -- rolls the whole thing back.
//
// The deferred rollback is a no-op after a successful commit, so it is
// safe on both paths and covers an early return from anywhere in fn.
func Run(ctx context.Context, db *sql.DB, fn func(q *generated.Queries) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(generated.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
