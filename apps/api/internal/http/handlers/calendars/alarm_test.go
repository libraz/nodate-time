package calendars

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// TestReminderIsWrittenIntoTheFile verifies the reminder leaves the product.
// Nothing here raises one -- no process runs while the app is closed, which is
// when a reminder is worth having -- so a reminder that stays behind in the
// database is one the person who set it will never receive.
func TestReminderIsWrittenIntoTheFile(t *testing.T) {
	ev := generated.CalendarEvent{
		PublicID:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:              "Clinic",
		Timezone:           "Asia/Tokyo",
		NotificationOffset: sql.NullInt32{Int32: 30, Valid: true},
	}
	out := buildICS("Cal", []exportEvent{{event: ev}})

	for _, want := range []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Clinic", "TRIGGER:-PT30M", "END:VALARM"} {
		if !strings.Contains(out, want) {
			t.Errorf("export should carry %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "BEGIN:VALARM") > strings.Index(out, "END:VEVENT") {
		t.Error("the alarm belongs inside the event it reminds about")
	}
}

// TestNoReminderWritesNoAlarm verifies an event without one stays silent, so a
// client does not raise a reminder nobody asked for.
func TestNoReminderWritesNoAlarm(t *testing.T) {
	ev := generated.CalendarEvent{Title: "Lunch", Timezone: "UTC"}
	if out := buildICS("Cal", []exportEvent{{event: ev}}); strings.Contains(out, "VALARM") {
		t.Errorf("an event with no reminder should carry no alarm:\n%s", out)
	}
}

func TestTriggerRendering(t *testing.T) {
	cases := map[int32]string{
		0:    "PT0S",
		5:    "-PT5M",
		60:   "-PT60M",
		1440: "-PT1440M",
	}
	for minutes, want := range cases {
		if got := icsTrigger(minutes); got != want {
			t.Errorf("icsTrigger(%d) = %q, want %q", minutes, got, want)
		}
	}
}

// TestTriggerParsing covers the forms other calendars write. Each is a
// reminder someone set in another product and expects to keep.
func TestTriggerParsing(t *testing.T) {
	cases := []struct {
		value   string
		params  []string
		minutes int32
		ok      bool
	}{
		{value: "-PT15M", minutes: 15, ok: true},
		{value: "-PT1H", minutes: 60, ok: true},
		{value: "-P1D", minutes: 1440, ok: true},
		{value: "-P1DT12H", minutes: 2160, ok: true},
		{value: "-P1W", minutes: 10080, ok: true},
		{value: "PT0S", minutes: 0, ok: true},
		{value: "-PT0S", minutes: 0, ok: true},
		// A reminder after the start is not a reminder this product can hold.
		{value: "PT10M", ok: false},
		// Sub-minute precision would have to be rounded, and a rounded reminder
		// is a different reminder.
		{value: "-PT90S", ok: false},
		// An instant is not an offset: it does not move when the event does.
		{value: "20250624T090000Z", params: []string{"VALUE=DATE-TIME"}, ok: false},
		// Relative to the end, which the stored offset cannot express.
		{value: "-PT15M", params: []string{"RELATED=END"}, ok: false},
		{value: "nonsense", ok: false},
		{value: "", ok: false},
	}
	for _, c := range cases {
		got, ok := parseTriggerMinutes(c.value, c.params)
		if ok != c.ok {
			t.Errorf("parseTriggerMinutes(%q, %v) ok = %v, want %v", c.value, c.params, ok, c.ok)
			continue
		}
		if ok && got != c.minutes {
			t.Errorf("parseTriggerMinutes(%q) = %d, want %d", c.value, got, c.minutes)
		}
	}
}

// TestAlarmDescriptionStaysOutOfTheMemo verifies the nested component is read
// as one. A VALARM repeats property names the event itself uses, and read flat
// its DESCRIPTION lands on the event -- replacing what the file said the event
// was about with the text of its reminder.
func TestAlarmDescriptionStaysOutOfTheMemo(t *testing.T) {
	ics := strings.Join([]string{
		"BEGIN:VEVENT",
		"UID:x@example.com",
		"SUMMARY:Clinic",
		"DESCRIPTION:Bring the referral letter",
		"DTSTART:20250624T010000Z",
		"DTEND:20250624T020000Z",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Reminder",
		"TRIGGER:-PT30M",
		"END:VALARM",
		"END:VEVENT",
	}, "\r\n")

	events := parseICS(ics)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].desc != "Bring the referral letter" {
		t.Errorf("the event's own description should survive its alarm, got %q", events[0].desc)
	}
	if events[0].alarmMinutes == nil || *events[0].alarmMinutes != 30 {
		t.Errorf("the alarm should be read, got %v", events[0].alarmMinutes)
	}
}

// TestOnlyShowableRemindersAreImported verifies an offset the picker cannot
// display is dropped on the way in. Kept, it would be invisible to its owner
// and cleared by their next edit without them choosing to.
func TestOnlyShowableRemindersAreImported(t *testing.T) {
	shown := int32(15)
	if got := importAlarm(&shown); !got.Valid || got.Int32 != 15 {
		t.Errorf("a reminder the picker offers should be kept, got %v", got)
	}
	odd := int32(45)
	if got := importAlarm(&odd); got.Valid {
		t.Errorf("a reminder the picker cannot show should be dropped, got %v", got)
	}
	if got := importAlarm(nil); got.Valid {
		t.Errorf("no alarm should stay no reminder, got %v", got)
	}
}

// TestFirstUsableAlarmWins verifies an event carrying several alarms keeps one.
// A file may pair a display alarm with an email one; there is a single stored
// reminder, and the one that can be shown is the one to keep.
func TestFirstUsableAlarmWins(t *testing.T) {
	ics := strings.Join([]string{
		"BEGIN:VEVENT",
		"SUMMARY:Clinic",
		"DTSTART:20250624T010000Z",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"TRIGGER:-PT10M",
		"END:VALARM",
		"BEGIN:VALARM",
		"ACTION:EMAIL",
		"TRIGGER:-P1D",
		"END:VALARM",
		"END:VEVENT",
	}, "\r\n")

	events := parseICS(ics)
	if len(events) != 1 || events[0].alarmMinutes == nil {
		t.Fatalf("expected one event with an alarm, got %+v", events)
	}
	if *events[0].alarmMinutes != 10 {
		t.Errorf("the first usable alarm should win, got %d", *events[0].alarmMinutes)
	}
}
