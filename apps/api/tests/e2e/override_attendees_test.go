package e2e

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// oaSeries creates a daily Tokyo series of five 30-minute meetings from
// 2026-09-14 with the given participants and returns its id.
func oaSeries(t *testing.T, calURL, token string, participants ...string) string {
	t.Helper()
	if participants == nil {
		participants = []string{}
	}
	var created struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, map[string]any{
		"title":          "Stand-up",
		"allDay":         false,
		"startAt":        "2026-09-14T22:00:00+09:00",
		"endAt":          "2026-09-14T22:30:00+09:00",
		"timezone":       "Asia/Tokyo",
		"participantIds": participants,
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-30T22:00:00+09:00"},
	}, &created)
	return created.ID
}

// oaEtag is the tag a write to the event id names.
func oaEtag(t *testing.T, calURL, token, id string) string {
	t.Helper()
	var got ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, token, nil, &got)
	require.NotEmpty(t, got.Etag)
	return got.Etag
}

// oaEditOccurrence edits one occurrence of a series with the given participants.
func oaEditOccurrence(t *testing.T, calURL, token, seriesID, date, title string, participants ...string) {
	t.Helper()
	if participants == nil {
		participants = []string{}
	}
	id := seriesID + "_" + date
	status, raw, _ := helpers.DoJSONFull(t, http.MethodPatch, calURL+"/events/"+id+"?scope=this", token,
		map[string]any{"title": title, "participantIds": participants},
		etagHeader(oaEtag(t, calURL, token, id)))
	require.Equal(t, http.StatusOK, status, string(raw))
}

func oaAttendees(t *testing.T, calURL, token, id string) []string {
	t.Helper()
	var got ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, token, nil, &got)
	return got.attendeeIDs()
}

// oaOverrideRows reads the attendee rows of a series' overrides straight from
// the table: user public id -> enabled.
func oaOverrideRows(t *testing.T, seriesID string) map[string]bool {
	t.Helper()
	rows, err := testDB.QueryContext(t.Context(), `
		SELECT BIN_TO_UUID(u.public_id), a.enabled
		  FROM calendar_event_attendees a
		  JOIN calendar_events o ON o.id = a.event_id
		  JOIN calendar_events p ON p.id = o.recurrence_parent_id
		  JOIN users u ON u.id = a.user_id
		 WHERE p.public_id = UUID_TO_BIN(?)`, seriesID)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		var enabled bool
		require.NoError(t, rows.Scan(&id, &enabled))
		out[id] = enabled
	}
	require.NoError(t, rows.Err())
	return out
}

// Taking everyone off one occurrence must leave the series' people on the
// other occurrences, and the occurrence must not fall back to them.
func TestRemovingEveryoneFromAnOccurrenceKeepsItEmpty(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	id := oaSeries(t, calURL, owner.AccessToken, guest.UserID)

	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up (solo)")

	require.Empty(t, oaAttendees(t, calURL, owner.AccessToken, id+"_20260916"))
	require.Equal(t, []string{guest.UserID}, oaAttendees(t, calURL, owner.AccessToken, id+"_20260917"))
	require.Equal(t, []string{guest.UserID}, oaAttendees(t, calURL, owner.AccessToken, id))
	require.Equal(t, map[string]bool{guest.UserID: false}, oaOverrideRows(t, id),
		"the person left out is recorded as a disabled row")

	// Editing the same occurrence again with nobody keeps it empty.
	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up (solo, renamed)")
	require.Empty(t, oaAttendees(t, calURL, owner.AccessToken, id+"_20260916"))
}

// Somebody added to an occurrence after everyone was removed shows up on it
// alone; a person added to the series afterwards does not.
func TestAddingToAnEmptiedOccurrenceLater(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	late := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	joinAs(t, calURL, owner.AccessToken, late, "editor")
	id := oaSeries(t, calURL, owner.AccessToken, guest.UserID)

	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up")
	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up", guest.UserID)
	require.Equal(t, []string{guest.UserID}, oaAttendees(t, calURL, owner.AccessToken, id+"_20260916"))
	require.Equal(t, map[string]bool{guest.UserID: true}, oaOverrideRows(t, id),
		"the removed row is revived rather than duplicated")

	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up", late.UserID)
	got := oaAttendees(t, calURL, owner.AccessToken, id+"_20260916")
	require.Equal(t, []string{late.UserID}, got)
}

// An override with no attendee row follows the series, including people added
// to the series after the override was made.
func TestOverrideWithoutRowsFollowsTheSeries(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	id := oaSeries(t, calURL, owner.AccessToken)

	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260916", "Stand-up (renamed)")
	require.Empty(t, oaOverrideRows(t, id), "nothing to leave out, so no row")

	status, raw, _ := helpers.DoJSONFull(t, http.MethodPatch, calURL+"/events/"+id, owner.AccessToken,
		map[string]any{"participantIds": []string{guest.UserID}},
		etagHeader(oaEtag(t, calURL, owner.AccessToken, id)))
	require.Equal(t, http.StatusOK, status, string(raw))

	require.Equal(t, []string{guest.UserID}, oaAttendees(t, calURL, owner.AccessToken, id+"_20260916"))
}

