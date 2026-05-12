package e2e

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

var (
	testServerURL string
	testDB        *sql.DB
	testMailer    *helpers.CapturingMailer
	testStorage   *storage.Client
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

	srv := helpers.NewTestServerForMain(db)
	testServerURL = srv.BaseURL
	testMailer = srv.Mailer
	testStorage = srv.Storage

	code := m.Run()
	srv.Server.Close()
	db.Close()
	os.Exit(code)
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

// testCtx returns a fresh context for ad-hoc storage assertions in tests.
func testCtx() context.Context { return context.Background() }
