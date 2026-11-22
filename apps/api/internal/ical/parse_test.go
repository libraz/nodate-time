package ical

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func icsBody(lines ...string) []byte {
	return []byte(strings.Join(append(append([]string{"BEGIN:VCALENDAR"}, lines...), "END:VCALENDAR"), "\r\n"))
}

func parse(t *testing.T, body []byte) []ParsedEvent {
	t.Helper()
	events, err := Parse(body, ParseOptions{})
	if err != nil {
		t.Fatalf("lenient parse failed: %v", err)
	}
	return events
}

// TestControlCharactersAreDroppedFromText verifies what a value may contain:
// RFC 5545 allows no C0 character but HTAB inside one.
func TestControlCharactersAreDroppedFromText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"before\x00after", "beforeafter"},
		{"\x1b[31mred\x07", "[31mred"},
		{"kept\ttab", "kept\ttab"},
		{"kept\nbreak", "kept\nbreak"},
		{"del\x7fete", "delete"},
		{"nothing to do", "nothing to do"},
	}
	for _, c := range cases {
		if got := DropForbiddenControl(c.in); got != c.want {
			t.Errorf("DropForbiddenControl(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The rows in the database were not all written by the import, so the
// renderer cannot assume its input is clean.
func TestRenderCannotWriteAControlCharacter(t *testing.T) {
	if got := escapeText("clear\x1b[2Jscreen"); got != "clear[2Jscreen" {
		t.Errorf("escapeText dropped nothing: %q", got)
	}
	// A real line break is content, and escaping keeps it inside the value.
	if got := escapeText("one\r\ntwo"); got != `one\ntwo` {
		t.Errorf("escapeText(%q) = %q", "one\r\ntwo", got)
	}
	if got := escapeURI("https://example.com/\x1b[0m"); got != "https://example.com/[0m" {
		t.Errorf("escapeURI dropped nothing: %q", got)
	}
}

// The grammar tells a duration from a typo; adding up whatever units a
// malformed value carries answers a file that asked for no reminder with one.
func TestDurationGrammarIsFollowedRatherThanApproximated(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"P1W", 7 * 24 * time.Hour, true},
		{"P1DT12H", 36 * time.Hour, true},
		{"PT1H30M", 90 * time.Minute, true},
		{"PT0S", 0, true},
		{"P", 0, false},
		{"PT", 0, false},
		{"PT1M1M", 0, false},
		{"PT1S30M", 0, false},
		{"P1DT1H2D", 0, false},
		{"P1W2D", 0, false},
		{"PT1D", 0, false},
		{"P1H", 0, false},
		{"P2T1H", 0, false},
		{"P106751991167301D", 0, false},
		{"nonsense", 0, false},
	}
	for _, c := range cases {
		got, ok := parseDuration(c.value)
		if ok != c.ok {
			t.Errorf("parseDuration(%q) ok = %v, want %v", c.value, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

// TestTriggerParsing covers the forms other calendars write.
func TestTriggerParsing(t *testing.T) {
	cases := []struct {
		value   string
		params  []string
		minutes int32
		usable  bool
		valid   bool
	}{
		{value: "-PT15M", minutes: 15, usable: true, valid: true},
		{value: "-PT1H", minutes: 60, usable: true, valid: true},
		{value: "-P1D", minutes: 1440, usable: true, valid: true},
		{value: "-P1DT12H", minutes: 2160, usable: true, valid: true},
		{value: "-P1W", minutes: 10080, usable: true, valid: true},
		{value: "PT0S", minutes: 0, usable: true, valid: true},
		{value: "-PT0S", minutes: 0, usable: true, valid: true},
		// After the start: a trigger, but not one the stored offset holds.
		{value: "PT10M", valid: true},
		// Sub-minute precision would have to be rounded.
		{value: "-PT90S", valid: true},
		// An instant does not move when the event does.
		{value: "20250624T090000Z", params: []string{"VALUE=DATE-TIME"}, valid: true},
		{value: "-PT15M", params: []string{"VALUE=DATE-TIME"}},
		// Relative to the end, which the stored offset cannot express.
		{value: "-PT15M", params: []string{"RELATED=END"}, valid: true},
		{value: "soon", params: []string{"RELATED=END"}},
		{value: "nonsense"},
		{value: ""},
	}
	for _, c := range cases {
		got, usable, valid := parseTrigger(c.value, c.params)
		if usable != c.usable || valid != c.valid {
			t.Errorf("parseTrigger(%q, %v) usable, valid = %v, %v; want %v, %v",
				c.value, c.params, usable, valid, c.usable, c.valid)
			continue
		}
		if usable && got != c.minutes {
			t.Errorf("parseTrigger(%q) = %d, want %d", c.value, got, c.minutes)
		}
	}
}

// A VALARM repeats property names the event uses, and read flat its
// DESCRIPTION would replace the event's.
func TestAlarmDescriptionStaysOutOfTheMemo(t *testing.T) {
	events := parse(t, []byte(strings.Join([]string{
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
	}, "\r\n")))
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Description != "Bring the referral letter" {
		t.Errorf("the event's own description should survive its alarm, got %q", events[0].Description)
	}
	if m, ok := FirstExpressibleAlarm(events[0].Alarms); !ok || m != 30 {
		t.Errorf("the alarm should be read, got %d %v", m, ok)
	}
	if events[0].Alarms[0].Action != "DISPLAY" {
		t.Errorf("action = %q", events[0].Alarms[0].Action)
	}
}

// An event carrying several alarms keeps one: the first that reads as an
// offset.
func TestFirstUsableAlarmWins(t *testing.T) {
	events := parse(t, []byte(strings.Join([]string{
		"BEGIN:VEVENT",
		"SUMMARY:Clinic",
		"DTSTART:20250624T010000Z",
		"BEGIN:VALARM",
		"ACTION:EMAIL",
		"TRIGGER;RELATED=END:-PT5M",
		"END:VALARM",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"TRIGGER:-PT10M",
		"END:VALARM",
		"BEGIN:VALARM",
		"ACTION:EMAIL",
		"TRIGGER:-P1D",
		"END:VALARM",
		"END:VEVENT",
	}, "\r\n")))
	if len(events) != 1 || len(events[0].Alarms) != 3 {
		t.Fatalf("expected one event with three alarms, got %+v", events)
	}
	if m, ok := FirstExpressibleAlarm(events[0].Alarms); !ok || m != 10 {
		t.Errorf("the first usable alarm should win, got %d %v", m, ok)
	}
}

// A reminder is kept whenever the calendar can store it. One longer than that
// is dropped, and a later alarm does not stand in for it.
func TestFirstExpressibleAlarmKeepsWhatTheCalendarStores(t *testing.T) {
	for _, m := range []int32{0, 15, 45, MaxAlarmMinutes} {
		if got, ok := FirstExpressibleAlarm([]Alarm{{Minutes: m, HasOffset: true}}); !ok || got != m {
			t.Errorf("a reminder of %d minutes should be kept, got %d %v", m, got, ok)
		}
	}
	long := []Alarm{{Minutes: MaxAlarmMinutes + 1, HasOffset: true}, {Minutes: 10, HasOffset: true}}
	if _, ok := FirstExpressibleAlarm(long); ok {
		t.Error("a reminder longer than the calendar stores should be dropped")
	}
	if _, ok := FirstExpressibleAlarm([]Alarm{{Action: "DISPLAY"}}); ok {
		t.Error("an alarm without a usable trigger is no reminder")
	}
	if _, ok := FirstExpressibleAlarm(nil); ok {
		t.Error("no alarm should stay no reminder")
	}
}

// A file separated by bare CR is one long line, and a byte order mark against
// a component's first line keeps it from matching; both still yield events.
func TestFilesWithUnusualLineEndingsStillYieldTheirEvents(t *testing.T) {
	body := strings.Join([]string{
		"BEGIN:VCALENDAR", "BEGIN:VEVENT", "SUMMARY:Clinic", "DTSTART:20260601T100000Z", "END:VEVENT", "END:VCALENDAR",
	}, "\r")
	if events := parse(t, []byte(body)); len(events) != 1 || events[0].Summary != "Clinic" {
		t.Errorf("a file written with bare CR still describes one event, got %+v", events)
	}

	marked := "\ufeffBEGIN:VCALENDAR\r\n\ufeffBEGIN:VEVENT\r\nSUMMARY:Clinic\r\n" +
		"DTSTART:20260601T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if events := parse(t, []byte(marked)); len(events) != 1 || events[0].Summary != "Clinic" {
		t.Errorf("a stray byte order mark must not hide the event, got %+v", events)
	}
}

// A quoted parameter value may hold ':' and ';'.
func TestParseSplitsOutsideQuotes(t *testing.T) {
	events := parse(t, icsBody(
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
	if want := time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC); !e.Start.Equal(want) || e.TZID != "Asia/Tokyo" {
		t.Errorf("start = %s tz %q, want %s in Asia/Tokyo", e.Start.UTC(), e.TZID, want)
	}
	if e.Description != "The details" {
		t.Errorf("desc = %q", e.Description)
	}
	if m, ok := FirstExpressibleAlarm(e.Alarms); !ok || m != 15 {
		t.Errorf("alarm = %d %v, want 15", m, ok)
	}
}

// Component names are case-insensitive like every other iCalendar name.
func TestParseComponentNamesIgnoreCase(t *testing.T) {
	body := "begin:vcalendar\r\nbegin:vevent\r\nSUMMARY:Lower\r\nDTSTART:20260601T100000Z\r\n" +
		"Begin:Valarm\r\nDESCRIPTION:not the memo\r\nend:valarm\r\nend:vevent\r\nend:vcalendar"
	events := parse(t, []byte(body))
	if len(events) != 1 || events[0].Summary != "Lower" || events[0].Description != "" {
		t.Fatalf("events = %+v", events)
	}
	if !ContainsComponent([]byte(body)) {
		t.Error("a lower-case calendar must not read as unreadable")
	}
	lower := parse(t, []byte(strings.ToLower(body)))
	if len(lower) != 1 || !lower[0].Start.Equal(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("a fully lower-cased file must still date its event: %+v", lower)
	}
}

// EXDATE values are deduplicated as they are read and capped.
func TestParseBoundsExdates(t *testing.T) {
	var dup []string
	for range 5000 {
		dup = append(dup, "20260601T100000Z")
	}
	e := parse(t, icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260601T100000Z",
		"EXDATE:"+strings.Join(dup, ","), "EXDATE:20260601T100000Z", "END:VEVENT"))[0]
	if len(e.Exdates) != 1 || e.TooManyExdates {
		t.Errorf("repeated values: %d kept, overflow %v; want 1, false", len(e.Exdates), e.TooManyExdates)
	}

	var many []string
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range MaxExdates + 1 {
		many = append(many, base.Add(time.Duration(i)*time.Hour).Format("20060102T150405Z"))
	}
	e = parse(t, icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"EXDATE:"+strings.Join(many, ","), "END:VEVENT"))[0]
	if !e.TooManyExdates || len(e.Exdates) != MaxExdates {
		t.Errorf("over the cap: %d kept, overflow %v", len(e.Exdates), e.TooManyExdates)
	}
}

// A description past the memo limit is cut to it on a character boundary.
func TestParseCutsTheDescriptionToTheMemoLimit(t *testing.T) {
	long := "B" + strings.Repeat("あ", MaxDescriptionRunes+50)
	e := parse(t, icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+long, "END:VEVENT"))[0]
	if !utf8.ValidString(e.Description) || utf8.RuneCountInString(e.Description) != MaxDescriptionRunes ||
		!strings.HasPrefix(long, e.Description) {
		t.Errorf("description kept %d runes (valid %v), want the first %d",
			utf8.RuneCountInString(e.Description), utf8.ValidString(e.Description), MaxDescriptionRunes)
	}
	short := strings.Repeat("あ", MaxDescriptionRunes)
	e = parse(t, icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z",
		"DESCRIPTION:"+short, "END:VEVENT"))[0]
	if e.Description != short {
		t.Error("a description within the limit must arrive whole")
	}
}

// A date is a day, not an instant: the zone a file names for it does not move
// it off UTC midnight.
func TestAllDayDatesParseAsUTCMidnight(t *testing.T) {
	got := parse(t, icsBody("BEGIN:VEVENT", "UID:a", "SUMMARY:Holiday",
		"DTSTART;VALUE=DATE;TZID=America/Los_Angeles:20260704",
		"DTEND;VALUE=DATE;TZID=America/Los_Angeles:20260705",
		"EXDATE;VALUE=DATE;TZID=America/Los_Angeles:20260711",
		"END:VEVENT"))
	if len(got) != 1 {
		t.Fatalf("want one event, got %d", len(got))
	}
	want := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if !got[0].Start.Equal(want) || !got[0].End.Equal(want.AddDate(0, 0, 1)) {
		t.Errorf("bounds = %s..%s, want UTC dates", got[0].Start, got[0].End)
	}
	if len(got[0].Exdates) != 1 || !got[0].Exdates[0].Equal(want.AddDate(0, 0, 7)) {
		t.Errorf("exdates = %v", got[0].Exdates)
	}
}

// The zone an event is stored in: none when the file named none, the IANA
// name for a Windows one, UTC for one nothing resolves.
func TestParsedZone(t *testing.T) {
	cases := map[string]string{"": "", "Tokyo Standard Time": "Asia/Tokyo", "Asia/Tokyo": "Asia/Tokyo", "Mars/Olympus": "UTC"}
	for tzid, want := range cases {
		if got := (ParsedEvent{TZID: tzid}).Zone(); got != want {
			t.Errorf("Zone(%q) = %q, want %q", tzid, got, want)
		}
	}
	e := parse(t, icsBody("BEGIN:VEVENT", "SUMMARY:x", "DTSTART;TZID=Mars/Olympus:20260101T090000", "END:VEVENT"))[0]
	if !e.TZUnknown || !e.Start.Equal(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("an unknown zone reads as UTC and says so: %+v", e)
	}
}

// Strict refuses what the lenient parser drops: an unreadable value of a
// property it reads, a malformed line and an unterminated VEVENT.
func TestStrictRefusesWhatLenientDrops(t *testing.T) {
	cases := map[string][]string{
		"DTSTART":       {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:2026-01-01", "END:VEVENT"},
		"DTEND":         {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "DTEND;VALUE=DATE:20260101T000000", "END:VEVENT"},
		"RECURRENCE-ID": {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "RECURRENCE-ID:tomorrow", "END:VEVENT"},
		"EXDATE":        {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "EXDATE:20260102T000000Z,bad", "END:VEVENT"},
		"DURATION":      {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "DURATION:P1H", "END:VEVENT"},
		"TRIGGER":       {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "BEGIN:VALARM", "TRIGGER:soon", "END:VALARM", "END:VEVENT"},
		"absolute":      {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "BEGIN:VALARM", "TRIGGER;VALUE=DATE-TIME:soon", "END:VALARM", "END:VEVENT"},
		"line":          {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z", "LOCATION", "END:VEVENT"},
		"nested":        {"BEGIN:VEVENT", "SUMMARY:x", "BEGIN:VEVENT", "SUMMARY:y", "END:VEVENT"},
		"unterminated":  {"BEGIN:VEVENT", "SUMMARY:x", "DTSTART:20260101T000000Z"},
	}
	for name, lines := range cases {
		body := icsBody(lines...)
		if name == "unterminated" {
			body = []byte(strings.Join(lines, "\r\n"))
		}
		if _, err := Parse(body, ParseOptions{}); err != nil {
			t.Errorf("%s: lenient parse must not fail: %v", name, err)
		}
		if _, err := Parse(body, ParseOptions{Strict: true}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: strict parse = %v, want ErrInvalid", name, err)
		}
	}
}

// Strict reads the same as lenient wherever every value is readable, ignores
// properties it does not read and treats an alarm it cannot express as no
// error. It keeps a description whole so its length is checked, not cut.
func TestStrictAcceptsWhatItCanRead(t *testing.T) {
	long := strings.Repeat("a", MaxDescriptionRunes+1)
	body := icsBody(
		"BEGIN:VEVENT",
		"UID:u@example.com",
		"SEQUENCE:3",
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
		"GEO:not;a;geo",
		"ATTENDEE;CN=Someone:mailto:someone@example.com",
		"STATUS:CONFIRMED",
		"SUMMARY:Clinic",
		"DTSTART;TZID=Unknown/Zone:20260601T100000",
		"DURATION:PT1H",
		"CLASS:X-SECRET",
		"RRULE:FREQ=WEEKLY",
		"EXDATE:20260608T100000Z",
		"DESCRIPTION:"+long,
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"TRIGGER;RELATED=END:-PT15M",
		"END:VALARM",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"TRIGGER:PT9H",
		"END:VALARM",
		"",
		"END:VEVENT",
	)
	strict, err := Parse(body, ParseOptions{Strict: true})
	if err != nil {
		t.Fatalf("strict parse failed: %v", err)
	}
	lenient := parse(t, body)
	if len(strict) != 1 || len(lenient) != 1 {
		t.Fatalf("got %d strict / %d lenient events", len(strict), len(lenient))
	}
	s, l := strict[0], lenient[0]
	if s.Description != long || l.Description != long[:MaxDescriptionRunes] {
		t.Errorf("strict keeps %d runes, lenient %d", len(s.Description), len(l.Description))
	}
	s.Description, l.Description = "", ""
	if s.Summary != "Clinic" || !s.TZUnknown || s.Duration != time.Hour || s.Class != "X-SECRET" ||
		len(s.Exdates) != 1 || len(s.Alarms) != 2 {
		t.Errorf("strict event = %+v", s)
	}
	if _, ok := FirstExpressibleAlarm(s.Alarms); ok {
		t.Error("neither alarm is expressible")
	}
}