func oaOverrideStarts(t *testing.T, seriesID string) []time.Time {
	t.Helper()
	rows, err := testDB.QueryContext(t.Context(), `
		SELECT o.recurrence_original_start
		  FROM calendar_events o JOIN calendar_events p ON p.id = o.recurrence_parent_id
		 WHERE p.public_id = UUID_TO_BIN(?) ORDER BY 1`, seriesID)
	require.NoError(t, err)
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var s time.Time
		require.NoError(t, rows.Scan(&s))
		out = append(out, s.UTC())
	}
	require.NoError(t, rows.Err())
	return out
}

func oaReminderStarts(t *testing.T, seriesID string) []time.Time {
	t.Helper()
	rows, err := testDB.QueryContext(t.Context(), `
		SELECT r.occurrence_start FROM calendar_event_reminders r
		  JOIN calendar_events e ON e.id = r.event_id
		 WHERE e.public_id = UUID_TO_BIN(?) ORDER BY 1`, seriesID)
	require.NoError(t, err)
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var s time.Time
		require.NoError(t, rows.Scan(&s))
		out = append(out, s.UTC())
	}
	require.NoError(t, rows.Err())
	return out
}

// Switching a recurring series between timed and all-day re-keys what names
// its occurrences -- exclusions, overrides and send records -- in the same
// write, so each keeps naming the occurrence it stood for, in both directions.
func TestAllDayToggleRewritesWhatNamesOccurrences(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	id := oaSeries(t, calURL, owner.AccessToken)
	seriesURL := calURL + "/events/" + id

	// 2026-09-16 cancelled, 2026-09-17 edited, 2026-09-18 reminded.
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, seriesURL+"_20260916?scope=this", owner.AccessToken, nil,
		etagHeader(oaEtag(t, calURL, owner.AccessToken, id+"_20260916")))
	require.Less(t, status, 300, string(raw))
	oaEditOccurrence(t, calURL, owner.AccessToken, id, "20260917", "Stand-up (edited)")
	_, err := testDB.ExecContext(t.Context(), `
		INSERT INTO calendar_event_reminders (workspace_id, event_id, occurrence_start)
		SELECT workspace_id, id, '2026-09-18 13:00:00.000' FROM calendar_events
		 WHERE public_id = UUID_TO_BIN(?)`, id)
	require.NoError(t, err)

	put := func(body map[string]any) {
		t.Helper()
		status, raw, _ := helpers.DoJSONFull(t, http.MethodPatch, seriesURL, owner.AccessToken, body,
			etagHeader(oaEtag(t, calURL, owner.AccessToken, id)))
		require.Equal(t, http.StatusOK, status, string(raw))
	}

	put(map[string]any{
		"allDay": true, "timezone": "Asia/Tokyo",
		"startDate": "2026-09-14", "endDate": "2026-09-15",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-30"},
	})
	stored := ctLoadEvent(t, id)
	require.True(t, stored.AllDay)
	require.JSONEq(t, `["2026-09-16"]`, stored.Exceptions.String)
	require.Equal(t, []time.Time{ctMidnight("2026-09-17")}, oaOverrideStarts(t, id))
	require.Equal(t, []time.Time{ctMidnight("2026-09-18")}, oaReminderStarts(t, id))

	put(map[string]any{
		"allDay": false, "timezone": "Asia/Tokyo",
		"startAt": "2026-09-14T22:00:00+09:00", "endAt": "2026-09-14T22:30:00+09:00",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-30T22:00:00+09:00"},
	})
	stored = ctLoadEvent(t, id)
	require.False(t, stored.AllDay)
	require.JSONEq(t, `["2026-09-16T13:00:00Z"]`, stored.Exceptions.String)
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return v
	}
	require.Equal(t, []time.Time{at("2026-09-17T13:00:00Z")}, oaOverrideStarts(t, id))
	require.Equal(t, []time.Time{at("2026-09-18T13:00:00Z")}, oaReminderStarts(t, id))
}

// Handing a participant the right to edit is recorded as an attendee.canEdit
// change naming that participant.
func TestGrantingCanEditIsLoggedAsAChange(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	id := oaSeries(t, calURL, owner.AccessToken, guest.UserID)

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+id+"/attendees/"+guest.UserID, owner.AccessToken,
		map[string]any{"canEdit": true}, nil)

	rows, err := testDB.QueryContext(t.Context(), `
		SELECT JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.attendeeUserId')),
		       CAST(JSON_EXTRACT(payload_json, '$.changes') AS CHAR)
		  FROM events
		 WHERE JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.eventId')) = ?
		   AND JSON_CONTAINS(JSON_EXTRACT(payload_json, '$.changes'), '"attendee.canEdit"')`, id)
	require.NoError(t, err)
	defer rows.Close()
	var who []string
	for rows.Next() {
		var attendee, changes string
		require.NoError(t, rows.Scan(&attendee, &changes))
		who = append(who, attendee)
	}
	require.NoError(t, rows.Err())
	sort.Strings(who)
	require.Equal(t, []string{guest.UserID}, who)
}
