package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestReminderReachesSomethingThatCanRaiseIt verifies a reminder set here
// leaves the product. This app raises none of its own -- nothing runs while it
// is closed, which is the whole time a reminder matters -- so the alarm in the
// exported calendar is the only thing standing between a saved reminder and
// one that never arrives.
func TestReminderReachesSomethingThatCanRaiseIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var created struct {
		ID                 string `json:"id"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":              "Clinic",
			"allDay":             false,
			"startAt":            "2026-10-01T10:00:00+09:00",
			"endAt":              "2026-10-01T11:00:00+09:00",
			"notificationOffset": 30,
		}, &created)
	require.NotNil(t, created.NotificationOffset)
	require.Equal(t, 30, *created.NotificationOffset)

	exported := fetchICS(t, calURL, tt.AccessToken)
	require.Contains(t, exported, "BEGIN:VALARM")
	require.Contains(t, exported, "TRIGGER:-PT30M")
	require.Contains(t, exported, "ACTION:DISPLAY")

	// And it survives being read back, so moving between calendars does not
	// quietly drop the reminder.
	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Imported reminders")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken,
		map[string]any{"ics": exported}, &res)
	require.Equal(t, 1, res.Imported)

	var imported []struct {
		Title              string `json:"title"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, targetURL+"/events?start=2026-10-01&end=2026-10-31",
		tt.AccessToken, nil, &imported)
	require.Len(t, imported, 1)
	require.NotNil(t, imported[0].NotificationOffset, "the reminder should survive the round trip")
	require.Equal(t, 30, *imported[0].NotificationOffset)
}

// TestAnAlarmFromAnotherProductIsRead verifies a file written elsewhere brings
// its reminders with it. An import that dropped them would leave the events
// looking complete while every reminder the person relied on was gone.
func TestAnAlarmFromAnotherProductIsRead(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "From elsewhere")

	ics := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Example//EN",
		"BEGIN:VEVENT",
		"UID:a@example.com",
		"SUMMARY:Day before",
		"DTSTART:20261102T010000Z",
		"DTEND:20261102T020000Z",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Reminder",
		"TRIGGER:-P1D",
		"END:VALARM",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:b@example.com",
		"SUMMARY:Unshowable offset",
		"DTSTART:20261103T010000Z",
		"DTEND:20261103T020000Z",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Reminder",
		"TRIGGER:-PT45M",
		"END:VALARM",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	var res importResult
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", tt.AccessToken,
		map[string]any{"ics": ics}, &res)
	require.Equal(t, 2, res.Imported)

	var events []struct {
		Title              string `json:"title"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-11-01&end=2026-11-30",
		tt.AccessToken, nil, &events)
	require.Len(t, events, 2)

	byTitle := map[string]*int{}
	for _, e := range events {
		byTitle[e.Title] = e.NotificationOffset
	}
	require.NotNil(t, byTitle["Day before"], "a day-before alarm is one the picker offers")
	require.Equal(t, 1440, *byTitle["Day before"])
	// An offset the picker cannot show is left off rather than kept where its
	// owner can neither see it nor change it.
	require.Nil(t, byTitle["Unshowable offset"])
}
