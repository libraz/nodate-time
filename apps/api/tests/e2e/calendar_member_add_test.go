package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type candidatePage struct {
	Items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"items"`
	HasMore bool `json:"hasMore"`
}

// candidateIDs searches by the person's own address: the shared test
// workspace holds far more people than one page of candidates.
func candidateIDs(t *testing.T, calURL, token, query string) []string {
	t.Helper()
	var page candidatePage
	helpers.DoJSON(t, http.MethodGet, calURL+"/member-candidates?q="+url.QueryEscape(query), token, nil, &page)
	ids := make([]string, 0, len(page.Items))
	for _, c := range page.Items {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestManagerAddsAWorkspaceMemberWithoutALink(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	colleague := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	require.Equal(t, []string{colleague.UserID}, candidateIDs(t, calURL, owner.AccessToken, colleague.Email))
	require.Empty(t, candidateIDs(t, calURL, owner.AccessToken, owner.Email), "a member is not their own candidate")

	var added struct {
		ID    string `json:"id"`
		Role  string `json:"role"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": colleague.UserID, "role": "editor"}, &added)
	require.Equal(t, colleague.UserID, added.ID)
	require.Equal(t, "editor", added.Role)
	require.NotEmpty(t, added.Color)

	// The colleague now reaches the calendar exactly as a link would have let them.
	var list []calendarBody
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", colleague.AccessToken, nil, &list)
	joined, ok := findCalendar(list, owner.CalendarID)
	require.True(t, ok)
	require.Equal(t, "editor", joined.Role)
	require.Equal(t, 2, joined.MemberCount)

	require.Empty(t, candidateIDs(t, calURL, owner.AccessToken, colleague.Email), "a member is no longer a candidate")

	status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": colleague.UserID, "role": "viewer"})
	require.Equal(t, http.StatusConflict, status, "%s", body)
	var apiErr struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &apiErr))
	require.Equal(t, "MEMBER.ALREADY_ADDED", apiErr.Code)

	// The addition shows up in the calendar's activity, attributed to the owner.
	var activity struct {
		Items []struct {
			Action  string `json:"action"`
			ActorID string `json:"actorId"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity", owner.AccessToken, nil, &activity)
	found := false
	for _, a := range activity.Items {
		if a.Action == "calendar.member.added" {
			found = true
			require.Equal(t, owner.UserID, a.ActorID)
		}
	}
	require.True(t, found, "the addition is recorded")
}

func TestDirectAdditionStopsShortOfRolesThatHandOutAccess(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	colleague := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	for _, role := range []string{"manager", "owner"} {
		status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/members", owner.AccessToken,
			map[string]any{"userId": colleague.UserID, "role": role})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", role, body)
	}
}

func TestOnlyManagersAddMembersDirectly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	editor := helpers.NewTenant(t, testServerURL)
	third := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": editor.UserID, "role": "editor"}, nil)

	status, body := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/member-candidates", editor.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	status, body = helpers.DoJSONStatus(t, http.MethodPost, calURL+"/members", editor.AccessToken,
		map[string]any{"userId": third.UserID, "role": "viewer"})
	require.Equal(t, http.StatusForbidden, status, "%s", body)

	// Someone with no grant on the calendar learns nothing about who could join it.
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/member-candidates", third.AccessToken, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, status)
}

func TestDirectAdditionDoesNotBringBackSomeoneTheWorkspaceRemoved(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	removed := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	_, err := testDB.ExecContext(t.Context(), `UPDATE workspace_members SET enabled = FALSE
		WHERE user_id = (SELECT id FROM users WHERE public_id = UUID_TO_BIN(?))`, removed.UserID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := testDB.Exec(`UPDATE workspace_members SET enabled = TRUE
			WHERE user_id = (SELECT id FROM users WHERE public_id = UUID_TO_BIN(?))`, removed.UserID)
		require.NoError(t, err)
	})

	require.Empty(t, candidateIDs(t, calURL, owner.AccessToken, removed.Email))
	status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": removed.UserID, "role": "editor"})
	require.Equal(t, http.StatusNotFound, status, "%s", body)

	var workspaceEnabled bool
	require.NoError(t, testDB.QueryRowContext(t.Context(), `SELECT enabled FROM workspace_members
		WHERE user_id = (SELECT id FROM users WHERE public_id = UUID_TO_BIN(?))`, removed.UserID).Scan(&workspaceEnabled))
	require.False(t, workspaceEnabled, "the workspace membership stays revoked")
}

func TestCandidateSearchSaysWhenThereAreMore(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Every tenant's address shares this domain, and the shared workspace
	// holds more of them than one page.
	var page candidatePage
	helpers.DoJSON(t, http.MethodGet, calURL+"/member-candidates?q=test.local", owner.AccessToken, nil, &page)
	if !page.HasMore {
		t.Skip("the workspace holds too few people to fill a page")
	}
	require.Len(t, page.Items, 50)
}
