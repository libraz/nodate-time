package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestMemberCanManageEvents verifies that a user invited as a "member" can
// create, edit, and delete events on a calendar they do not own.
func TestMemberCanManageEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Owner invites member, member accepts.
	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil, nil)

	// Member creates an event on the owner's calendar.
	var evt struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", member.AccessToken,
		map[string]any{
			"title":   "Member-created event",
			"allDay":  false,
			"startAt": "2026-05-10T10:00:00+09:00",
			"endAt":   "2026-05-10T11:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	// Member edits the event.
	var updated struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, member.AccessToken,
		map[string]any{
			"title":              "Member-edited event",
			"allDay":             false,
			"startAt":            "2026-05-10T10:00:00+09:00",
			"endAt":              "2026-05-10T12:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &updated)
	require.Equal(t, "Member-edited event", updated.Title)

	// The owner sees the member's edit.
	var ownerView []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31", owner.AccessToken, nil, &ownerView)
	require.Len(t, ownerView, 1)
	require.Equal(t, "Member-edited event", ownerView[0].Title)

	// Member deletes the event.
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, member.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	var after []struct{ ID string }
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31", owner.AccessToken, nil, &after)
	require.Len(t, after, 0)
}

// TestRemovedMemberLosesAccess verifies that once a member is removed from a
// calendar they can no longer read its events.
func TestRemovedMemberLosesAccess(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil, nil)

	// Member has access before removal.
	beforeStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-05-01&end=2026-05-31", member.AccessToken, nil)
	require.Equal(t, 200, beforeStatus)

	// Owner removes the member.
	helpers.DoJSON(t, http.MethodDelete, calURL+"/members/"+member.UserID, owner.AccessToken, nil, nil)

	// Member no longer has access.
	afterStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-05-01&end=2026-05-31", member.AccessToken, nil)
	require.Equal(t, 403, afterStatus)
}
