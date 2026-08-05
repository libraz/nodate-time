package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The routes alone are not the server. These assert that the handler the tests
// drive is the one the process serves — otherwise every other test in this
// package is exercising a stack that is never deployed.
func TestServedStackAppliesSecurityHeaders(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	resp, err := http.Get(testServerURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		require.Equal(t, want, resp.Header.Get(header), "missing %s", header)
	}
}

// A route that does not exist must still come back through the stack rather
// than as a bare net/http 404 with no headers on it.
func TestServedStackCoversUnknownRoutes(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	resp, err := http.Get(testServerURL + "/definitely-not-a-route")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
}
