package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// attendeeLogRows counts the attendee changes recorded for one person on one
// event: an answer is rsvp.updated, an edit grant attendee.updated. Both read
// the same payload keys, the registry's eventId and userId.
func attendeeLogRows(t *testing.T, eventID, userID string) int {
	t.Helper()
	return attendeeLogRowsOfKind(t, eventID, userID, eventlog.TypeRSVPUpdated, eventlog.TypeAttendeeUpdated)
}

func attendeeLogRowsOfKind(t *testing.T, eventID, userID string, kinds ...string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(`
		SELECT COUNT(*) FROM events
		 WHERE type IN (?, ?)
		   AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.eventId')) = ?
		   AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.userId')) = ?`,
		kinds[0], kinds[len(kinds)-1], eventID, userID).Scan(&n))
	return n
}

// Handing a participant the right to edit an event is an access change, and an
// answer is part of what the event says. Each is recorded once, in the event's
// history, and gives the event a new revision the way every other write to it
// does. The answer's response reports the grant the attendee holds.
func TestAttendeeGrantAndAnswerAreRecordedAndReported(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		eventBody("Hospital visit", "2026-06-20T10:00:00+09:00", "2026-06-20T11:00:00+09:00", guest.UserID), &evt)
	eventURL := calURL + "/events/" + evt.ID
	etagOf := func() string {
		t.Helper()
		_, _, headers := helpers.DoJSONFull(t, http.MethodGet, eventURL, owner.AccessToken, nil, nil)
		return headers.Get("ETag")
	}

	before := etagOf()
	helpers.DoJSON(t, http.MethodPut, eventURL+"/attendees/"+guest.UserID, owner.AccessToken,
		map[string]any{"canEdit": true}, nil)
	afterGrant := etagOf()
	require.NotEqual(t, before, afterGrant, "the grant gives the event a new revision")
	require.Equal(t, 1, attendeeLogRowsOfKind(t, evt.ID, guest.UserID, eventlog.TypeAttendeeUpdated),
		"the grant is recorded as an attendee change")

	var answer struct {
		Rsvp    string `json:"rsvp"`
		CanEdit bool   `json:"canEdit"`
	}
	helpers.DoJSON(t, http.MethodPut, eventURL+"/rsvp", guest.AccessToken, map[string]any{"rsvp": "accepted"}, &answer)
	require.Equal(t, "accepted", answer.Rsvp)
	require.True(t, answer.CanEdit, "the answer reports the grant the attendee holds")
	require.NotEqual(t, afterGrant, etagOf(), "the answer gives the event a new revision")
	require.Equal(t, 1, attendeeLogRowsOfKind(t, evt.ID, guest.UserID, eventlog.TypeRSVPUpdated),
		"the answer is recorded as an rsvp change")
	require.Equal(t, 2, attendeeLogRows(t, evt.ID, guest.UserID))
}

// A grant racing the attendee's removal from the event either lands before
// the removal, and is recorded, or is told the person is no longer attending.
// It never reports success for a grant nobody holds.
func TestAGrantRacingTheAttendeesRemovalIsOrderedOrRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")

	for range 8 {
		var evt struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			eventBody("Check-up", "2026-06-21T10:00:00+09:00", "2026-06-21T11:00:00+09:00", guest.UserID), &evt)
		eventURL := calURL + "/events/" + evt.ID

		statuses := runTogether(2, func(i int) int {
			if i == 0 {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, eventURL, owner.AccessToken,
					eventBody("Check-up", "2026-06-21T10:00:00+09:00", "2026-06-21T11:00:00+09:00"))
				return status
			}
			status, _ := helpers.DoJSONStatus(t, http.MethodPut, eventURL+"/attendees/"+guest.UserID,
				owner.AccessToken, map[string]any{"canEdit": true})
			return status
		})
		require.Equal(t, http.StatusOK, statuses[0])
		switch statuses[1] {
		case http.StatusOK:
			require.Equal(t, 1, attendeeLogRows(t, evt.ID, guest.UserID),
				"a grant reported as made is one that was recorded before the removal")
		case http.StatusForbidden:
			require.Zero(t, attendeeLogRows(t, evt.ID, guest.UserID))
		default:
			t.Fatalf("grant answered %d", statuses[1])
		}
	}
}
