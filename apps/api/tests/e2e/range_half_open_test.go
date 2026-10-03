package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A listing naming only one end of its window is refused, through either door,
// rather than answered with the default window, which the caller did not ask
// for and cannot tell apart from the one they meant.
func TestListingWithOnlyOneEndIsRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	for _, q := range []string{"?start=2026-03-01", "?end=2026-03-31"} {
		status, raw := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events"+q, owner.AccessToken, nil)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", q, raw)
	}
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events?days=7", owner.AccessToken, nil)
	require.Equal(t, http.StatusOK, status)

	// The public feed reads the same parameters the same way.
	token := publicShareToken(t, calURL, owner.AccessToken)
	for _, q := range []string{"?start=2026-03-01", "?end=2026-03-31"} {
		status, raw := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/"+token+"/events"+q, "", nil)
		require.Equal(t, http.StatusBadRequest, status, "share %s: %s", q, raw)
	}
}
