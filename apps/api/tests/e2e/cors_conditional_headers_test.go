package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// An event save sends the revision it was built on in If-Match. Cross-origin,
// the browser asks first; a preflight that does not allow the header means the
// save is never sent.
func TestCORSAllowsIfMatchOnAnEventSave(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})
	resp := stackRequest(t, http.MethodOptions, srv.BaseURL+"/calendars/c/events/e", map[string]string{
		"Origin":                         corsAllowedOrigin,
		"Access-Control-Request-Method":  http.MethodPut,
		"Access-Control-Request-Headers": "authorization, content-type, if-match",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, corsAllowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	require.Contains(t, strings.ToLower(resp.Header.Get("Access-Control-Allow-Headers")), "if-match")
}

// The SPA reads an event's ETag and an export's file name off the response;
// cross-origin, script sees only the headers the API exposes.
func TestCORSExposesTheHeadersTheSPAReads(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})
	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/health", map[string]string{
		"Origin": corsAllowedOrigin,
	})
	exposed := strings.ToLower(resp.Header.Get("Access-Control-Expose-Headers"))
	for _, h := range []string{"etag", "content-disposition", "x-result-truncated"} {
		require.Contains(t, exposed, h)
	}
}
