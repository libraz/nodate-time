package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// readEvent is the part of an event a client echoes back when it saves.
type readEvent struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	StartAt      string   `json:"startAt"`
	EndAt        string   `json:"endAt"`
	Participants []string `json:"participants"`
	OwnerID      string   `json:"ownerId"`
	Attendees    []struct {
		UserID  string `json:"userId"`
		CanEdit bool   `json:"canEdit"`
	} `json:"attendees"`
}

// echoMove is the body a drag sends: the event as read, at new times.
func echoMove(e readEvent, startAt, endAt string) map[string]any {
	body := eventBody(e.Title, startAt, endAt, e.Participants...)
	body["ownerId"] = e.OwnerID
	return body
}

func getEvent(t *testing.T, calURL, token, id string) (readEvent, string) {
	t.Helper()
	status, raw, header := helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+id, token, nil, nil)
	require.Equal(t, 200, status, string(raw))
	var e readEvent
	require.NoError(t, json.Unmarshal(raw, &e))
	return e, header.Get("ETag")
}

func occurrenceInApril(t *testing.T, calURL, token, id string) readEvent {
	t.Helper()
	var evts []readEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", token, nil, &evts)
	for _, e := range evts {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("event %s not listed", id)
	return readEvent{}
}

// TestRemovedMemberNoLongerHoldsTheirEvents verifies that taking somebody off
// a calendar takes them off its events too. A write accepts members only, so an
// event still naming the departed member as owner or participant could never
// be saved back as read -- by anyone, the calendar's owner included.
func TestRemovedMemberNoLongerHoldsTheirEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	stayer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, leaver, "editor")
	joinAs(t, calURL, owner.AccessToken, stayer, "editor")

	// A series the leaver owns, with one occurrence edited into its own row. It
	// goes first: the helper counts what the month holds.
	occurrences := createWeeklyFriday(t, calURL, leaver.AccessToken)
	occurrence := occurrences[1].ID
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occurrence+"?scope=this", leaver.AccessToken,
		eventBody("Their occurrence", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00", leaver.UserID), nil)

	// One event the leaver owns and attends with somebody staying.
	var theirs readEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", leaver.AccessToken, map[string]any{
		"title":        "Their plan",
		"allDay":       false,
		"startAt":      "2026-04-07T10:00:00+09:00",
		"endAt":        "2026-04-07T11:00:00+09:00",
		"participants": []string{leaver.UserID, stayer.UserID},
	}, &theirs)

	// One the owner made, on which the leaver holds an edit grant.
	var shared readEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title":        "Shared plan",
		"allDay":       false,
		"startAt":      "2026-04-08T10:00:00+09:00",
		"endAt":        "2026-04-08T11:00:00+09:00",
		"participants": []string{leaver.UserID},
	}, &shared)
	grant, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/events/"+shared.ID+"/attendees/"+leaver.UserID, owner.AccessToken,
		map[string]any{"canEdit": true})
	require.Equal(t, 200, grant)

	stale, staleTag := getEvent(t, calURL, owner.AccessToken, theirs.ID)
	require.Equal(t, leaver.UserID, stale.OwnerID)

	removed, body := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+leaver.UserID, owner.AccessToken, nil)
	require.Equal(t, 204, removed, string(body))

	after, _ := getEvent(t, calURL, owner.AccessToken, theirs.ID)
	require.Equal(t, owner.UserID, after.OwnerID, "the departed member's event passes to the calendar's owner")
	require.Equal(t, []string{stayer.UserID}, after.Participants)

	sharedAfter, _ := getEvent(t, calURL, owner.AccessToken, shared.ID)
	require.Empty(t, sharedAfter.Participants)

	occAfter := occurrenceInApril(t, calURL, owner.AccessToken, occurrence)
	require.Equal(t, owner.UserID, occAfter.OwnerID)
	require.Empty(t, occAfter.Participants)

	// A copy read before the departure is refused as stale, not as invalid.
	staleStatus, _ := helpers.DoJSONStatusWithHeaders(t, http.MethodPut, calURL+"/events/"+theirs.ID, owner.AccessToken,
		echoMove(stale, "2026-04-07T12:00:00+09:00", "2026-04-07T13:00:00+09:00"),
		map[string]string{"If-Match": staleTag})
	require.Equal(t, 409, staleStatus)

	// Every event echoed back as now read saves.
	moved, body := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+theirs.ID, owner.AccessToken,
		echoMove(after, "2026-04-07T12:00:00+09:00", "2026-04-07T13:00:00+09:00"))
	require.Equal(t, 200, moved, string(body))
	moved, body = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+shared.ID, owner.AccessToken,
		echoMove(sharedAfter, "2026-04-08T12:00:00+09:00", "2026-04-08T13:00:00+09:00"))
	require.Equal(t, 200, moved, string(body))
	moved, body = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occurrence+"?scope=this", owner.AccessToken,
		echoMove(occAfter, "2026-04-10T17:00:00+09:00", "2026-04-10T18:00:00+09:00"))
	require.Equal(t, 200, moved, string(body))

	// Coming back is not getting the old grant back.
	joinAs(t, calURL, owner.AccessToken, leaver, "editor")
	rejoined, _ := getEvent(t, calURL, owner.AccessToken, shared.ID)
	require.Empty(t, rejoined.Participants, "rejoining does not put somebody back on events")
	var readded readEvent
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+shared.ID, owner.AccessToken,
		eventBody("Shared plan", "2026-04-08T12:00:00+09:00", "2026-04-08T13:00:00+09:00", leaver.UserID), &readded)
	require.Len(t, readded.Attendees, 1)
	require.False(t, readded.Attendees[0].CanEdit, "a revived attendee row must not carry the grant it lost")
}

// TestMemberWhoLeavesNoLongerBlocksTheirEvents covers the other way out of a
// calendar: leaving of one's own accord.
func TestMemberWhoLeavesNoLongerBlocksTheirEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, leaver, "editor")

	var evt readEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", leaver.AccessToken, map[string]any{
		"title":        "Before leaving",
		"allDay":       false,
		"startAt":      "2026-04-14T10:00:00+09:00",
		"endAt":        "2026-04-14T11:00:00+09:00",
		"participants": []string{leaver.UserID, owner.UserID},
	}, &evt)

	left, body := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+leaver.UserID, leaver.AccessToken, nil)
	require.Equal(t, 204, left, string(body))

	after, _ := getEvent(t, calURL, owner.AccessToken, evt.ID)
	require.Equal(t, owner.UserID, after.OwnerID)
	require.Equal(t, []string{owner.UserID}, after.Participants)

	moved, body := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken,
		echoMove(after, "2026-04-14T12:00:00+09:00", "2026-04-14T13:00:00+09:00"))
	require.Equal(t, 200, moved, string(body))
}
