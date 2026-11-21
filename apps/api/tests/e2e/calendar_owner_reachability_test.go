package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// An owner who left the workspace can no longer administer the calendar, so
// they neither count toward the last-owner guard nor show up as a member.
func TestOwnerOutsideTheWorkspaceIsNotAReachableOwner(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + a.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/members", a.AccessToken,
		map[string]any{"userId": b.UserID, "role": "editor"}, nil)
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+b.UserID+"/role", a.AccessToken,
		map[string]any{"role": "owner"}, nil)

	_, err := testDB.ExecContext(t.Context(),
		`UPDATE workspace_members wm JOIN users u ON u.id = wm.user_id
		 SET wm.enabled = FALSE WHERE u.email = ?`, b.Email)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := testDB.Exec(
			`UPDATE workspace_members wm JOIN users u ON u.id = wm.user_id
			 SET wm.enabled = TRUE WHERE u.email = ?`, b.Email)
		require.NoError(t, err)
	})

	lastOwner := func(status int, body []byte) {
		t.Helper()
		require.Equal(t, http.StatusBadRequest, status, "%s", body)
		var apiErr struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(body, &apiErr))
		require.Equal(t, "MEMBER.LAST_ADMIN", apiErr.Code)
	}

	lastOwner(helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+a.UserID, a.AccessToken, nil))

	var members []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", a.AccessToken, nil, &members)
	require.Len(t, members, 1)
	require.Equal(t, a.UserID, members[0].ID)

	var single calendarBody
	helpers.DoJSON(t, http.MethodGet, calURL, a.AccessToken, nil, &single)
	require.Equal(t, 1, single.MemberCount, "the count matches the list")
}
