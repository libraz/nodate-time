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
		map[string]any{"role": "member"}, &inv)
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
			"title":   "Member-edited event",
			"allDay":  false,
			"startAt": "2026-05-10T10:00:00+09:00",
			"endAt":   "2026-05-10T12:00:00+09:00",
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

// TestNonMemberCannotAccessEvents verifies that a user with no membership on a
// calendar cannot read, create, edit, or delete its events.
func TestNonMemberCannotAccessEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Owner creates an event.
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Private event",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	// Outsider cannot list events.
	listStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-05-01&end=2026-05-31", outsider.AccessToken, nil)
	require.Equal(t, 403, listStatus)

	// Outsider cannot read the single event.
	getStatus, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+evt.ID, outsider.AccessToken, nil)
	require.Equal(t, 403, getStatus)

	// Outsider cannot create an event.
	createStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", outsider.AccessToken,
		map[string]any{
			"title":   "Intruder event",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		})
	require.Equal(t, 403, createStatus)

	// Outsider cannot edit the event.
	updateStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, outsider.AccessToken,
		map[string]any{
			"title":   "Hijacked",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		})
	require.Equal(t, 403, updateStatus)

	// Outsider cannot delete the event.
	deleteStatus, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, outsider.AccessToken, nil)
	require.Equal(t, 403, deleteStatus)

	// The event still exists for the owner.
	var stillThere []struct{ ID string }
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31", owner.AccessToken, nil, &stillThere)
	require.Len(t, stillThere, 1)
}

// TestViewerIsReadOnly verifies that a user invited as a "viewer" can read
// calendar content but cannot create, edit, or delete it.
func TestViewerIsReadOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Owner invites a viewer.
	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", viewer.AccessToken, nil, nil)

	// Owner creates an event the viewer can read.
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Shared event",
			"allDay":  false,
			"startAt": "2026-05-15T09:00:00+09:00",
			"endAt":   "2026-05-15T10:00:00+09:00",
		}, &evt)

	// Viewer CAN read events.
	listStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-05-01&end=2026-05-31", viewer.AccessToken, nil)
	require.Equal(t, 200, listStatus)
	getStatus, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+evt.ID, viewer.AccessToken, nil)
	require.Equal(t, 200, getStatus)

	// Viewer CANNOT create an event.
	createStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", viewer.AccessToken,
		map[string]any{
			"title":   "Viewer event",
			"allDay":  false,
			"startAt": "2026-05-16T09:00:00+09:00",
			"endAt":   "2026-05-16T10:00:00+09:00",
		})
	require.Equal(t, 403, createStatus)

	// Viewer CANNOT edit the event.
	updateStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, viewer.AccessToken,
		map[string]any{
			"title":   "Edited by viewer",
			"allDay":  false,
			"startAt": "2026-05-15T09:00:00+09:00",
			"endAt":   "2026-05-15T10:00:00+09:00",
		})
	require.Equal(t, 403, updateStatus)

	// Viewer CANNOT delete the event.
	deleteStatus, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, viewer.AccessToken, nil)
	require.Equal(t, 403, deleteStatus)

	// Viewer CANNOT comment on the event.
	commentStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", viewer.AccessToken,
		map[string]any{"content": "viewer comment"})
	require.Equal(t, 403, commentStatus)

	// Viewer CANNOT create a memo.
	memoStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/memos", viewer.AccessToken,
		map[string]any{"title": "viewer memo", "sortOrder": 0})
	require.Equal(t, 403, memoStatus)

	// The original event is untouched.
	var stillThere []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-05-31", owner.AccessToken, nil, &stillThere)
	require.Len(t, stillThere, 1)
	require.Equal(t, "Shared event", stillThere[0].Title)
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
		map[string]any{"role": "member"}, &inv)
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
