package calendars

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
)

func icsBody(lines ...string) string {
	return strings.Join(append(append([]string{"BEGIN:VCALENDAR"}, lines...), "END:VCALENDAR"), "\r\n")
}

// A quoted parameter value may hold ':' and ';'. Splitting at the first colon
// would cut the property inside the quotes.
func TestParseICSSplitsOutsideQuotes(t *testing.T) {
	events := parseICS(icsBody(
		"BEGIN:VEVENT",
		"SUMMARY:Clinic",
		`DTSTART;X-LABEL="a:b;c";TZID="Asia/Tokyo":20260601T100000`,
		`DESCRIPTION;ALTREP="cid:part1.0001@example.org":The details`,
		"BEGIN:VALARM",
		`TRIGGER;X-NOTE="x:y":-PT15M`,
		"END:VALARM",
		"END:VEVENT",
	))
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	e := events[0]
	if want := time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC); !e.start.Equal(want) || e.tzid != "Asia/Tokyo" {
		t.Errorf("start = %s tz %q, want %s in Asia/Tokyo", e.start.UTC(), e.tzid, want)
	}
	if e.desc != "The details" {
		t.Errorf("desc = %q", e.desc)
	}
	if e.alarmMinutes == nil || *e.alarmMinutes != 15 {
		t.Errorf("alarm = %v, want 15", e.alarmMinutes)
	}
}

// Component names are case-insensitive like every other iCalendar name.
func TestParseICSComponentNamesIgnoreCase(t *testing.T) {
	body := "begin:vcalendar\r\nbegin:vevent\r\nSUMMARY:Lower\r\nDTSTART:20260601T100000Z\r\n" +
		"Begin:Valarm\r\nDESCRIPTION:not the memo\r\nend:valarm\r\nend:vevent\r\nend:vcalendar"
	events := parseICS(body)
	if len(events) != 1 || events[0].summary != "Lower" || events[0].desc != "" {
		t.Fatalf("events = %+v", events)
	}
	if !containsICSComponent(body) {
		t.Error("a lower-case calendar must not read as unreadable")
	}
	lower := parseICS(strings.ToLower(body))
	if len(lower) != 1 || !lower[0].start.Equal(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("a fully lower-cased file must still date its event: %+v", lower)
	}
}

// EXDATE values are deduplicated as they are read and capped, so the cost of
// a file is linear in its size and the stored list stays bounded.
func TestParseICSBoundsExdates(t *testing.T) {
	var dup []string
	for range 5000 {
		dup = append(dup, "20260601T100000Z")
	}
	e := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260601T100000Z",
		"EXDATE:"+strings.Join(dup, ","), "EXDATE:20260601T100000Z", "END:VEVENT"))[0]
	if len(e.exdates) != 1 || e.tooManyExdates {
		t.Errorf("repeated values: %d kept, overflow %v; want 1, false", len(e.exdates), e.tooManyExdates)
	}

	var many []string
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range calimport.MaxExdates + 1 {
		many = append(many, base.Add(time.Duration(i)*time.Hour).Format("20060102T150405Z"))
	}
	e = parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"EXDATE:"+strings.Join(many, ","), "END:VEVENT"))[0]
	if !e.tooManyExdates || len(e.exdates) != calimport.MaxExdates {
		t.Errorf("over the cap: %d kept, overflow %v", len(e.exdates), e.tooManyExdates)
	}
}

// A description past the memo limit is cut to it on a character boundary.
func TestParseICSCutsTheDescriptionToTheMemoLimit(t *testing.T) {
	long := "B" + strings.Repeat("あ", calimport.MaxMemoRunes+50)
	e := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+long, "END:VEVENT"))[0]
	if !utf8.ValidString(e.desc) || utf8.RuneCountInString(e.desc) != calimport.MaxMemoRunes ||
		!strings.HasPrefix(long, e.desc) {
		t.Errorf("description kept %d runes (valid %v), want the first %d",
			utf8.RuneCountInString(e.desc), utf8.ValidString(e.desc), calimport.MaxMemoRunes)
	}
	short := strings.Repeat("あ", calimport.MaxMemoRunes)
	e = parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+short, "END:VEVENT"))[0]
	if e.desc != short {
		t.Error("a description within the limit must arrive whole")
	}
}

// A date is a day, not an instant: the zone a file names for it does not move
// it off UTC midnight.
func TestAllDayDatesParseAsUTCMidnight(t *testing.T) {
	ics := strings.Join([]string{
		"BEGIN:VCALENDAR", "BEGIN:VEVENT", "UID:a", "SUMMARY:Holiday",
		"DTSTART;VALUE=DATE;TZID=America/Los_Angeles:20260704",
		"DTEND;VALUE=DATE;TZID=America/Los_Angeles:20260705",
		"EXDATE;VALUE=DATE;TZID=America/Los_Angeles:20260711",
		"END:VEVENT", "END:VCALENDAR"}, "\r\n")
	got := parseICS(ics)
	if len(got) != 1 {
		t.Fatalf("want one event, got %d", len(got))
	}
	want := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if !got[0].start.Equal(want) || !got[0].end.Equal(want.AddDate(0, 0, 1)) {
		t.Errorf("bounds = %s..%s, want UTC dates", got[0].start, got[0].end)
	}
	if len(got[0].exdates) != 1 || !got[0].exdates[0].Equal(want.AddDate(0, 0, 7)) {
		t.Errorf("exdates = %v", got[0].exdates)
	}
}
