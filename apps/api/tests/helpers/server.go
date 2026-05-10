package helpers

import (
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
)

const (
	TestJWTSecret = "test-jwt-secret-for-e2e"
	TestWebURL    = "http://web.test.local"
)

type TestServer struct {
	BaseURL string
	Server  *httptest.Server
	DB      *sql.DB
	Mailer  *CapturingMailer
}

// NewTestServer boots an httptest.Server with the full router against a real DB.
func NewTestServer(t *testing.T, db *sql.DB) *TestServer {
	t.Helper()
	queries := generated.New(db)
	mc := &CapturingMailer{}

	handler := router.Build(router.Deps{
		DB:        db,
		Queries:   queries,
		JWTSecret: TestJWTSecret,
		Mailer:    mc,
		WebURL:    TestWebURL,
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(func() { srv.Close() })

	return &TestServer{
		BaseURL: srv.URL,
		Server:  srv,
		DB:      db,
		Mailer:  mc,
	}
}

// OpenTestDB opens a connection to the test MySQL.
func OpenTestDB(t *testing.T) *sql.DB {
	if t != nil {
		t.Helper()
	}
	dsn := "ttuser:ttpw@tcp(127.0.0.1:3307)/timetree_clone?parseTime=true"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		if t != nil {
			t.Fatalf("open test db: %v", err)
		}
		return nil
	}
	db.SetMaxOpenConns(8)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		if t != nil {
			t.Skipf("test database not available: %v (run: TC_DB_PORT=3307 docker compose up -d mysql)", err)
		}
		return nil
	}
	if t != nil {
		t.Cleanup(func() { db.Close() })
	}
	return db
}

// NewTestServerForMain is like NewTestServer but for use in TestMain (no *testing.T).
func NewTestServerForMain(db *sql.DB) *TestServer {
	queries := generated.New(db)
	mc := &CapturingMailer{}
	handler := router.Build(router.Deps{
		DB:        db,
		Queries:   queries,
		JWTSecret: TestJWTSecret,
		Mailer:    mc,
		WebURL:    TestWebURL,
	})
	srv := httptest.NewServer(handler)
	return &TestServer{BaseURL: srv.URL, Server: srv, DB: db, Mailer: mc}
}
