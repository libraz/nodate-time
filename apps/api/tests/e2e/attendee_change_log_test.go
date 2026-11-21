package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
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
				status, _ := sendAnyVersion(t, http.MethodPatch, eventURL, owner.AccessToken,
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

// participantLogRows returns the payloads of one kind recorded for an event,
// oldest first.
func participantLogRows(t *testing.T, eventID, kind string) []map[string]any {
	t.Helper()
	rows, err := testDB.Query(`
		SELECT payload_json FROM events
		 WHERE type = ? AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.eventId')) = ?
		 ORDER BY id`, kind, eventID)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw))
		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		out = append(out, payload)
	}
	require.NoError(t, rows.Err())
	return out
}

// attendeeRowEnabled reads the stored attendee row directly, so a test can tell
// a row that is hidden from one that is gone.
func attendeeRowEnabled(t *testing.T, eventID, userID string) bool {
	t.Helper()
	var enabled bool
	require.NoError(t, testDB.QueryRow(`
		SELECT a.enabled FROM calendar_event_attendees a
		  JOIN calendar_events e ON e.id = a.event_id
		  JOIN users u ON u.id = a.user_id
		 WHERE e.public_id = UUID_TO_BIN(?) AND u.public_id = UUID_TO_BIN(?)`,
		eventID, userID).Scan(&enabled))
	return enabled
}

// departFromWorkspace disables the user's workspace membership without touching
// their calendar membership, the state a workspace-level removal leaves behind.
func departFromWorkspace(t *testing.T, calendarID, userID string) {
	t.Helper()
	res, err := testDB.Exec(`
		UPDATE workspace_members wm
		  JOIN calendars c ON c.workspace_id = wm.workspace_id
		  JOIN users u ON u.id = wm.user_id
		   SET wm.enabled = FALSE
		 WHERE c.public_id = UUID_TO_BIN(?) AND u.public_id = UUID_TO_BIN(?)`, calendarID, userID)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	t.Cleanup(func() {
		_, err := testDB.Exec(`
			UPDATE workspace_members wm
			  JOIN calendars c ON c.workspace_id = wm.workspace_id
			  JOIN users u ON u.id = wm.user_id
			   SET wm.enabled = TRUE
			 WHERE c.public_id = UUID_TO_BIN(?) AND u.public_id = UUID_TO_BIN(?)`, calendarID, userID)
		require.NoError(t, err)
	})
}

