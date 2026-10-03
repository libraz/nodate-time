package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// An edited occurrence shows its own attendees, so an answer or an edit grant
// given on that occurrence is written to the same rows it is read back from,
// and the grant decides who may change that occurrence.
func TestAttendeeStateOfAnEditedOccurrenceIsTheOneItShows(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	extra := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	joinAs(t, calURL, owner.AccessToken, extra, "editor")

	body := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", guest.UserID)
	body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
	var series struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, body, &series)
	edited := calURL + "/events/" + series.ID + "_20260410"
	untouched := calURL + "/events/" + series.ID + "_20260417"

	type attendee struct {
		UserID  string `json:"userId"`
		Rsvp    string `json:"rsvp"`
		CanEdit bool   `json:"canEdit"`
	}
	attendeeOf := func(url, userID string) attendee {
		t.Helper()
		var got struct {
			Attendees []attendee `json:"attendees"`
		}
		helpers.DoJSON(t, http.MethodGet, url, owner.AccessToken, nil, &got)
		for _, a := range got.Attendees {
			if a.UserID == userID {
				return a
			}
		}
		t.Fatalf("%s is not an attendee of %s: %+v", userID, url, got.Attendees)
		return attendee{}
	}
	rsvp := func(url, token, answer string) (int, string) {
		t.Helper()
		status, raw := helpers.DoJSONStatus(t, http.MethodPut, url+"/rsvp", token, map[string]any{"rsvp": answer})
		return status, string(raw)
	}

	// Answered on the series, then the occurrence is edited: the answer stays.
	status, raw := rsvp(calURL+"/events/"+series.ID, guest.AccessToken, "accepted")
	require.Equal(t, http.StatusOK, status, raw)
	helpers.DoJSON(t, http.MethodPut, edited+"?scope=this", owner.AccessToken,
		eventBody("Moved this week", "2026-04-10T17:00:00+09:00", "2026-04-10T18:00:00+09:00",
			guest.UserID, extra.UserID), nil)
	require.Equal(t, "accepted", attendeeOf(edited, guest.UserID).Rsvp, "editing the occurrence keeps the answer")

	// An answer on the edited occurrence is what that occurrence shows, and
	// only that occurrence.
	status, raw = rsvp(edited, guest.AccessToken, "declined")
	require.Equal(t, http.StatusOK, status, raw)
	require.Equal(t, "declined", attendeeOf(edited, guest.UserID).Rsvp)
	require.Equal(t, "accepted", attendeeOf(untouched, guest.UserID).Rsvp)

	// Someone invited to this occurrence alone can answer it.
	status, raw = rsvp(edited, extra.AccessToken, "tentative")
	require.Equal(t, http.StatusOK, status, raw)
	require.Equal(t, "tentative", attendeeOf(edited, extra.UserID).Rsvp)

	// An edit grant on the occurrence shows on it and lets the guest change
	// that occurrence, and not the others.
	helpers.DoJSON(t, http.MethodPut, edited+"/attendees/"+guest.UserID, owner.AccessToken,
		map[string]any{"canEdit": true}, nil)
	require.True(t, attendeeOf(edited, guest.UserID).CanEdit)
	require.False(t, attendeeOf(untouched, guest.UserID).CanEdit)

	status, rawBytes := helpers.DoJSONStatus(t, http.MethodPut, edited+"?scope=this", guest.AccessToken,
		eventBody("Guest moved it", "2026-04-10T18:00:00+09:00", "2026-04-10T19:00:00+09:00",
			guest.UserID, extra.UserID))
	require.Equal(t, http.StatusOK, status, string(rawBytes))
	status, rawBytes = helpers.DoJSONStatus(t, http.MethodPut, untouched+"?scope=this", guest.AccessToken,
		eventBody("Not theirs", "2026-04-17T18:00:00+09:00", "2026-04-17T19:00:00+09:00", guest.UserID))
	require.Equal(t, http.StatusForbidden, status, string(rawBytes))
	require.True(t, strings.Contains(string(rawBytes), "EDIT"), string(rawBytes))
}
