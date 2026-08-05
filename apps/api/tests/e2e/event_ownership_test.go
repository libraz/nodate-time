package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// eventBody is a full-replace update payload for an event, which the contract
// requires to carry every content field.
func eventBody(title, startAt, endAt string, participants ...string) map[string]any {
	if participants == nil {
		participants = []string{}
	}
	return map[string]any{
		"title":              title,
		"allDay":             false,
		"startAt":            startAt,
		"endAt":              endAt,
		"location":           "",
		"memo":               "",
		"url":                "",
		"notificationOffset": nil,
		"participants":       participants,
		"ownerId":            nil,
		"recurrenceRule":     nil,
	}
}

// TestEditorCannotRewriteAnotherMembersEvent verifies that sharing a calendar
// does not hand every editor on it the right to rewrite everyone else's plans.
func TestEditorCannotRewriteAnotherMembersEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	editor := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, editor, "editor")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Appointment",
			"allDay":  false,
			"startAt": "2026-06-10T10:00:00+09:00",
			"endAt":   "2026-06-10T11:00:00+09:00",
		}, &evt)

	// The editor can see it -- this is a shared calendar.
	getStatus, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+evt.ID, editor.AccessToken, nil)
	require.Equal(t, 200, getStatus)

	// But not rewrite it.
	updateStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, editor.AccessToken,
		eventBody("Rewritten", "2026-06-10T15:00:00+09:00", "2026-06-10T16:00:00+09:00"))
	require.Equal(t, 403, updateStatus)

	deleteStatus, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, editor.AccessToken, nil)
	require.Equal(t, 403, deleteStatus)

	// Nor reach it through the things that hang off it.
	checklistStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events/"+evt.ID+"/checklist",
		editor.AccessToken, map[string]any{"title": "added by somebody else"})
	require.Equal(t, 403, checklistStatus)

	// The editor's own events stay their own business.
	var mine struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", editor.AccessToken,
		map[string]any{
			"title":   "My own plan",
			"allDay":  false,
			"startAt": "2026-06-11T10:00:00+09:00",
			"endAt":   "2026-06-11T11:00:00+09:00",
		}, &mine)
	ownStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+mine.ID, editor.AccessToken,
		eventBody("Moved my own plan", "2026-06-11T15:00:00+09:00", "2026-06-11T16:00:00+09:00"))
	require.Equal(t, 200, ownStatus)

	// And whoever runs the calendar still can.
	adminStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+mine.ID, owner.AccessToken,
		eventBody("Moved by the owner", "2026-06-11T17:00:00+09:00", "2026-06-11T18:00:00+09:00"))
	require.Equal(t, 200, adminStatus)

	// The original event is untouched.
	var after struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, owner.AccessToken, nil, &after)
	require.Equal(t, "Appointment", after.Title)
}

// TestDelegatedAttendeeCanEdit verifies the delegation the schema describes:
// an owner hands one participant the right to change the event, without
// handing them the calendar.
func TestDelegatedAttendeeCanEdit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	delegate := helpers.NewTenant(t, testServerURL)
	bystander := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, delegate, "editor")
	joinAs(t, calURL, owner.AccessToken, bystander, "editor")

	var evt struct {
		ID        string `json:"id"`
		Attendees []struct {
			UserID  string `json:"userId"`
			Rsvp    string `json:"rsvp"`
			CanEdit bool   `json:"canEdit"`
		} `json:"attendees"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":        "Hospital visit",
			"allDay":       false,
			"startAt":      "2026-06-20T10:00:00+09:00",
			"endAt":        "2026-06-20T11:00:00+09:00",
			"participants": []string{delegate.UserID, bystander.UserID},
		}, &evt)
	require.Len(t, evt.Attendees, 2)
	for _, a := range evt.Attendees {
		require.Equal(t, "pending", a.Rsvp)
		require.False(t, a.CanEdit)
	}

	// Being a participant is not yet permission to edit.
	before, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, delegate.AccessToken,
		eventBody("Moved by a participant", "2026-06-20T14:00:00+09:00", "2026-06-20T15:00:00+09:00", delegate.UserID, bystander.UserID))
	require.Equal(t, 403, before)

	// A participant cannot hand the grant to themselves.
	selfGrant, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/events/"+evt.ID+"/attendees/"+delegate.UserID, delegate.AccessToken,
		map[string]any{"canEdit": true})
	require.Equal(t, 403, selfGrant)

	// The owner grants it.
	grant, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/events/"+evt.ID+"/attendees/"+delegate.UserID, owner.AccessToken,
		map[string]any{"canEdit": true})
	require.Equal(t, 200, grant)

	after, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, delegate.AccessToken,
		eventBody("Moved by the delegate", "2026-06-20T14:00:00+09:00", "2026-06-20T15:00:00+09:00", delegate.UserID, bystander.UserID))
	require.Equal(t, 200, after)

	// The grant is one person's, not the participant list's.
	other, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, bystander.AccessToken,
		eventBody("Moved by a bystander", "2026-06-20T18:00:00+09:00", "2026-06-20T19:00:00+09:00", delegate.UserID, bystander.UserID))
	require.Equal(t, 403, other)

	// Revoking it takes the right back.
	revoke, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/events/"+evt.ID+"/attendees/"+delegate.UserID, owner.AccessToken,
		map[string]any{"canEdit": false})
	require.Equal(t, 200, revoke)
	afterRevoke, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, delegate.AccessToken,
		eventBody("Moved again", "2026-06-20T20:00:00+09:00", "2026-06-20T21:00:00+09:00", delegate.UserID, bystander.UserID))
	require.Equal(t, 403, afterRevoke)
}

// TestRsvpRoundTrip verifies that answering an invitation is reachable, is the
// answerer's own to give, and survives into what the event reports.
func TestRsvpRoundTrip(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, guest, "viewer")
	joinAs(t, calURL, owner.AccessToken, outsider, "editor")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":        "Dinner",
			"allDay":       false,
			"startAt":      "2026-06-25T18:00:00+09:00",
			"endAt":        "2026-06-25T20:00:00+09:00",
			"participants": []string{guest.UserID},
		}, &evt)

	// A read-only member still answers for themselves.
	answer, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID+"/rsvp",
		guest.AccessToken, map[string]any{"rsvp": "accepted"})
	require.Equal(t, 200, answer)

	// Somebody who was not invited has nothing to answer.
	uninvited, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID+"/rsvp",
		outsider.AccessToken, map[string]any{"rsvp": "accepted"})
	require.Equal(t, 403, uninvited)

	var read struct {
		Attendees []struct {
			UserID string `json:"userId"`
			Rsvp   string `json:"rsvp"`
		} `json:"attendees"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, owner.AccessToken, nil, &read)
	require.Len(t, read.Attendees, 1)
	require.Equal(t, guest.UserID, read.Attendees[0].UserID)
	require.Equal(t, "accepted", read.Attendees[0].Rsvp)
}
