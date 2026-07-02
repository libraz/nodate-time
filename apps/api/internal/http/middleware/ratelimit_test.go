package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientIPIgnoresSpoofedForwardedFor(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req.RemoteAddr = "192.0.2.10:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	require.Equal(t, "192.0.2.10", clientIP(req))
}
