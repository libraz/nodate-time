package e2e

import (
	"net"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Every sign-in path records the client address on the session row. The column
// is a packed 16-byte address, so anything longer than a short dotted IPv4 in
// text form overflows it and takes the whole sign-in down with it — which is
// invisible when the only client is an httptest loopback listener.
func TestSignInFromAnIPv6ClientStoresThePackedAddress(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	const clientIP = "2001:0db8:85a3:0000:0000:8a2e:0370:7334"

	email := uniqueEmail()
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPost,
		testServerURL+"/auth/register", "",
		map[string]any{"email": email, "password": "password123", "name": "IPv6 User"},
		map[string]string{"X-Forwarded-For": clientIP},
	)
	require.Equal(t, http.StatusOK, status, "register from an IPv6 client: %s", string(raw))

	var stored []byte
	require.NoError(t, testDB.QueryRow(`
		SELECT s.ip_address FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE u.email = ?
		ORDER BY s.id DESC LIMIT 1`, email).Scan(&stored))
	require.Len(t, stored, net.IPv6len, "address must be stored packed, not as text")
	require.Equal(t, net.ParseIP(clientIP).String(), net.IP(stored).String())
}

// IPv4 is stored in the same 16-byte form, so reading a row never has to guess
// the width.
func TestSignInFromAnIPv4ClientStoresTheMappedForm(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	const clientIP = "203.0.113.9"

	email := uniqueEmail()
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPost,
		testServerURL+"/auth/register", "",
		map[string]any{"email": email, "password": "password123", "name": "IPv4 User"},
		map[string]string{"X-Forwarded-For": clientIP},
	)
	require.Equal(t, http.StatusOK, status, "register from an IPv4 client: %s", string(raw))

	var stored []byte
	require.NoError(t, testDB.QueryRow(`
		SELECT s.ip_address FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE u.email = ?
		ORDER BY s.id DESC LIMIT 1`, email).Scan(&stored))
	require.Len(t, stored, net.IPv6len)
	require.Equal(t, clientIP, net.IP(stored).String())
}
