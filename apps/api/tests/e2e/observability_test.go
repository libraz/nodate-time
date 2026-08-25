package e2e

import (
	"io"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/observability"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The metrics and the profiles live on a listener of their own, and the whole
// point of that separation is that no route on the public one reaches them.
// This is what fails if a later change mounts either onto the served router --
// a mistake nothing else here would notice, because both would work.
func TestTheAdministrativeSurfaceIsNotOnThePublicPort(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)

	paths := []string{
		"/metrics",
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/profile",
		"/debug/pprof/cmdline",
		"/health/detail",
	}

	for _, path := range paths {
		resp := stackRequest(t, http.MethodGet, srv.BaseURL+path, nil)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"%s is served on the public port", path)
	}
}

// The public health endpoint stays what a load balancer needs: reachable,
// cheap, and silent about which dependency is unwell. The detailed report is
// the administrative listener's.
func TestPublicHealthStaysMinimal(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)

	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/health", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "database")
	assert.NotContains(t, string(body), "object_storage")
}

// Every response carries an identifier, so a user reporting a failure can
// quote one value that finds the whole request in the log.
func TestEveryResponseCarriesARequestIdentifier(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)

	for _, path := range []string{"/health", "/no-such-route"} {
		resp := stackRequest(t, http.MethodGet, srv.BaseURL+path, nil)
		id := resp.Header.Get(observability.RequestIDHeader)
		require.NotEmpty(t, id, "%s answered without an identifier", path)
		_, ok := observability.SanitizeRequestID(id)
		assert.True(t, ok, "%s answered with an unusable identifier %q", path, id)
	}
}

// An identifier a client chooses is one a client can reuse, and two unrelated
// requests sharing one is worse than neither carrying the proxy's. The test
// suite's servers trust no proxy, so nothing supplied here may be adopted.
func TestAClientCannotChooseItsOwnRequestIdentifier(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)

	const supplied = "chosen-by-the-client"
	resp := stackRequest(t, http.MethodGet, srv.BaseURL+"/health", map[string]string{
		observability.RequestIDHeader: supplied,
	})

	assert.NotEqual(t, supplied, resp.Header.Get(observability.RequestIDHeader))
}
