package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestCrossTenantEventLookupReturns404 verifies that fetching an event which
// exists in another tenant's calendar, via one's own calendar path, returns 404
// (EventNotFound) — identical to a non-existent id — so the endpoint cannot be
// used as a cross-tenant existence oracle.
func TestCrossTenantEventLookupReturns404(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	victim := helpers.NewTenant(t, testServerURL)
	attacker := helpers.NewTenant(t, testServerURL)

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+victim.CalendarID+"/events", victim.AccessToken,
		map[string]any{
			"title":   "victim event",
			"allDay":  false,
			"startAt": "2026-05-10T10:00:00+09:00",
			"endAt":   "2026-05-10T11:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	// Attacker references the real (foreign) event id under their own calendar path.
	existingForeign, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+attacker.CalendarID+"/events/"+evt.ID, attacker.AccessToken, nil)

	// Attacker references a non-existent event id under their own calendar path.
	nonexistent, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+attacker.CalendarID+"/events/019770a0-0000-7000-8000-000000000000",
		attacker.AccessToken, nil)

	require.Equal(t, http.StatusNotFound, existingForeign, "foreign event must be indistinguishable from missing")
	require.Equal(t, http.StatusNotFound, nonexistent)
}

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