// departFromCalendar disables the user's calendar membership only.
func departFromCalendar(t *testing.T, calendarID, userID string) {
	t.Helper()
	res, err := testDB.Exec(`
		UPDATE calendar_members cm
		  JOIN calendars c ON c.id = cm.calendar_id
		  JOIN users u ON u.id = cm.user_id
		   SET cm.enabled = FALSE
		 WHERE c.public_id = UUID_TO_BIN(?) AND u.public_id = UUID_TO_BIN(?)`, calendarID, userID)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func idSet(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func participantsOfEvent(t *testing.T, eventURL, token string) []string {
	t.Helper()
	var got struct {
		Participants []string `json:"participantIds"`
	}
	helpers.DoJSON(t, http.MethodGet, eventURL, token, nil, &got)
	return got.Participants
}

// Replacing the participant list records the people who arrived in one row and
// each person who left in a row of their own, both naming them.
func TestReplacingParticipantsLogsWhoArrivedAndWhoLeft(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	c := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	for _, m := range []*helpers.TestTenant{a, b, c} {
		joinAs(t, calURL, owner.AccessToken, m, "editor")
	}

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		eventBody("Lunch", "2026-06-20T12:00:00+09:00", "2026-06-20T13:00:00+09:00", a.UserID, b.UserID), &evt)
	eventURL := calURL + "/events/" + evt.ID

	// Guard: creation records the event, not its attendee rows.
	require.Empty(t, participantLogRows(t, evt.ID, eventlog.TypeAttendeeAdded))
	require.Empty(t, participantLogRows(t, evt.ID, eventlog.TypeAttendeeRemoved))

	doAnyVersion(t, http.MethodPatch, eventURL, owner.AccessToken,
		eventBody("Lunch", "2026-06-20T12:00:00+09:00", "2026-06-20T13:00:00+09:00", b.UserID, c.UserID), nil)

	added := participantLogRows(t, evt.ID, eventlog.TypeAttendeeAdded)
	require.Len(t, added, 1)
	require.EqualValues(t, 1, added[0]["count"])
	require.ElementsMatch(t, []string{c.UserID}, idSet(added[0]["userIds"]))
	require.Equal(t, c.Name, added[0]["summary"])
	require.NotContains(t, added[0], "scope", "a whole-event edit carries no occurrence scope")

	removed := participantLogRows(t, evt.ID, eventlog.TypeAttendeeRemoved)
	require.Len(t, removed, 1)
	require.Equal(t, a.UserID, removed[0]["userId"])
	require.Equal(t, a.Name, removed[0]["summary"])

	// Two arrive at once: one row, naming both.
	d := helpers.NewTenant(t, testServerURL)
	joinAs(t, calURL, owner.AccessToken, d, "editor")
	doAnyVersion(t, http.MethodPatch, eventURL, owner.AccessToken,
		eventBody("Lunch", "2026-06-20T12:00:00+09:00", "2026-06-20T13:00:00+09:00", b.UserID, c.UserID, a.UserID, d.UserID), nil)
	added = participantLogRows(t, evt.ID, eventlog.TypeAttendeeAdded)
	require.Len(t, added, 2)
	require.EqualValues(t, 2, added[1]["count"])
	require.ElementsMatch(t, []string{a.UserID, d.UserID}, idSet(added[1]["userIds"]))
	require.Len(t, strings.Split(added[1]["summary"].(string), ", "), 2)

	// Saving the same list again changes nobody and logs nothing.
	doAnyVersion(t, http.MethodPatch, eventURL, owner.AccessToken,
		eventBody("Lunch", "2026-06-20T12:00:00+09:00", "2026-06-20T13:00:00+09:00", b.UserID, c.UserID, a.UserID, d.UserID), nil)
	require.Len(t, participantLogRows(t, evt.ID, eventlog.TypeAttendeeAdded), 2)
	require.Len(t, participantLogRows(t, evt.ID, eventlog.TypeAttendeeRemoved), 1)
}

// An attendee whose workspace membership ended is not shown, so saving the
// shown list succeeds, and the row the replacement disables is housekeeping the
// editor never saw: it is not logged.
func TestDepartedAttendeeIsHiddenAndItsDisableIsNotLogged(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for name, depart := range map[string]func(*testing.T, string, string){
		"workspace": departFromWorkspace,
		"calendar":  departFromCalendar,
	} {
		t.Run(name, func(t *testing.T) {
			owner := helpers.NewTenant(t, testServerURL)
			gone := helpers.NewTenant(t, testServerURL)
			stay := helpers.NewTenant(t, testServerURL)
			fresh := helpers.NewTenant(t, testServerURL)
			calURL := testServerURL + "/calendars/" + owner.CalendarID
			for _, m := range []*helpers.TestTenant{gone, stay, fresh} {
				joinAs(t, calURL, owner.AccessToken, m, "editor")
			}
			var evt struct {
				ID string `json:"id"`
			}
			helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
				eventBody("Dinner", "2026-06-22T19:00:00+09:00", "2026-06-22T20:00:00+09:00", gone.UserID, stay.UserID), &evt)
			eventURL := calURL + "/events/" + evt.ID

			depart(t, owner.CalendarID, gone.UserID)
			require.True(t, attendeeRowEnabled(t, evt.ID, gone.UserID))
			require.Equal(t, []string{stay.UserID}, participantsOfEvent(t, eventURL, owner.AccessToken),
				"a user who no longer qualifies is not listed")

			doAnyVersion(t, http.MethodPatch, eventURL, owner.AccessToken,
				eventBody("Dinner", "2026-06-22T19:00:00+09:00", "2026-06-22T20:00:00+09:00", stay.UserID, fresh.UserID), nil)
			require.False(t, attendeeRowEnabled(t, evt.ID, gone.UserID))

			require.Empty(t, participantLogRows(t, evt.ID, eventlog.TypeAttendeeRemoved),
				"a row the editor never saw is not reported as removed")
			added := participantLogRows(t, evt.ID, eventlog.TypeAttendeeAdded)
			require.Len(t, added, 1)
			require.Equal(t, []string{fresh.UserID}, idSet(added[0]["userIds"]))
		})
	}
}

// The first edit of an occurrence copies the series' attendees onto the new
// override, so what it removes is measured against the series; the second edit
// replaces the override's own list. Both carry the occurrence's start.
func TestOccurrenceEditsLogParticipantChanges(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	for _, m := range []*helpers.TestTenant{a, b} {
		joinAs(t, calURL, owner.AccessToken, m, "editor")
	}

	body := eventBody("Weekly sync", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00", a.UserID)
	body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
	var series struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, body, &series)
	var occ []recInstance
	getItems(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", owner.AccessToken, nil, &occ)
	require.Len(t, occ, 4)
	occURL := calURL + "/events/" + occ[1].ID + "?scope=this"

	doAnyVersion(t, http.MethodPatch, occURL, owner.AccessToken,
		eventBody("Weekly sync", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00"), nil)
	removed := participantLogRows(t, series.ID, eventlog.TypeAttendeeRemoved)
	require.Len(t, removed, 1)
	require.Equal(t, a.UserID, removed[0]["userId"])
	require.Equal(t, a.Name, removed[0]["summary"])
	require.Equal(t, "occurrence", removed[0]["scope"])
	require.NotEmpty(t, removed[0]["originalStart"])
	require.Empty(t, participantLogRows(t, series.ID, eventlog.TypeAttendeeAdded))

	doAnyVersion(t, http.MethodPatch, occURL, owner.AccessToken,
		eventBody("Weekly sync", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00", b.UserID), nil)
	added := participantLogRows(t, series.ID, eventlog.TypeAttendeeAdded)
	require.Len(t, added, 1)
	require.EqualValues(t, 1, added[0]["count"])
	require.Equal(t, []string{b.UserID}, idSet(added[0]["userIds"]))
	require.Equal(t, b.Name, added[0]["summary"])
	require.NotEmpty(t, added[0]["originalStart"])
	require.Len(t, participantLogRows(t, series.ID, eventlog.TypeAttendeeRemoved), 1)
}
