package helpers

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
)

// CountingDB wraps a database handle and counts the statements sent through
// it, so a test can assert on how a handler asks rather than only on what it
// answers.
//
// It exists for the reads whose cost grows with the size of the answer. Such a
// handler stays correct as it degrades, so nothing but a count notices when a
// batched read goes back to being one query per row.
type CountingDB struct {
	inner   generated.DBTX
	queries atomic.Int64
}

func (c *CountingDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.queries.Add(1)
	return c.inner.ExecContext(ctx, q, args...)
}

func (c *CountingDB) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return c.inner.PrepareContext(ctx, q)
}

func (c *CountingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries.Add(1)
	return c.inner.QueryContext(ctx, q, args...)
}

func (c *CountingDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.queries.Add(1)
	return c.inner.QueryRowContext(ctx, q, args...)
}

// Reset zeroes the counter, so a test can set its fixtures up through the same
// server and then measure only the request it cares about.
func (c *CountingDB) Reset() { c.queries.Store(0) }

// Count reports the statements sent since the last Reset.
func (c *CountingDB) Count() int { return int(c.queries.Load()) }

// NewCountingTestServer is NewTestServer with every generated query routed
// through a counter. Transactions still run on the raw handle, so writes are
// invisible to it -- what it measures is the read path.
//
// The server has its own workspace-scoped queries but shares the database, so
// a test using it must not run in parallel with itself.
func NewCountingTestServer(t *testing.T, db *sql.DB) (*TestServer, *CountingDB) {
	t.Helper()
	mc := NewCapturingMailer()
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	counter := &CountingDB{inner: db}
	deps := buildHandler(db, mc, sc)
	deps.Queries = generated.New(counter)

	srv := httptest.NewServer(app.NewHandler(*deps, testAppOptions()))
	t.Cleanup(func() { srv.Close() })

	return &TestServer{
		BaseURL: srv.URL,
		Server:  srv,
		DB:      db,
		Mailer:  mc,
		Storage: sc,
		Bucket:  bucket,
	}, counter
}
