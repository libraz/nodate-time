package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// newPanickingServer serves the stack the process serves, over deps no handler
// can work with: reaching for a database that is not there dereferences nil,
// which is the shape a real bug takes. No route panics on purpose, and adding
// one would exercise a route rather than the recovery wrapped around it.
//
// The panic is logged with its stack, so this test is expected to be noisy.
func newPanickingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(app.NewHandler(router.Deps{
		JWTSecret:      helpers.TestJWTSecret,
		AuthRateLimit:  -1,
		ShareRateLimit: -1,
	}, app.Options{}))
	t.Cleanup(srv.Close)
	return srv
}

// A panic must come back as the ordinary error envelope. Unrecovered it closes
// the connection with nothing written, so the client reports a network error
// instead of a failure and none of its error handling runs.
//
// This one needs neither database nor storage: the failure is manufactured out
// of empty deps, so it runs whether or not integration mode is on.
func TestServedStackTurnsAPanicIntoAnErrorResponse(t *testing.T) {
	t.Parallel()
	srv := newPanickingServer(t)

	status, body, headers := helpers.DoJSONFull(t, http.MethodPost, srv.URL+"/auth/refresh", "",
		map[string]any{"refreshToken": "no-database-behind-this"}, nil)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, "application/json", headers.Get("Content-Type"))

	var envelope struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope),
		"a panic should answer with the error envelope, got %s", string(body))
	require.Equal(t, "INTERNAL.UNEXPECTED", envelope.Code)
	require.Equal(t, http.StatusInternalServerError, envelope.Status)

	// Recovery sits outside the security headers, so they are on the failure
	// too -- a 500 is still a response a browser reads.
	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", headers.Get("X-Frame-Options"))
}

// And the process serves the next request: one bad request must not take the
// server down with it.
func TestServedStackKeepsServingAfterAPanic(t *testing.T) {
	t.Parallel()
	srv := newPanickingServer(t)

	status, _, _ := helpers.DoJSONFull(t, http.MethodPost, srv.URL+"/auth/refresh", "",
		map[string]any{"refreshToken": "no-database-behind-this"}, nil)
	require.Equal(t, http.StatusInternalServerError, status)

	resp, err := http.Get(srv.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
