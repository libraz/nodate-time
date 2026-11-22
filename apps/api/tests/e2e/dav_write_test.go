package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// davWriteEvent is what a write test reads back over REST.
type davWriteEvent struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Location           string   `json:"location"`
	Memo               string   `json:"memo"`
	AllDay             bool     `json:"allDay"`
	StartDate          *string  `json:"startDate"`
	ShowAs             string   `json:"showAs"`
	Visibility         string   `json:"visibility"`
	NotificationOffset *int     `json:"notificationOffset"`
	ParticipantIDs     []string `json:"participantIds"`
	RecurrenceDate     *string  `json:"recurrenceDate"`
}

const davJuneRange = "?start=2026-06-01&end=2026-06-30&tz=Asia/Tokyo"

func davJune(t *testing.T, calURL, token string) []davWriteEvent {
	t.Helper()
	return listedItems[davWriteEvent](t, calURL+"/events"+davJuneRange, token)
}

// davICS wraps VEVENT lines in a calendar; each argument is one VEVENT body.
func davICS(vevents ...string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//e2e//EN\r\n")
	for _, v := range vevents {
		b.WriteString("BEGIN:VEVENT\r\nDTSTAMP:20260101T000000Z\r\n")
		b.WriteString(strings.ReplaceAll(strings.TrimSpace(v), "\n", "\r\n"))
		b.WriteString("\r\nEND:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// put sends a PUT of body to p, with extra headers.
func (a *davAccount) put(t *testing.T, p, body string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	h := map[string]string{"Content-Type": "text/calendar; charset=utf-8"}
	for k, v := range headers {
		h[k] = v
	}
	return a.do(t, http.MethodPut, p, h, body)
}

func (a *davAccount) etag(t *testing.T, p string) (string, string) {
	t.Helper()
	status, h, body := a.do(t, http.MethodGet, p, nil, "")
	require.Equal(t, http.StatusOK, status, body)
	return h.Get("ETag"), body
}

const davSingle = `UID:single@client
SUMMARY:Lunch
LOCATION:Cafe
DTSTART;TZID=Asia/Tokyo:20260610T120000
DTEND;TZID=Asia/Tokyo:20260610T130000`

// TestDAVWriteSingleEventLifecycle verifies create, update and delete of a
// one-off event through PUT and DELETE, with the REST view following each.
func TestDAVWriteSingleEventLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	a := newDAVAccount(t, owner)
	p := a.calendar(owner.CalendarID) + "lunch.ics"

	status, h, body := a.put(t, p, davICS(davSingle), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusCreated, status, body)
	require.Empty(t, h.Get("ETag"), "a stored body is not what the client sent")

	// A second PUT of the same new resource updates it, not a second event.
	status, _, body = a.put(t, p, davICS(davSingle), nil)
	require.Less(t, status, 300, body)
	evts := davJune(t, calURL, owner.AccessToken)
	require.Len(t, evts, 1)
	require.Equal(t, "Lunch", evts[0].Title)
	require.Equal(t, "Cafe", evts[0].Location)
	require.Nil(t, evts[0].NotificationOffset)

	status, _, _ = a.put(t, p, davICS(davSingle), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusPreconditionFailed, status)

	old, _ := a.etag(t, p)
	edited := strings.Replace(davSingle, "SUMMARY:Lunch", "SUMMARY:Long lunch", 1) +
		"\nBEGIN:VALARM\nACTION:DISPLAY\nTRIGGER:-PT10M\nEND:VALARM"
	status, _, body = a.put(t, p, davICS(edited), map[string]string{"If-Match": old})
	require.Less(t, status, 300, body)
	evts = davJune(t, calURL, owner.AccessToken)
	require.Len(t, evts, 1)
	require.Equal(t, "Long lunch", evts[0].Title)
	require.NotNil(t, evts[0].NotificationOffset)
	require.Equal(t, 10, *evts[0].NotificationOffset)

	status, _, _ = a.put(t, p, davICS(davSingle), map[string]string{"If-Match": old})
	require.Equal(t, http.StatusPreconditionFailed, status, "a stale ETag must not overwrite")
	status, _, _ = a.do(t, http.MethodDelete, p, map[string]string{"If-Match": old}, "")
	require.Equal(t, http.StatusPreconditionFailed, status)

	status, _, _ = a.do(t, http.MethodDelete, p, nil, "")
	require.Equal(t, http.StatusNoContent, status)
	require.Empty(t, davJune(t, calURL, owner.AccessToken))
	status, _, _ = a.do(t, http.MethodGet, p, nil, "")
	require.Equal(t, http.StatusNotFound, status)

	// The name is free again once its event is gone.
	status, _, body = a.put(t, p, davICS(davSingle), map[string]string{"If-None-Match": "*"})
	require.Equal(t, http.StatusCreated, status, body)
	require.Len(t, davJune(t, calURL, owner.AccessToken), 1)
}

// TestDAVWriteKeepsWhatICalendarDoesNotCarry verifies that a PUT through the
// default name of a REST-created event changes only what the body changed:
// participants, a TENTATIVE show-as and an alarm the client cannot express
// stay as stored.
func TestDAVWriteKeepsWhatICalendarDoesNotCarry(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")
	id := davCreate(t, calURL, owner.AccessToken, map[string]any{
		"title": "Review", "showAs": "tentative", "notificationOffset": 15,
		"participantIds": []string{member.UserID},
	})

	a := newDAVAccount(t, owner)
	p := davObjectPath(a.calendar(owner.CalendarID), id)
	_, body := a.etag(t, p)
	require.Contains(t, body, "TRANSP:OPAQUE")

	edited := strings.Replace(body, "SUMMARY:Review", "SUMMARY:Review notes", 1)
	edited = strings.Replace(edited, "TRIGGER:-PT15M", "TRIGGER;RELATED=END:PT0S", 1)
	status, _, resp := a.put(t, p, edited, nil)
	require.Less(t, status, 300, resp)

	evts := davJune(t, calURL, owner.AccessToken)
	require.Len(t, evts, 1)
	require.Equal(t, "Review notes", evts[0].Title)
	require.Equal(t, "tentative", evts[0].ShowAs)
	require.Equal(t, []string{member.UserID}, evts[0].ParticipantIDs)
	require.NotNil(t, evts[0].NotificationOffset, "an alarm the calendar cannot store leaves the stored one")
	require.Equal(t, 15, *evts[0].NotificationOffset)

	// Dropping CLASS and TRANSP says nothing; dropping the alarm clears it.
	_, body = a.etag(t, p)
	var kept []string
	inAlarm := false
	for _, line := range strings.Split(body, "\r\n") {
		switch {
		case line == "BEGIN:VALARM":
			inAlarm = true
		case line == "END:VALARM":
			inAlarm = false
		case inAlarm, strings.HasPrefix(line, "CLASS:"), strings.HasPrefix(line, "TRANSP:"):
		default:
			kept = append(kept, line)
		}
	}
	status, _, resp = a.put(t, p, strings.Join(kept, "\r\n"), nil)
	require.Less(t, status, 300, resp)
	evts = davJune(t, calURL, owner.AccessToken)
	require.Equal(t, "tentative", evts[0].ShowAs)
	require.Nil(t, evts[0].NotificationOffset)

	// CLASS sent is CLASS written.
	status, _, resp = a.put(t, p, strings.Replace(strings.Join(kept, "\r\n"), "END:VEVENT", "CLASS:PRIVATE\r\nEND:VEVENT", 1), nil)
	require.Less(t, status, 300, resp)
	require.Equal(t, "private", davJune(t, calURL, owner.AccessToken)[0].Visibility)
}

// TestDAVWriteRefusals verifies what a PUT may not do: write a reserved name
// or UID, reuse a UID another visible series holds, or store data the
// calendar cannot hold.
func TestDAVWriteRefusals(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")
	a := newDAVAccount(t, owner)
	cal := a.calendar(owner.CalendarID)

	status, _, _ := a.put(t, cal+"ndt-mine.ics", davICS(davSingle), nil)
	require.Equal(t, http.StatusForbidden, status, "reserved name")
	status, _, _ = a.put(t, cal+"x.ics", davICS(strings.Replace(davSingle, "single@client", "abc@nodate-time", 1)), nil)
	require.Equal(t, http.StatusForbidden, status, "reserved UID")

	status, _, body := a.put(t, cal+"one.ics", davICS(davSingle), nil)
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = a.put(t, cal+"two.ics", davICS(davSingle), nil)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "no-uid-conflict")

	// A UID held by a series the writer cannot see does not tell them it exists.
	m := newDAVAccount(t, member)
	hidden := strings.Replace(davSingle, "single@client", "hidden@client", 1)
	status, _, body = m.put(t, m.calendar(owner.CalendarID)+"hidden.ics", davICS(hidden+"\nCLASS:CONFIDENTIAL"), nil)
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = a.put(t, cal+"mine.ics", davICS(hidden), nil)
	require.Equal(t, http.StatusCreated, status, body)

	for name, c := range map[string]struct {
		text string
		want string
	}{
		"empty SUMMARY": {davICS(strings.Replace(davSingle, "SUMMARY:Lunch", "SUMMARY:", 1)), "valid-calendar-data"},
		"RDATE":         {davICS(davSingle + "\nRDATE;TZID=Asia/Tokyo:20260611T120000"), "valid-calendar-data"},
		"VTODO": {"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//e2e//EN\r\nBEGIN:VTODO\r\nUID:todo@client\r\n" +
			"DTSTAMP:20260101T000000Z\r\nSUMMARY:x\r\nEND:VTODO\r\nEND:VCALENDAR\r\n", "supported-calendar-component"},
	} {
		status, _, body := a.put(t, cal+"bad.ics", c.text, nil)
		require.Equal(t, http.StatusConflict, status, name)
		require.Contains(t, body, c.want, name)
	}
}

// TestDAVWritePermissions verifies that a viewer cannot write over CalDAV and
// that an editor cannot rewrite or delete another member's event, as over REST.
func TestDAVWritePermissions(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	editor := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, viewer, "viewer")
	joinAs(t, calURL, owner.AccessToken, editor, "editor")
	id := davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "Owner's"})

	v := newDAVAccount(t, viewer)
	cal := v.calendar(owner.CalendarID)
	status, _, body := v.put(t, cal+"v.ics", davICS(davSingle), nil)
	require.Equal(t, http.StatusForbidden, status)
	require.NotContains(t, body, "<", "a permission refusal carries no DAV error element")
	status, _, _ = v.do(t, http.MethodDelete, davObjectPath(cal, id), nil, "")
	require.Equal(t, http.StatusForbidden, status)

	e := newDAVAccount(t, editor)
	p := davObjectPath(e.calendar(owner.CalendarID), id)
	_, text := e.etag(t, p)
	status, _, _ = e.put(t, p, strings.Replace(text, "SUMMARY:Owner's", "SUMMARY:Mine now", 1), nil)
	require.Equal(t, http.StatusForbidden, status)
	status, _, _ = e.do(t, http.MethodDelete, p, nil, "")
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "Owner's", davJune(t, calURL, owner.AccessToken)[0].Title)
}

