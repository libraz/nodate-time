package e2e

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

var (
	testServerURL string
	testDB        *sql.DB
	testMailer    *helpers.CapturingMailer
	testStorage   *storage.Client
	testBucket    string
	// testWorkspacePublicID is the scope object storage keys are built from.
	testWorkspacePublicID string
)

func TestMain(m *testing.M) {
	if os.Getenv("TC_TEST_INTEGRATION") == "" {
		// Run tests — they will skip individually
		os.Exit(m.Run())
	}

	db := helpers.OpenTestDB(nil)
	if db == nil {
		os.Exit(1)
	}
	testDB = db

	testWorkspacePublicID = helpers.TestWorkspacePublicID(db)
	srv := helpers.NewTestServerForMain(db)
	testServerURL = srv.BaseURL
	testMailer = srv.Mailer
	testStorage = srv.Storage
	testBucket = srv.Bucket
	restoreLocation := claimStorageLocation(db, srv.Storage)

	code := m.Run()
	restoreLocation()
	srv.Server.Close()
	db.Close()
	os.Exit(code)
}

// claimStorageLocation records this run's bucket as the database's storage
// location and opens the process's removal gate on it, as the cleanup drainer
// does when the server starts. The returned func puts the previous value back.
func claimStorageLocation(db *sql.DB, sc *storage.Client) func() {
	if sc == nil {
		return func() {}
	}
	ctx := context.Background()
	var prev sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT setting_value FROM instance_settings WHERE setting_key = 'storage.location'").Scan(&prev); err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Fatalf("read storage location: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM instance_settings WHERE setting_key = 'storage.location'"); err != nil {
		log.Fatalf("clear storage location: %v", err)
	}
	if err := blobref.LocationAllowsRemoval(ctx, generated.New(db), sc.Location()); err != nil {
		log.Fatalf("record storage location: %v", err)
	}
	return func() {
		if !prev.Valid {
			_, _ = db.ExecContext(ctx, "DELETE FROM instance_settings WHERE setting_key = 'storage.location'")
			return
		}
		_, _ = db.ExecContext(ctx,
			"UPDATE instance_settings SET setting_value = ? WHERE setting_key = 'storage.location'", prev.String)
	}
}

func bootstrap(t *testing.T) {
	t.Helper()
	if os.Getenv("TC_TEST_INTEGRATION") == "" {
		t.Skip("set TC_TEST_INTEGRATION=1 to run integration tests")
	}
	if testServerURL == "" {
		t.Fatal("test server not started")
	}
}

// getTestStorage returns the package-wide storage client (may be nil).
func getTestStorage() *storage.Client { return testStorage }

// getTestBucket returns the per-run storage bucket the test server uploads into.
func getTestBucket() string { return testBucket }

// testCtx returns a fresh context for ad-hoc storage assertions in tests.
func testCtx() context.Context { return context.Background() }
