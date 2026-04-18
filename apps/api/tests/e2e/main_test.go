package e2e

import (
	"database/sql"
	"os"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

var (
	testServerURL string
	testDB        *sql.DB
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