// davFridays is a weekly Friday series at 10:00 Tokyo through June 2026.
const davFridays = `UID:fridays@client
SUMMARY:Standup
DTSTART;TZID=Asia/Tokyo:20260605T100000
DTEND;TZID=Asia/Tokyo:20260605T110000
RRULE:FREQ=WEEKLY;BYDAY=FR;UNTIL=20260630T145959Z`

// TestDAVWriteSeries verifies that cancelled and changed occurrences in a PUT
// reach the series as REST exceptions, and that moving the series moves them.
func TestDAVWriteSeries(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	a := newDAVAccount(t, owner)
	p := a.calendar(owner.CalendarID) + "standup.ics"

	moved := `UID:fridays@client
RECURRENCE-ID;TZID=Asia/Tokyo:20260619T100000
SUMMARY:Standup (late)
DTSTART;TZID=Asia/Tokyo:20260619T150000
DTEND;TZID=Asia/Tokyo:20260619T160000`
	status, _, body := a.put(t, p, davICS(davFridays+"\nEXDATE;TZID=Asia/Tokyo:20260612T100000", moved), nil)
	require.Equal(t, http.StatusCreated, status, body)

	byDate := func() map[string]string {
		out := map[string]string{}
		for _, e := range davJune(t, calURL, owner.AccessToken) {
			require.NotNil(t, e.RecurrenceDate, e.ID)
			out[*e.RecurrenceDate] = e.Title
		}
		return out
	}
	require.Equal(t, map[string]string{
		"20260605": "Standup", "20260619": "Standup (late)", "20260626": "Standup",
	}, byDate())

	// Undo the cancellation, cancel another date, retitle the changed one.
	moved2 := strings.Replace(moved, "Standup (late)", "Standup (later)", 1)
	status, _, body = a.put(t, p, davICS(davFridays+"\nEXDATE;TZID=Asia/Tokyo:20260626T100000", moved2), nil)
	require.Less(t, status, 300, body)
	require.Equal(t, map[string]string{
		"20260605": "Standup", "20260612": "Standup", "20260619": "Standup (later)",
	}, byDate())

	// A master edit that leaves the grid alone keeps the occurrences as they are.
	master := strings.Replace(davFridays, "SUMMARY:Standup", "SUMMARY:Sync", 1)
	status, _, body = a.put(t, p, davICS(master+"\nEXDATE;TZID=Asia/Tokyo:20260626T100000", moved2), nil)
	require.Less(t, status, 300, body)
	require.Equal(t, map[string]string{
		"20260605": "Sync", "20260612": "Sync", "20260619": "Standup (later)",
	}, byDate())

	// Moving the series to 09:00 renames its occurrences; the body names them anew.
	early := strings.NewReplacer("T100000", "T090000", "T110000", "T100000").Replace(master)
	moved3 := strings.Replace(moved2, "20260619T100000", "20260619T090000", 1)
	status, _, body = a.put(t, p, davICS(early+"\nEXDATE;TZID=Asia/Tokyo:20260626T090000", moved3), nil)
	require.Less(t, status, 300, body)
	require.Equal(t, map[string]string{
		"20260605": "Sync", "20260612": "Sync", "20260619": "Standup (later)",
	}, byDate())
	_, text := a.etag(t, p)
	require.Contains(t, text, "RECURRENCE-ID;TZID=Asia/Tokyo:20260619T090000")

	// A RECURRENCE-ID the rule does not generate is refused.
	stray := strings.Replace(moved3, "20260619T090000", "20260620T090000", 1)
	status, _, body = a.put(t, p, davICS(early, stray), nil)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "valid-calendar-data")

	status, _, _ = a.do(t, http.MethodDelete, p, nil, "")
	require.Equal(t, http.StatusNoContent, status)
	require.Empty(t, davJune(t, calURL, owner.AccessToken))
}
