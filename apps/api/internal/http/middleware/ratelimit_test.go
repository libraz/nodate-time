package middleware

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientIPIgnoresSpoofedForwardedForByDefault(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req.RemoteAddr = "192.0.2.10:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	require.Equal(t, "192.0.2.10", ClientIP(req, nil))
}

func TestClientIPUsesForwardedForFromTrustedProxy(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	// RemoteAddr is the configured reverse proxy, so its appended hop is trusted.
	req.RemoteAddr = "192.0.2.10:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	trusted := []netip.Prefix{netip.MustParsePrefix("192.0.2.10/32")}
	require.Equal(t, "198.51.100.77", ClientIP(req, trusted))
}

func TestClientIPSkipsClientPrependedForwardedForHop(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req.RemoteAddr = "192.0.2.10:54321"
	// The client prepended a fake IP hoping to be attributed to it; the proxy
	// appends the real connecting peer as the rightmost, untrusted hop.
	req.Header.Set("X-Forwarded-For", "8.8.8.8, 203.0.113.5")

	trusted := []netip.Prefix{netip.MustParsePrefix("192.0.2.10/32")}
	require.Equal(t, "203.0.113.5", ClientIP(req, trusted))
}
