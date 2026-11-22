package ical

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// exceptionsOf builds a stored exclusion list from its elements.
func exceptionsOf(elements ...string) recurrence.ExceptionList {
	var list recurrence.ExceptionList
	for _, el := range elements {
		body, _ := json.Marshal(el)
		list = append(list, body)
	}
	return list
}

var testPublicID = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func render(series ...Series) string {
	return string(Render(series, RenderOptions{Now: time.Now(), AppName: "Nodate Time", CalendarName: "Cal"}))
}

// single is a non-recurring series holding e.
func single(e Event) Series {
	return Series{PublicID: testPublicID, AllDay: e.AllDay, Timezone: e.Timezone, Master: &e}
}

// An all-day event is dates: its UTC bounds are its dates, whatever zone it
// records.
func TestRenderAllDayWritesTheUTCDates(t *testing.T) {
	start := time.Date(2025, 6, 24, 0, 0, 0, 0, time.UTC)
	out := render(single(Event{Title: "Concert", AllDay: true, Timezone: "America/Los_Angeles",
		Start: start, End: start.AddDate(0, 0, 1)}))
	for _, want := range []string{"DTSTART;VALUE=DATE:20250624", "DTEND;VALUE=DATE:20250625"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "20250623") || strings.Contains(out, "TZID") {
		t.Errorf("an all-day event must not shift to the zone's day or name a zone:\n%s", out)
	}
}

