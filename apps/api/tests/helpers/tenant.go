package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TestTenant represents an isolated test user with a calendar.
type TestTenant struct {
	BaseURL     string
	Email       string
	Password    string
	Name        string
	UserID      string
	AccessToken string
	CalendarID  string
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
		map[string]any{"name": "テストカレンダー", "color": "#2ECC87"},
		&calResp)
	tt.CalendarID = calResp.ID

	return tt
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}
