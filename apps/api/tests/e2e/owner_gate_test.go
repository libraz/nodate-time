package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// joinAs invites a user at the given role and has them accept, returning
// nothing: the point of the helper is that the membership exists afterwards.
func joinAs(t *testing.T, calURL string, ownerToken string, joiner *helpers.TestTenant, role string) {
	t.Helper()
	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", ownerToken,
		map[string]any{"role": role}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", joiner.AccessToken, nil, nil)
}

// promote sets a member's role as the given actor and returns the status, so a
// test can assert on both the allowed and the refused direction.
func promote(t *testing.T, calURL string, actorToken string, target *helpers.TestTenant, role string) int {
	t.Helper()
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/members/"+target.UserID+"/role", actorToken,
		map[string]any{"role": role})
	return status
}

// TestManagerCannotMoveOwnership verifies that a manager may run the membership
// list but cannot hand ownership to anyone -- including an account it controls
// -- nor take it away from the owner.
func TestManagerCannotMoveOwnership(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	manager := helpers.NewTenant(t, testServerURL)
	accomplice := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, manager, "editor")
	joinAs(t, calURL, owner.AccessToken, accomplice, "editor")
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, manager, "manager"))

	// The manager's ordinary powers still work: it can move a member between
	// the roles below ownership.
	require.Equal(t, 200, promote(t, calURL, manager.AccessToken, accomplice, "viewer"))
	require.Equal(t, 200, promote(t, calURL, manager.AccessToken, accomplice, "manager"))

	// Handing ownership to a second account it controls is refused.
	require.Equal(t, 403, promote(t, calURL, manager.AccessToken, accomplice, "owner"))

	// So is taking it away from the person who has it.
	require.Equal(t, 403, promote(t, calURL, manager.AccessToken, owner, "editor"))

	// And so is the shorter path to the same end.
	removeStatus, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		calURL+"/members/"+owner.UserID, manager.AccessToken, nil)
	require.Equal(t, 403, removeStatus)

	// The owner is still the owner, and still the only one.
	var members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &members)
	owners := make([]string, 0, 1)
	for _, m := range members {
		if m.Role == "owner" {
			owners = append(owners, m.ID)
		}
	}
	require.Equal(t, []string{owner.UserID}, owners)

	// The owner can do what the manager could not.
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, accomplice, "owner"))
}

// TestManagerCannotDeleteCalendar verifies that destroying a shared calendar
// for everyone on it stays with the owner.
func TestManagerCannotDeleteCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	manager := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, manager, "editor")
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, manager, "manager"))

	deleteStatus, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, manager.AccessToken, nil)
	require.Equal(t, 403, deleteStatus)

	// The calendar is still there for everyone on it.
	stillThere, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL, manager.AccessToken, nil)
	require.Equal(t, 200, stillThere)

	// A manager keeps the settings it is meant to have.
	updateStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, manager.AccessToken,
		map[string]any{"name": "Renamed by the manager"})
	require.Equal(t, 200, updateStatus)

	// The owner can still delete it.
	ownerDelete, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, owner.AccessToken, nil)
	require.True(t, ownerDelete >= 200 && ownerDelete < 300)
}
