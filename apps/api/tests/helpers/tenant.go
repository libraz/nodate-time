package helpers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TenantCalendarColor is the colour every tenant's calendar is created with,
// and therefore the colour of the owner's membership on it -- which is what
// an event they own renders in, since the colour lives on the membership
// rather than on the event.
const TenantCalendarColor = "#2ECC87"

// TestTenant represents an isolated test user with a calendar.
type TestTenant struct {
	BaseURL     string
	Email       string
	Password    string
	Name        string
	UserID      string
	AccessToken string
	CalendarID  string
	// CalendarColor is what the tenant's own membership carries, and so the
	// colour every event they own renders in.
	CalendarColor string
}

var tenantSeq atomic.Int64

// NewTenant creates a new user + calendar via the API.
func NewTenant(t *testing.T, baseURL string) *TestTenant {
	t.Helper()
	seq := tenantSeq.Add(1)
	email := fmt.Sprintf("tenant-%d-%d@test.local", seq, time.Now().UnixNano())
	password := "testpass123"
	name := fmt.Sprintf("テスト%d", seq)

	// Register
	var regResp struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	DoJSON(t, http.MethodPost, baseURL+"/auth/register", "",
		map[string]any{"name": name, "email": email, "password": password},
		&regResp)

	tt := &TestTenant{
		BaseURL:     baseURL,
		Email:       email,
		Password:    password,
		Name:        name,
		UserID:      regResp.User.ID,
		AccessToken: regResp.Token,
	}

	// Create a default calendar
	var calResp struct {
		ID string `json:"id"`
	}
	DoJSON(t, http.MethodPost, baseURL+"/calendars", tt.AccessToken,
		map[string]any{"name": "テストカレンダー", "color": TenantCalendarColor},
		&calResp)
	tt.CalendarID = calResp.ID
	tt.CalendarColor = TenantCalendarColor

	return tt
}

// MakeInstanceAdmin grants the instance-admin role to an already-registered
// tenant. There is no API that hands this out -- the first admin is created by
// the bootstrap CLI -- so a test that needs to reach the admin surface writes
// the grant the same way that CLI does.
func MakeInstanceAdmin(t *testing.T, db *sql.DB, userPublicID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO instance_admins (public_id, user_id)
		 SELECT UUID_TO_BIN(UUID()), id FROM users WHERE public_id = UUID_TO_BIN(?)
		 ON DUPLICATE KEY UPDATE revoked_at = NULL, enabled = TRUE`,
		userPublicID)
	if err != nil {
		t.Fatalf("grant instance admin: %v", err)
	}
}

// DoJSON makes an HTTP request with JSON body, attaches Bearer token, asserts 2xx, unmarshals response.
func DoJSON(t *testing.T, method, url, bearer string, body any, out any) {
	t.Helper()
	status, raw := DoJSONStatus(t, method, url, bearer, body)
	if status < 200 || status >= 300 {
		t.Fatalf("DoJSON %s %s: status %d, body: %s", method, url, status, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("DoJSON unmarshal: %v\nbody: %s", err, string(raw))
		}
	}
}

// DoJSONStatus makes an HTTP request and returns status + raw body.
func DoJSONStatus(t *testing.T, method, url, bearer string, body any) (int, []byte) {
	t.Helper()
	return DoJSONStatusWithHeaders(t, method, url, bearer, body, nil)
}

// DoJSONStatusWithHeaders is DoJSONStatus with extra request headers, for the
// cases where what is being tested is how the server reads the request itself
// (client address, content negotiation) rather than the body.
func DoJSONStatusWithHeaders(
	t *testing.T, method, url, bearer string, body any, headers map[string]string,
) (int, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// SHA256Hex is the digest a presign request declares for the bytes it is
// about to upload. Content-addressed endpoints derive the storage key from
// it, so tests pass the digest of the bytes they actually upload rather than
// a constant: a future server-side check would then still find them honest.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
