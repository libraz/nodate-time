package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The origin the CORS server under test is configured to serve. The suite's
// shared server is same-origin, so cross-origin behaviour needs its own.
const corsAllowedOrigin = "https://app.test.local"

// stackRequest sends a bare request with the given headers. CORS is decided on
// headers alone, so the test needs to set them and read them back rather than
// go through the JSON helpers.
func stackRequest(t *testing.T, method, url string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A browser only sends credentials to an API that names its origin back. The
// header is set outside the routes, so no route test can catch it going away.
func TestCORSAnswersAnAllowedOrigin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})

	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/health", map[string]string{
		"Origin": corsAllowedOrigin,
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, corsAllowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
	// Header names travel canonicalized, so compare without regard to case.
	require.Contains(t, strings.ToLower(resp.Header.Get("Access-Control-Expose-Headers")), "x-ratelimit-remaining",
		"the rate-limit headers are useless to a cross-origin client that cannot read them")
}

// An origin that is not on the list gets no permission header. The response
// itself is still produced -- CORS is enforced in the browser, not here -- so
// the absence of the header is the whole assertion.
func TestCORSWithholdsPermissionFromAnUnknownOrigin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})

	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/health", map[string]string{
		"Origin": "https://not-the-app.example",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Credentials"))
}

// The preflight has to be answered before authentication runs: it carries no
// Authorization header, so a protected path that let it reach RequireAuth
// would answer 401 and the real request would never be sent.
func TestCORSAnswersThePreflightForAProtectedRoute(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})

	resp := stackRequest(t, http.MethodOptions, srv.BaseURL+"/calendars", map[string]string{
		"Origin":                         corsAllowedOrigin,
		"Access-Control-Request-Method":  http.MethodPost,
		"Access-Control-Request-Headers": "Authorization, Content-Type",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, corsAllowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	require.Equal(t, http.MethodPost, resp.Header.Get("Access-Control-Allow-Methods"))
	require.Contains(t, resp.Header.Get("Access-Control-Allow-Headers"), "Authorization")
	require.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
}

func TestCORSRefusesThePreflightFromAnUnknownOrigin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{
		CORSAllowedOrigins: []string{corsAllowedOrigin},
	})

	resp := stackRequest(t, http.MethodOptions, srv.BaseURL+"/calendars", map[string]string{
		"Origin":                        "https://not-the-app.example",
		"Access-Control-Request-Method": http.MethodPost,
	})
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Methods"))
}

// Configuring no origin is a same-origin deployment saying so, and it has to
// deny every origin. Nothing outside production checks the setting -- config
// validation only runs there -- so an unconfigured deployment is exactly where
// the default matters.
func TestCORSDeniesEveryOriginWhenNoneIsConfigured(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithOptions(t, testDB, app.Options{})

	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/auth/oauth/providers", map[string]string{
		"Origin": "https://anything-at-all.example",
	})
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"),
		"an unconfigured origin list must not turn into a wildcard")
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Credentials"))
}
