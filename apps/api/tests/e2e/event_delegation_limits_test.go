package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestDelegateChangesFieldsButNotWhoHoldsTheEvent verifies that an edit grant
// covers what the event says and nothing about who controls it. A delegate
// who could take the event over, or drop another grant holder, could pass the
// grant on or keep the owner from revoking it.
func TestDelegateChangesFieldsButNotWhoHoldsTheEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	delegate := helpers.NewTenant(t, testServerURL)
	peer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, delegate, "editor")
	joinAs(t, calURL, owner.AccessToken, peer, "editor")

	occurrences := createWeeklyFriday(t, calURL, owner.AccessToken)
	series := occurrences[0].ID
	occurrence := occurrences[1].ID
	everyone := []string{delegate.UserID, peer.UserID}
	put := func(id string, body map[string]any) int {
		t.Helper()
		status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+id, delegate.AccessToken, body)
		return status
	}
	withRule := func(body map[string]any) map[string]any {
		body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
		return body
	}

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series, owner.AccessToken,
		withRule(eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", everyone...)), nil)
	for _, who := range everyone {
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series+"/attendees/"+who, owner.AccessToken,
			map[string]any{"canEdit": true}, nil)
	}

	// Fields are the delegate's to change, and naming the current owner is
	// not a change of owner.
	keep := withRule(eventBody("Renamed by the delegate", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", everyone...))
	keep["ownerId"] = owner.UserID
	require.Equal(t, 200, put(series, keep))

	// Taking the event over is not, for the series or one occurrence.
	takeOver := withRule(eventBody("Mine now", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", everyone...))
	takeOver["ownerId"] = delegate.UserID
	require.Equal(t, 403, put(series, takeOver))
	require.Equal(t, 403, put(series+"?scope=all", takeOver))
	occTakeOver := eventBody("Mine now", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00", everyone...)
	occTakeOver["ownerId"] = delegate.UserID
	require.Equal(t, 403, put(occurrence+"?scope=this", occTakeOver))

	// Nor is dropping another grant holder, which would take their grant.
	require.Equal(t, 403, put(series, withRule(eventBody("Alone", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", delegate.UserID))))
	require.Equal(t, 403, put(occurrence+"?scope=this",
		eventBody("Alone", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00", delegate.UserID)))

	// The delegate still cannot grant or revoke directly.
	revoke, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+series+"/attendees/"+peer.UserID,
		delegate.AccessToken, map[string]any{"canEdit": false})
	require.Equal(t, 403, revoke)

	var after readEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series, owner.AccessToken, nil, &after)
	require.Equal(t, owner.UserID, after.OwnerID)
	require.ElementsMatch(t, everyone, after.Participants)
	for _, a := range after.Attendees {
		require.True(t, a.CanEdit, "no refused request may have touched a grant")
	}

	// A participant removed by the owner loses the grant with the row, so a
	// delegate re-adding them cannot hand it back.
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series, owner.AccessToken,
		withRule(eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", delegate.UserID)), nil)
	var readded readEvent
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series, delegate.AccessToken,
		withRule(eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", everyone...)), &readded)
	for _, a := range readded.Attendees {
		require.Equal(t, a.UserID == delegate.UserID, a.CanEdit, "only the grant never taken away survives")
	}
}

// TestDemotedOwnerCannotGrantEditOnTheirEvent verifies that owning an event does
// not outlive the role that allowed writing to the calendar: a member demoted
// to viewer keeps their events but may not delegate them.
func TestDemotedOwnerCannotGrantEditOnTheirEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	demoted := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, demoted, "editor")
	joinAs(t, calURL, owner.AccessToken, guest, "editor")

	var evt readEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", demoted.AccessToken, map[string]any{
		"title":        "Made while an editor",
		"allDay":       false,
		"startAt":      "2026-05-12T10:00:00+09:00",
		"endAt":        "2026-05-12T11:00:00+09:00",
		"participants": []string{guest.UserID},
	}, &evt)
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, demoted, "viewer"))

	grant, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID+"/attendees/"+guest.UserID,
		demoted.AccessToken, map[string]any{"canEdit": true})
	require.Equal(t, 403, grant)

	var after readEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, owner.AccessToken, nil, &after)
	require.Len(t, after.Attendees, 1)
	require.False(t, after.Attendees[0].CanEdit)
}
