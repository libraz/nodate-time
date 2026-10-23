package calendars

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestImportEndAtUsesDuration(t *testing.T) {
	timed := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260601T100000Z", "DURATION:PT3H", "END:VEVENT"))[0]
	if end, ok := importEndAt(timed); !ok || !end.Equal(time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("timed end = %s %v, want 13:00Z", end, ok)
	}
	allDay := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART;VALUE=DATE:20260601", "DURATION:P3D", "END:VEVENT"))[0]
	if end, ok := importEndAt(allDay); !ok || end.Format("20060102") != "20260604" {
		t.Errorf("all-day end = %s %v, want 20260604", end, ok)
	}
	for _, bad := range []string{"DURATION:PT", "DURATION:-PT1H", "DURATION:junk"} {
		e := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260601T100000Z", bad, "END:VEVENT"))[0]
		if _, ok := importEndAt(e); ok {
			t.Errorf("%s must be refused, not replaced by a default length", bad)
		}
	}
	partial := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART;VALUE=DATE:20260601", "DURATION:PT12H", "END:VEVENT"))[0]
	if _, ok := importEndAt(partial); ok {
		t.Error("a part-day duration on an all-day event must be refused")
	}
	none := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260601T100000Z", "END:VEVENT"))[0]
	if end, ok := importEndAt(none); !ok || !end.Equal(time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("default end = %s %v", end, ok)
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
	for i := range importMaxExdates + 1 {
		many = append(many, base.Add(time.Duration(i)*time.Hour).Format("20060102T150405Z"))
	}
	e = parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"EXDATE:"+strings.Join(many, ","), "END:VEVENT"))[0]
	if !e.tooManyExdates || len(e.exdates) != importMaxExdates {
		t.Errorf("over the cap: %d kept, overflow %v", len(e.exdates), e.tooManyExdates)
	}
}

// An RRULE without WKST means a Monday week start. The expander only does
// Sunday, so a rule where that changes the dates is refused.
func TestConvertRRuleHonorsTheDefaultWeekStart(t *testing.T) {
	saturday := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, 1, 11, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		rule  string
		start time.Time
		ok    bool
	}{
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", saturday, false},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU;WKST=MO", saturday, false},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU;WKST=SU", saturday, true},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE", monday, true},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO", sunday, false},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SU", sunday, true},
		{"FREQ=WEEKLY;BYDAY=SA,SU", saturday, true},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SA;WKST=TU", monday, false},
		// The rule contract the API holds rules to applies to imports too.
		{"FREQ=DAILY;BYDAY=MO", monday, false},
		{"FREQ=WEEKLY;BYMONTHDAY=5", monday, false},
		{"FREQ=MONTHLY;BYDAY=MO", monday, false},
		{"FREQ=MONTHLY;BYDAY=MO;BYSETPOS=2", monday, true},
		{"FREQ=DAILY;UNTIL=20260101T000000Z", monday, false},
	}
	for _, c := range cases {
		_, ok := convertRRule(c.rule, c.start)
		if ok != c.ok {
			t.Errorf("convertRRule(%q, %s) ok = %v, want %v", c.rule, c.start.Weekday(), ok, c.ok)
		}
	}
}

// A description past the memo limit is cut to it on a character boundary.
func TestParseICSCutsTheDescriptionToTheMemoLimit(t *testing.T) {
	long := "B" + strings.Repeat("あ", importMaxMemoRunes+50)
	e := parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+long, "END:VEVENT"))[0]
	if !utf8.ValidString(e.desc) || utf8.RuneCountInString(e.desc) != importMaxMemoRunes ||
		!strings.HasPrefix(long, e.desc) {
		t.Errorf("description kept %d runes (valid %v), want the first %d",
			utf8.RuneCountInString(e.desc), utf8.ValidString(e.desc), importMaxMemoRunes)
	}
	short := strings.Repeat("あ", importMaxMemoRunes)
	e = parseICS(icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+short, "END:VEVENT"))[0]
	if e.desc != short {
		t.Error("a description within the limit must arrive whole")
	}
}
