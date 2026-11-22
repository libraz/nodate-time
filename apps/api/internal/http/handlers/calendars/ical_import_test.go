package calendars

import (
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
)

// A parsed VEVENT lands on the store's shape: the zone resolved, CLASS,
// TRANSP and STATUS mapped, and the reminder the calendar can hold.
func TestImportEventMapsAParsedEvent(t *testing.T) {
	events, _ := ical.Parse([]byte("BEGIN:VEVENT\r\nUID:u\r\nSUMMARY:Clinic\r\n"+
		"DTSTART;TZID=Tokyo Standard Time:20260601T100000\r\nCLASS:PRIVATE\r\nTRANSP:TRANSPARENT\r\n"+
		"BEGIN:VALARM\r\nTRIGGER:-PT20M\r\nEND:VALARM\r\nEND:VEVENT\r\n"), ical.ParseOptions{})
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	got := importEvent(events[0])
	if got.SourceUID != "u" || got.Title != "Clinic" || got.Timezone != "Asia/Tokyo" ||
		got.Visibility != generated.CalendarEventsVisibilityPrivate || got.ShowAs != generated.CalendarEventsShowAsFree ||
		!got.NotificationOffset.Valid || got.NotificationOffset.Int32 != 20 {
		t.Errorf("importEvent = %+v", got)
	}

	if got := importEvent(ical.ParsedEvent{Transp: "OPAQUE", Status: "TENTATIVE"}); got.ShowAs != generated.CalendarEventsShowAsTentative {
		t.Errorf("STATUS:TENTATIVE imports as tentative, got %q", got.ShowAs)
	}
	if got := importEvent(ical.ParsedEvent{Transp: "TRANSPARENT", Status: "TENTATIVE"}); got.ShowAs != generated.CalendarEventsShowAsFree {
		t.Errorf("TRANSPARENT wins over STATUS, got %q", got.ShowAs)
	}

	floating := importEvent(ical.ParsedEvent{Class: "X-OTHER"})
	if floating.Timezone != "" || floating.Visibility != generated.CalendarEventsVisibilityDefault ||
		floating.ShowAs != generated.CalendarEventsShowAsBusy || floating.NotificationOffset.Valid {
		t.Errorf("a bare event takes the defaults, got %+v", floating)
	}
}
