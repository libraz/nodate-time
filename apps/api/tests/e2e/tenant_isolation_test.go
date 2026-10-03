package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestForceAddMemberEndpointRemoved verifies that the direct add-member endpoint
// no longer exists, so a user cannot be forced into a calendar without accepting
// an invite, and the email-enumeration oracle it exposed is gone.
func TestForceAddMemberEndpointRemoved(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	victim := helpers.NewTenant(t, testServerURL)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+owner.CalendarID+"/members", owner.AccessToken,
		map[string]any{"email": victim.Email, "role": "editor"})
	require.GreaterOrEqual(t, status, 400, "direct add-member endpoint must no longer exist")

	// The victim must not have been force-joined: only the owner remains.
	var members []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+owner.CalendarID+"/members", owner.AccessToken, nil, &members)
	require.Len(t, members, 1, "no member may be force-added; only the owner is present")
}