// A timed event leaves in its own zone, defined by a VTIMEZONE in the same
// file; one in UTC stays a bare instant.
func TestRenderTimedEventCarriesItsZone(t *testing.T) {
	start := time.Date(2025, 6, 24, 1, 0, 0, 0, time.UTC) // 10:00 JST
	ev := Event{Title: "Meeting", Timezone: "Asia/Tokyo", Start: start, End: start.Add(time.Hour)}
	out := render(single(ev))
	for _, want := range []string{
		"DTSTART;TZID=Asia/Tokyo:20250624T100000",
		"DTEND;TZID=Asia/Tokyo:20250624T110000",
		"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	ev.Timezone = "UTC"
	out = render(single(ev))
	if !strings.Contains(out, "DTSTART:20250624T010000Z") {
		t.Errorf("a UTC event should emit a UTC instant, got:\n%s", out)
	}
	if strings.Contains(out, "TZID") {
		t.Errorf("a UTC event needs no zone definition, got:\n%s", out)
	}
}

// TEXT values are escaped; URI values are not.
func TestRenderEscaping(t *testing.T) {
	now := time.Now().UTC()
	out := render(single(Event{
		Title: "A, B; C", Description: "line1\r\nline2", URL: "https://example.com/a,b;c",
		Timezone: "UTC", Start: now, End: now,
	}))
	if !strings.Contains(out, `SUMMARY:A\, B\; C`) {
		t.Errorf("SUMMARY commas/semicolons must be TEXT-escaped:\n%s", out)
	}
	if !strings.Contains(out, `DESCRIPTION:line1\nline2`) {
		t.Errorf("DESCRIPTION CRLF must be normalized to \\n:\n%s", out)
	}
	if !strings.Contains(out, "URL:https://example.com/a,b;c") {
		t.Errorf("URL value must NOT be TEXT-escaped:\n%s", out)
	}
}

// A recurring series leaves as one VEVENT carrying its rule, not one per
// occurrence a client could no longer edit as a series.
func TestRenderWritesASeriesAsOneVEventWithItsRule(t *testing.T) {
	base := time.Date(2025, 6, 24, 9, 0, 0, 0, time.UTC)
	s := single(Event{Title: "Standup", Timezone: "UTC", Start: base, End: base.Add(time.Hour)})
	s.Rule = &recurrence.Rule{Freq: "daily", Interval: 1, Count: 30}
	s.Exdates = exceptionsOf(base.AddDate(0, 0, 2).Format(time.RFC3339))
	out := render(s)
	if n := strings.Count(out, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("expected 1 VEVENT for the series, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RRULE:FREQ=DAILY;COUNT=30;WKST=MO") {
		t.Errorf("the rule itself must be written out:\n%s", out)
	}
	if !strings.Contains(out, "EXDATE:20250626T090000Z") {
		t.Errorf("a cancelled occurrence must be written as EXDATE:\n%s", out)
	}
}

// A changed occurrence shares the series' UID and is told apart by its
// RECURRENCE-ID; a UID of its own would show it next to the occurrence it
// replaces.
func TestRenderTiesAChangedOccurrenceToItsSeries(t *testing.T) {
	base := time.Date(2025, 6, 24, 9, 0, 0, 0, time.UTC)
	s := single(Event{Title: "Standup", Timezone: "UTC", Start: base, End: base.Add(time.Hour)})
	s.Rule = &recurrence.Rule{Freq: "daily", Interval: 1}
	s.Overrides = []Event{{
		Title: "Standup (moved)", Timezone: "UTC",
		Start: base.AddDate(0, 0, 1).Add(4 * time.Hour), End: base.AddDate(0, 0, 1).Add(5 * time.Hour),
		OriginalStart: base.AddDate(0, 0, 1),
	}}
	out := render(s)
	if n := strings.Count(out, "UID:0102030405060708090a0b0c0d0e0f10@nodate-time"); n != 2 {
		t.Errorf("both must carry the series UID, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RECURRENCE-ID:20250625T090000Z") {
		t.Errorf("the changed occurrence must name the one it replaces:\n%s", out)
	}
}

// A Monday 08:00 Tokyo series is Sunday 23:00 in UTC. Written as a UTC
// DTSTART, BYDAY=MO expands to Mondays in UTC -- Tuesdays in Tokyo.
func TestRenderWritesASeriesInItsOwnZone(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(2026, 1, 5, 8, 0, 0, 0, tokyo) // Monday
	s := single(Event{Title: "Standup", Timezone: "Asia/Tokyo", Start: start.UTC(), End: start.Add(time.Hour).UTC()})
	s.Rule = &recurrence.Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}}
	s.Exdates = exceptionsOf(start.AddDate(0, 0, 7).UTC().Format(time.RFC3339))
	out := render(s)
	for _, want := range []string{
		"DTSTART;TZID=Asia/Tokyo:20260105T080000",
		"DTEND;TZID=Asia/Tokyo:20260105T090000",
		"EXDATE;TZID=Asia/Tokyo:20260112T080000",
		"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n",
		"TZOFFSETTO:+0900",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DTSTART:20260104T230000Z") {
		t.Errorf("the series must not start on the UTC Sunday:\n%s", out)
	}
}

// A New York series keeps its wall-clock time across DST only when the rule
// expands in New York, which needs the zone's rules in the file.
func TestRenderDefinesTheZoneAcrossDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, ny)
	s := single(Event{Title: "Sync", Timezone: "America/New_York", Start: start.UTC(), End: start.Add(time.Hour).UTC()})
	s.Rule = &recurrence.Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}}
	out := render(s)
	for _, want := range []string{
		"DTSTART;TZID=America/New_York:20260302T090000",
		"TZID:America/New_York",
		"TZOFFSETFROM:-0500\r\nTZOFFSETTO:-0400\r\nTZNAME:EDT\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU",
		"TZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nTZNAME:EST\r\nRRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// A rule counted from the end of the month is the one Europe uses.
func TestVTimezoneRecoversLastSundayRules(t *testing.T) {
	var b strings.Builder
	writeVTimezone(&b, "Europe/London", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	out := b.String()
	for _, want := range []string{
		"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
		"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// RECURRENCE-ID takes the series' value type, whatever the override became.
func TestRecurrenceIDFollowsTheSeries(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	original := time.Date(2026, 1, 12, 8, 0, 0, 0, tokyo)
	out := render(Series{PublicID: testPublicID, Timezone: "Asia/Tokyo", Overrides: []Event{{
		Title: "Off", AllDay: true, Timezone: "Asia/Tokyo",
		Start:         time.Date(2026, 1, 12, 0, 0, 0, 0, tokyo).UTC(),
		End:           time.Date(2026, 1, 13, 0, 0, 0, 0, tokyo).UTC(),
		OriginalStart: original.UTC(),
	}}})
	if !strings.Contains(out, "RECURRENCE-ID;TZID=Asia/Tokyo:20260112T080000") {
		t.Errorf("an all-day override of a timed series must name a timed occurrence:\n%s", out)
	}
	if strings.Contains(out, "RECURRENCE-ID;VALUE=DATE") {
		t.Errorf("RECURRENCE-ID must not follow the override's own all-day flag:\n%s", out)
	}

	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	out = render(Series{PublicID: testPublicID, AllDay: true, Timezone: "Asia/Tokyo", Overrides: []Event{{
		Title: "Moved", Timezone: "Asia/Tokyo",
		Start: day.Add(10 * time.Hour).UTC(), End: day.Add(11 * time.Hour).UTC(), OriginalStart: day,
	}}})
	if !strings.Contains(out, "RECURRENCE-ID;VALUE=DATE:20260112") {
		t.Errorf("a timed override of an all-day series must name a date:\n%s", out)
	}
}

// A reader who sees an event with its details withheld gets the title and the
// times, and nothing that says where, what about, or the link.
func TestRenderMaskedEventLeavesOnlyTitleAndTimes(t *testing.T) {
	start := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	out := render(single(Event{
		Title: "Planning", Timezone: "UTC", Location: "Room 4", Description: "agenda",
		URL: "https://example.test/x", Start: start, End: start.Add(time.Hour), Masked: true,
	}))
	if !strings.Contains(out, "SUMMARY:Planning") {
		t.Errorf("the title stays:\n%s", out)
	}
	for _, leaked := range []string{"LOCATION", "DESCRIPTION:agenda", "URL:", "Room 4"} {
		if strings.Contains(out, leaked) {
			t.Errorf("masked export leaks %q:\n%s", leaked, out)
		}
	}
}

// Every element of a series' exclusion list leaves as the occurrence it names,
// in the series' own value type; what cannot be read stays out of the file.
func TestExdateWritesEveryReadableForm(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(2026, 1, 5, 8, 0, 0, 0, tokyo)
	timed := exceptionsOf("2026-01-12", "2026-01-18T23:00:00Z", "2026-01-26T08:00:00", "not a date")
	want := "EXDATE;TZID=Asia/Tokyo:20260112T080000,20260119T080000,20260126T080000"
	if got := exdateProp(timed, false, "Asia/Tokyo", start.UTC()); got != want {
		t.Errorf("timed series: got %q, want %q", got, want)
	}

	allDay := exceptionsOf("2026-01-12", "2026-01-19T00:00:00Z", "2026-01-26T00:00:00", "garbage")
	want = "EXDATE;VALUE=DATE:20260112,20260119,20260126"
	if got := exdateProp(allDay, true, "America/Los_Angeles", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)); got != want {
		t.Errorf("all-day series: got %q, want %q", got, want)
	}
	if got := exdateProp(exceptionsOf("garbage"), true, "UTC", start); got != "" {
		t.Errorf("an unreadable element writes nothing, got %q", got)
	}
}

// A reminder left in the database is one nobody receives: only the client
// holding the calendar can raise it.
func TestReminderIsWrittenIntoTheFile(t *testing.T) {
	out := render(single(Event{Title: "Clinic", Timezone: "Asia/Tokyo", AlarmMinutes: minutes(30)}))
	for _, want := range []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Clinic", "TRIGGER:-PT30M", "END:VALARM"} {
		if !strings.Contains(out, want) {
			t.Errorf("export should carry %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "BEGIN:VALARM") > strings.Index(out, "END:VEVENT") {
		t.Error("the alarm belongs inside the event it reminds about")
	}
}

func TestNoReminderWritesNoAlarm(t *testing.T) {
	if out := render(single(Event{Title: "Lunch", Timezone: "UTC"})); strings.Contains(out, "VALARM") {
		t.Errorf("an event with no reminder should carry no alarm:\n%s", out)
	}
}

func TestTriggerRendering(t *testing.T) {
	cases := map[int32]string{0: "PT0S", 5: "-PT5M", 60: "-PT60M", 1440: "-PT1440M"}
	for m, want := range cases {
		if got := trigger(m); got != want {
			t.Errorf("trigger(%d) = %q, want %q", m, got, want)
		}
	}
}

// The options name a series and stamp it; without them the default UID and
// Now apply.
func TestRenderOptionsNameAndStampASeries(t *testing.T) {
	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := single(Event{Title: "x", Timezone: "UTC", Start: start, End: start.Add(time.Hour), UpdatedAt: start.Add(-time.Hour)})
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	out := string(Render([]Series{s}, RenderOptions{Now: now}))
	if !strings.Contains(out, "UID:"+DefaultUID(testPublicID)+"\r\n") || !strings.Contains(out, "DTSTAMP:20261122T000000Z") {
		t.Errorf("defaults not applied:\n%s", out)
	}
	out = string(Render([]Series{s}, RenderOptions{
		Now:   now,
		Stamp: func(s Series) time.Time { return s.Master.UpdatedAt },
		UID:   func(Series) string { return "client-uid" },
	}))
	if !strings.Contains(out, "UID:client-uid\r\n") || !strings.Contains(out, "DTSTAMP:20260501T080000Z") {
		t.Errorf("options not applied:\n%s", out)
	}
}

// The content hash leaves out what changes without the series changing: the
// clock, which moves DTSTAMP and the VTIMEZONE scan, and the stamp.
func TestContentHashIgnoresTheClock(t *testing.T) {
	for _, s := range goldenSeries() {
		a := ContentHash(s, RenderOptions{Now: goldenNow})
		b := ContentHash(s, RenderOptions{Now: goldenNow.AddDate(7, 3, 1)})
		c := ContentHash(s, RenderOptions{Now: goldenNow, Stamp: func(Series) time.Time { return time.Unix(0, 0) }})
		if a != b || a != c || len(a) != 32 {
			t.Errorf("series %x: hash %q / %q / %q must be one 32-digit value", s.PublicID, a, b, c)
		}
	}
}

// The content hash moves with anything Render writes for the series.
func TestContentHashFollowsTheContent(t *testing.T) {
	base := goldenSeries()[3]
	want := ContentHash(base, RenderOptions{})
	changes := map[string]func(*Series){
		"title":    func(s *Series) { s.Master.Title += "!" },
		"rule":     func(s *Series) { s.Rule = &recurrence.Rule{Freq: "daily", Interval: 1} },
		"exdate":   func(s *Series) { s.Exdates = s.Exdates[:1] },
		"override": func(s *Series) { s.Overrides = s.Overrides[:1] },
		"alarm":    func(s *Series) { s.Master.AlarmMinutes = nil },
		"class":    func(s *Series) { s.Master.Class = "PRIVATE" },
		"masked":   func(s *Series) { s.Overrides[0].Masked = true },
	}
	for name, change := range changes {
		s := goldenSeries()[3]
		change(&s)
		if ContentHash(s, RenderOptions{}) == want {
			t.Errorf("%s: hash did not change", name)
		}
	}
	if got := ContentHash(base, RenderOptions{UID: func(Series) string { return "other" }}); got == want {
		t.Error("the UID is part of the content")
	}
}
