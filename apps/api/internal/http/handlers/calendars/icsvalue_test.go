package calendars

import (
	"strings"
	"testing"
	"time"
)

// TestControlCharactersAreDroppedFromText verifies what a value may contain.
// RFC 5545 allows no C0 character but HTAB inside one, and what the forbidden
// ones do instead of showing is up to whatever renders them.
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
		if got := dropForbiddenControl(c.in); got != c.want {
			t.Errorf("dropForbiddenControl(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestExportCannotWriteAControlCharacter verifies the way out. The rows in the
// database were not all written by this import, so the export cannot assume its
// input is clean -- and a file whose title carries an escape sequence runs a
// command in the terminal of whoever reads it.
func TestExportCannotWriteAControlCharacter(t *testing.T) {
	if got := icsEscape("clear\x1b[2Jscreen"); got != "clear[2Jscreen" {
		t.Errorf("icsEscape dropped nothing: %q", got)
	}
	// A real line break is still content, and escaping is what keeps it inside
	// the value rather than turning it into another property.
	if got := icsEscape("one\r\ntwo"); got != `one\ntwo` {
		t.Errorf("icsEscape(%q) = %q", "one\r\ntwo", got)
	}
	if got := icsURI("https://example.com/\x1b[0m"); got != "https://example.com/[0m" {
		t.Errorf("icsURI dropped nothing: %q", got)
	}
}

// TestDurationGrammarIsFollowedRatherThanApproximated verifies the rules that
// tell a duration from a typo. Adding up whatever units a malformed value
// happens to carry answers a file that asked for no reminder with one, and a
// reminder at the wrong time is worse than one the import did not take.
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
		// Names no unit at all, so it is not a duration -- it used to read as
		// zero, which is a reminder at the moment the event starts.
		{"P", 0, false},
		{"PT", 0, false},
		// Each unit appears at most once, in order.
		{"PT1M1M", 0, false},
		{"PT1S30M", 0, false},
		{"P1DT1H2D", 0, false},
		// Weeks combine with nothing.
		{"P1W2D", 0, false},
		// A day is not part of the time half, and an hour is not part of the
		// date half.
		{"PT1D", 0, false},
		{"P1H", 0, false},
		{"P2T1H", 0, false},
		// More than a duration can hold: kept, it would wrap round to some
		// unrelated offset and read as an interval someone chose.
		{"P106751991167301D", 0, false},
		{"nonsense", 0, false},
	}
	for _, c := range cases {
		got, ok := parseICSDuration(c.value)
		if ok != c.ok {
			t.Errorf("parseICSDuration(%q) ok = %v, want %v", c.value, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseICSDuration(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

// TestFilesWithUnusualLineEndingsStillYieldTheirEvents verifies the two shapes
// that used to import nothing while answering that everything was fine: a file
// separated by bare CR, which is one long line, and a byte order mark written
// against a component's first line instead of at the head of the file.
func TestFilesWithUnusualLineEndingsStillYieldTheirEvents(t *testing.T) {
	body := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"SUMMARY:Clinic",
		"DTSTART:20260601T100000Z",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r")
	if events := parseICS(body); len(events) != 1 || events[0].summary != "Clinic" {
		t.Errorf("a file written with bare CR still describes one event, got %+v", events)
	}

	marked := "\ufeffBEGIN:VCALENDAR\r\n\ufeffBEGIN:VEVENT\r\nSUMMARY:Clinic\r\n" +
		"DTSTART:20260601T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if events := parseICS(marked); len(events) != 1 || events[0].summary != "Clinic" {
		t.Errorf("a stray byte order mark carries no text and must not hide the event, got %+v", events)
	}
}
