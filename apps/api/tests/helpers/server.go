package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
)

const (
	TestJWTSecret = "test-jwt-secret-for-e2e"
	TestWebURL    = "http://web.test.local"
	// The suite runs every tenant inside one workspace, the same way the
	// application does. Tenant isolation is a calendar-membership property
	// here, not a workspace one, and the isolation tests assert exactly that.
	TestWorkspaceSlug = "test"
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

// TestWorkspace resolves the workspace every test server runs in. It is
// separate from buildHandler so a test that assembles its own router.Deps
// (the OAuth suite does) cannot forget it: a zero id does not fail, it just
// matches nothing, which surfaces as a pile of confusing not-founds.
func TestWorkspace(queries *generated.Queries) workspace.Scope {
	ws, err := workspace.Ensure(context.Background(), queries, TestWorkspaceSlug, "Nodate Time (test)", "Asia/Tokyo", "JP")
	if err != nil {
		panic(fmt.Sprintf("test setup: resolve workspace: %v", err))
	}
	return ws
}

// TestWorkspacePublicID returns the workspace's external id as a string, for
// tests that need to reconstruct an object storage key.
func TestWorkspacePublicID(db *sql.DB) string {
	ws := TestWorkspace(generated.New(db))
	u, err := uuid.FromBytes(ws.PublicID)
	if err != nil {
		panic(fmt.Sprintf("test setup: workspace public id: %v", err))
	}
	return u.String()
}

func buildHandler(db *sql.DB, mc mailer.Mailer, sc *storage.Client) *router.Deps {
	queries := generated.New(db)
	ws := TestWorkspace(queries)
	return &router.Deps{
		DB:                db,
		Queries:           queries,
		WorkspaceID:       ws.ID,
		WorkspacePublicID: ws.PublicID,
		JWTSecret:         TestJWTSecret,
		Mailer:            mc,
		WebURL:            TestWebURL,
		Storage:           sc,
		// Tests register tenants over the email+password flow.
		PasswordLoginEnabled: true,
		// Parallel tenants register from one loopback IP; the per-IP limiter would
		// otherwise reject them with 429.
		AuthRateLimit: -1,
		// Likewise for the share budget: a parallel run reads many links from
		// the same address.
		ShareRateLimit: -1,
		// The test client is the only peer, so treating loopback as a proxy hop
		// lets a test present an arbitrary client address — including a
		// full-length IPv6 one, which an httptest listener never produces on its
		// own — the way a real deployment behind a reverse proxy would.
		TrustedProxies: LoopbackProxies(),
	}
}

// testAppOptions mirrors a same-origin deployment: the test client is not a
// browser, so no origin needs allowing.
func testAppOptions() app.Options { return app.Options{} }

// LoopbackProxies is the trusted-proxy set the test server runs with.
func LoopbackProxies() []netip.Prefix {
	return []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}
}

// NewTestServerWithMailer boots a test server that sends through the given
// mailer, for a test about delivery itself rather than about message content.
// The returned server has no CapturingMailer -- the test brought its own.
func NewTestServerWithMailer(t *testing.T, db *sql.DB, m mailer.Mailer) *TestServer {
	t.Helper()
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	deps := buildHandler(db, m, sc)
	srv := httptest.NewServer(app.NewHandler(*deps, testAppOptions()))
	t.Cleanup(func() { srv.Close() })

	return &TestServer{
		BaseURL: srv.URL,
		Server:  srv,
		DB:      db,
		Storage: sc,
		Bucket:  bucket,
	}
}

// NewTestServer boots an httptest.Server with the full router against a real DB.
func NewTestServer(t *testing.T, db *sql.DB) *TestServer {
	t.Helper()
	mc := NewCapturingMailer()
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	deps := buildHandler(db, mc, sc)
	srv := httptest.NewServer(app.NewHandler(*deps, testAppOptions()))
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
	name := os.Getenv("TC_DB_NAME")
	if name == "" {
		name = "timetree_clone"
	}
	// Same normalization the server applies, so tests observe the production
	// time semantics rather than the driver's defaults.
	dsn, err := config.NormalizeDSN(
		fmt.Sprintf("ttuser:ttpw@tcp(127.0.0.1:%s)/%s", port, name),
	)
	if err != nil {
		if t != nil {
			t.Fatalf("test db dsn: %v", err)
		}
		return nil
	}
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
	mc := NewCapturingMailer()
	sc, bucket, err := newTestStorage(context.Background())
	if err != nil {
		// We deliberately do not abort the process — when MinIO is not running
		// but TC_TEST_MINIO is set, tests will still skip individually.
		fmt.Fprintf(os.Stderr, "warn: test storage unavailable: %v\n", err)
	}
	deps := buildHandler(db, mc, sc)
	srv := httptest.NewServer(app.NewHandler(*deps, testAppOptions()))
	return &TestServer{BaseURL: srv.URL, Server: srv, DB: db, Mailer: mc, Storage: sc, Bucket: bucket}
}
