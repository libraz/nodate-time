package dbtx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// fakeDriver counts transactions; it supports nothing but Begin/Commit/Rollback.
type fakeDriver struct {
	begins, commits, rollbacks atomic.Int32
	serializable               atomic.Int32
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d}, nil }

type fakeConn struct{ d *fakeDriver }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { c.d.begins.Add(1); return &fakeTx{c.d}, nil }

func (c *fakeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if sql.IsolationLevel(opts.Isolation) == sql.LevelSerializable {
		c.d.serializable.Add(1)
	}
	return c.Begin()
}

type fakeTx struct{ d *fakeDriver }

func (t *fakeTx) Commit() error   { t.d.commits.Add(1); return nil }
func (t *fakeTx) Rollback() error { t.d.rollbacks.Add(1); return nil }

type fakeConnector struct{ d *fakeDriver }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{c.d}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return c.d }

func newDB(t *testing.T) (*sql.DB, *fakeDriver) {
	t.Helper()
	d := &fakeDriver{}
	db := sql.OpenDB(fakeConnector{d})
	t.Cleanup(func() { _ = db.Close() })
	return db, d
}

func TestRunRetriesDeadlockAndLockWait(t *testing.T) {
	for _, number := range []uint16{1213, 1205} {
		db, d := newDB(t)
		calls := 0
		err := Run(context.Background(), db, func(*generated.Queries) error {
			calls++
			if calls < 3 {
				return &mysql.MySQLError{Number: number}
			}
			return nil
		})
		if err != nil || calls != 3 || d.begins.Load() != 3 || d.commits.Load() != 1 {
			t.Fatalf("code %d: err=%v calls=%d begins=%d commits=%d", number, err, calls, d.begins.Load(), d.commits.Load())
		}
	}
}

func TestRunGivesUpAfterLimit(t *testing.T) {
	db, _ := newDB(t)
	calls := 0
	err := Run(context.Background(), db, func(*generated.Queries) error {
		calls++
		return &mysql.MySQLError{Number: 1213}
	})
	var me *mysql.MySQLError
	if !errors.As(err, &me) || calls != maxAttempts {
		t.Fatalf("err=%v calls=%d want %d", err, calls, maxAttempts)
	}
}

func TestRunDoesNotRetryOtherErrors(t *testing.T) {
	db, _ := newDB(t)
	for _, e := range []error{errors.New("boom"), &mysql.MySQLError{Number: 1062}} {
		calls := 0
		err := Run(context.Background(), db, func(*generated.Queries) error { calls++; return e })
		if !errors.Is(err, e) || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	}
}

func TestRunDoesNotRetryWithAmbientTx(t *testing.T) {
	db, _ := newDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	calls := 0
	err = Run(WithTx(context.Background(), tx), db, func(*generated.Queries) error {
		calls++
		return &mysql.MySQLError{Number: 1213}
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRunSerializableRetriesAndUsesSerializable(t *testing.T) {
	db, d := newDB(t)
	calls := 0
	err := RunSerializable(context.Background(), db, func(*generated.Queries) error {
		calls++
		if calls == 1 {
			return &mysql.MySQLError{Number: 1213}
		}
		return nil
	})
	if err != nil || calls != 2 || d.commits.Load() != 1 || d.serializable.Load() != 2 {
		t.Fatalf("err=%v calls=%d commits=%d", err, calls, d.commits.Load())
	}
}
