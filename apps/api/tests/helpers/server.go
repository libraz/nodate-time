package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const (
	TestJWTSecret = "test-jwt-secret-for-e2e"
	TestWebURL    = "http://web.test.local"
)

// MinIO defaults — must match compose.yml.
const (
	testMinioEndpoint = "127.0.0.1:9000"
	testMinioAccess   = "minioadmin"
	testMinioSecret   = "minioadmin"
)

var testBucketSeq atomic.Int64

type TestServer struct {
	BaseURL string
	Server  *httptest.Server
	DB      *sql.DB
	Mailer  *CapturingMailer
	Storage *storage.Client
	Bucket  string
}

// StorageEnabled reports whether E2E tests should exercise MinIO.
func StorageEnabled() bool {
	return os.Getenv("TC_TEST_MINIO") != ""
}

// newTestStorage builds a storage client against the local MinIO if enabled,
// using a unique bucket per server so parallel tests do not collide.
func newTestStorage(ctx context.Context) (*storage.Client, string, error) {
	if !StorageEnabled() {
		return nil, "", nil
	}
	endpoint := os.Getenv("TC_S3_ENDPOINT")
	if endpoint == "" {
		endpoint = testMinioEndpoint
	}
	access := os.Getenv("TC_S3_ACCESS_KEY")
	if access == "" {
		access = testMinioAccess
	}
	secret := os.Getenv("TC_S3_SECRET_KEY")
	if secret == "" {
		secret = testMinioSecret
	}
	bucket := fmt.Sprintf("nodate-test-%d-%d", time.Now().UnixNano(), testBucketSeq.Add(1))
	c, err := storage.NewClient(endpoint, access, secret, bucket, false)
	if err != nil {
		return nil, "", fmt.Errorf("new storage client: %w", err)
	}
	if err := c.EnsureBucket(ctx); err != nil {
		return nil, "", fmt.Errorf("ensure bucket %s: %w", bucket, err)
	}
	return c, bucket, nil
}

func buildHandler(db *sql.DB, mc *CapturingMailer, sc *storage.Client) *router.Deps {
	queries := generated.New(db)
	return &router.Deps{
		DB:        db,
		Queries:   queries,
		JWTSecret: TestJWTSecret,
		Mailer:    mc,
		WebURL:    TestWebURL,
		Storage:   sc,
		// Tests register tenants over the email+password flow.
		PasswordLoginEnabled: true,
		// Parallel tenants register from one loopback IP; the per-IP limiter would
		// otherwise reject them with 429.
		AuthRateLimit: -1,
	}
}

// NewTestServer boots an httptest.Server with the full router against a real DB.
func NewTestServer(t *testing.T, db *sql.DB) *TestServer {
	t.Helper()
	mc := &CapturingMailer{}
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	deps := buildHandler(db, mc, sc)
	srv := httptest.NewServer(router.Build(*deps))
	t.Cleanup(func() { srv.Close() })

	return &TestServer{
		BaseURL: srv.URL,
		Server:  srv,
		DB:      db,
		Mailer:  mc,
		Storage: sc,
		Bucket:  bucket,
	}
}

// OpenTestDB opens a connection to the test MySQL.
func OpenTestDB(t *testing.T) *sql.DB {
	if t != nil {
		t.Helper()
	}
	port := os.Getenv("TC_DB_PORT")
	if port == "" {
		port = "33306"
	}
	dsn := fmt.Sprintf("ttuser:ttpw@tcp(127.0.0.1:%s)/timetree_clone?parseTime=true", port)
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
			t.Skipf("test database not available: %v (run: docker compose up -d mysql)", err)
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
	mc := &CapturingMailer{}
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		// We deliberately do not abort the process — when MinIO is not running
		// but TC_TEST_MINIO is set, tests will still skip individually.
		fmt.Fprintf(os.Stderr, "warn: test storage unavailable: %v\n", err)
	}
	deps := buildHandler(db, mc, sc)
	srv := httptest.NewServer(router.Build(*deps))
	return &TestServer{BaseURL: srv.URL, Server: srv, DB: db, Mailer: mc, Storage: sc, Bucket: bucket}
}
