package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The import endpoint eats a whole file chosen by whoever uploads it, and the
// parser is hand-written. These tests point deliberately hostile input at it.
//
// The bar for every case is the same: a defined refusal or a clean partial
// import. Never a 500, never a hang, and never a row that lands where nobody
// can see or delete it. Where the parser is best-effort per event, what is
// asserted is what actually survived -- a "success" that quietly dropped half
// the file would otherwise read as a clean import.

// hostileWindow is the range every fixture in this file falls inside, wide
// enough to show what landed and narrow enough for one listing request.
const hostileWindow = "?start=2026-05-25&end=2026-06-30"

func importICS(t *testing.T, calURL, token, ics string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost, calURL+"/import", token,
		map[string]any{"ics": ics})
}

// importOK posts a file and requires the endpoint to answer with a result
// rather than an error, which is the first half of the bar on its own.
func importOK(t *testing.T, calURL, token, ics string) importResult {
	t.Helper()
	status, raw := importICS(t, calURL, token, ics)
	require.Equal(t, http.StatusOK, status,
		"a file the parser cannot use is the caller's problem to be told about, not the server's to fall over on: %s",
		cut(string(raw), 400))
	var res importResult
	require.NoError(t, json.Unmarshal(raw, &res), "body: %s", cut(string(raw), 400))
	return res
}

// importRawJSON posts a body byte for byte, for input a Go string cannot carry
// to the server intact.
func importRawJSON(t *testing.T, calURL, token, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, calURL+"/import", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// wrap puts VEVENT bodies inside a minimal VCALENDAR.
func wrap(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" + body + "END:VCALENDAR\r\n"
}

// vevent renders one VEVENT from its property lines.
func vevent(lines ...string) string {
	return "BEGIN:VEVENT\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\n"
}

// oneGoodEvent is a VEVENT with nothing wrong with it, used to tell "the
// parser rejected this file" apart from "the parser lost its place in it".
func oneGoodEvent() string {
	return vevent("UID:good@example.com", "SUMMARY:Fine",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z")
}

func hostileCalendar(t *testing.T, tt *helpers.TestTenant, name string) string {
	t.Helper()
	return testServerURL + "/calendars/" + newCalendar(t, tt, name)
}

func listHostile(t *testing.T, calURL, token string) []recInstance {
	t.Helper()
	var listed []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events"+hostileWindow, token, nil, &listed)
	return listed
}

// deleteEverything removes every row the calendar shows. An import that leaves
// behind something its owner cannot get rid of is worse than one that refused
// the file, so every test that writes anything ends here.
func deleteEverything(t *testing.T, calURL, token string, rows []recInstance) {
	t.Helper()
	done := map[string]bool{}
	for _, e := range rows {
		// A series is listed once per occurrence under one id; deleting the
		// series answers for all of them.
		seriesID, _, _ := strings.Cut(e.ID, "_")
		if done[seriesID] {
			continue
		}
		done[seriesID] = true
		status, body := helpers.DoJSONStatus(t, http.MethodDelete,
			calURL+"/events/"+e.ID+"?scope=all", token, nil)
		require.Equal(t, http.StatusNoContent, status,
			"an imported row must be removable by the person who imported it: %s", cut(string(body), 200))
	}
	require.Empty(t, listHostile(t, calURL, token), "the calendar must come back empty")
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// A file that stops mid-line, never closes a component, or uses line endings
// no exporter should produce still has to leave the endpoint answering. What
// is pinned here is that the parser does not lose its place: a broken file
// imports what it unambiguously contains and nothing else.
func TestICalImportSurvivesStructurallyBrokenFiles(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct {
		name string
		ics  string
		want importResult
	}{
		{
			name: "truncated mid-property",
			ics:  "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a@example.com\r\nSUMMARY:Half a summ",
			want: importResult{},
		},
		{
			name: "VEVENT never closed",
			ics: "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a@example.com\r\nSUMMARY:Open\r\n" +
				"DTSTART:20260601T100000Z\r\n",
			want: importResult{},
		},
		{
			// The wrapper carries nothing the importer reads, so a fragment of
			// a file -- what a copy-paste produces -- still imports.
			name: "no VCALENDAR wrapper",
			ics:  oneGoodEvent(),
			want: importResult{Imported: 1},
		},
		{
			name: "byte order mark at the start of the file",
			ics:  "\ufeff" + wrap(oneGoodEvent()),
			want: importResult{Imported: 1},
		},
		{
			name: "LF line endings",
			ics:  strings.ReplaceAll(wrap(oneGoodEvent()), "\r\n", "\n"),
			want: importResult{Imported: 1},
		},
		{
			name: "line endings mixed within one file",
			ics: "BEGIN:VCALENDAR\nBEGIN:VEVENT\r\nUID:m@example.com\nSUMMARY:Mixed\r\n" +
				"DTSTART:20260601T100000Z\nDTEND:20260601T110000Z\r\nEND:VEVENT\nEND:VCALENDAR\r\n",
			want: importResult{Imported: 1},
		},
		{
			name: "END:VEVENT before any BEGIN",
			ics:  wrap("END:VEVENT\r\n" + oneGoodEvent()),
			want: importResult{Imported: 1},
		},
		{
			// The unclosed first event is abandoned rather than merged into the
			// second, which would give the survivor properties from both.
			name: "VEVENT opened twice",
			ics: wrap("BEGIN:VEVENT\r\nUID:n1@example.com\r\nSUMMARY:Outer\r\n" +
				"DTSTART:20260601T100000Z\r\n" + oneGoodEvent()),
			want: importResult{Imported: 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Broken "+c.name)
			require.Equal(t, c.want, importOK(t, calURL, tt.AccessToken, c.ics))

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, c.want.Imported,
				"the calendar must hold exactly what the response said it imported")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// A file whose lines are separated by bare CR, and one whose BOM sits against
// the BEGIN:VEVENT rather than at the top, both contain a complete event that
// no line of the parser ever sees.
//
// Both are accepted with a result of all zeroes. That is not a lie -- nothing
// was imported -- but it is indistinguishable from a file that genuinely had
// nothing in it, so what is pinned here is only that the report and the
// calendar agree with each other.
func TestICalImportReportsNothingItDidNotDo(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, ics string }{
		{"bare CR line endings", strings.ReplaceAll(wrap(oneGoodEvent()), "\r\n", "\r")},
		{"byte order mark against BEGIN:VEVENT", wrap("\ufeff" + oneGoodEvent())},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Silent "+c.name)
			require.Equal(t, importResult{}, importOK(t, calURL, tt.AccessToken, c.ics),
				"an event the parser never reaches must not be counted as imported")
			require.Empty(t, listHostile(t, calURL, tt.AccessToken),
				"and nothing may land on the calendar either")
		})
	}
}

// The two bounds on the body itself. Both are refusals the caller can read,
// which is what keeps a 6 MiB paste from being answered by the parser at all.
func TestICalImportRefusesBodiesOutsideItsBounds(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Bounds")

	status, _ := importICS(t, calURL, tt.AccessToken, strings.Repeat("x", 6*1024*1024))
	require.Equal(t, http.StatusUnprocessableEntity, status, "a file past the size limit")

	status, _ = importICS(t, calURL, tt.AccessToken, "")
	require.Equal(t, http.StatusUnprocessableEntity, status, "an empty body")

	require.Empty(t, listHostile(t, calURL, tt.AccessToken))
}

// A property value longer than the column behind it is refused rather than cut
// down to fit. A truncating import would be the worst outcome available: the
// file reads as imported and the events quietly say something shorter than
// what their author wrote.
func TestICalImportRefusesValuesTooLongForTheirColumnRatherThanTruncating(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	huge := strings.Repeat("A", 200000)

	// One SUMMARY spread over fifty thousand continuation lines. Unfolding
	// joins them into a single value, so this arrives as the case above by a
	// different route -- and the joining itself must terminate.
	var folded strings.Builder
	folded.WriteString("BEGIN:VEVENT\r\nUID:folded@example.com\r\nSUMMARY:Folded")
	for range 50000 {
		folded.WriteString("\r\n ABCDEFGHIJ")
	}
	folded.WriteString("\r\nDTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\nEND:VEVENT\r\n")

	cases := []struct{ name, ics string }{
		{"200 KB SUMMARY", wrap(vevent("UID:h1@example.com", "SUMMARY:"+huge,
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))},
		{"200 KB LOCATION", wrap(vevent("UID:h2@example.com", "SUMMARY:Loc", "LOCATION:"+huge,
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))},
		{"200 KB URL", wrap(vevent("UID:h3@example.com", "SUMMARY:Url",
			"URL:https://e.example/"+huge,
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))},
		{"50000 folded continuation lines", wrap(folded.String())},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Long "+c.name)
			require.Equal(t, importResult{Failed: 1}, importOK(t, calURL, tt.AccessToken, c.ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken),
				"a value the store refused must leave no event behind")
		})
	}
}

// Folding is how a long value is legally written, so the joined value has to
// be what lands -- the counterpart to the case above, where the join is what
// makes the value too long.
func TestICalImportJoinsFoldedLinesIntoOneValue(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Folded")

	ics := wrap("BEGIN:VEVENT\r\nUID:fold@example.com\r\n" +
		"SUMMARY:A value split mid-wo\r\n rd and continued\r\n\t with a tab\r\n" +
		"DTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\nEND:VEVENT\r\n")
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	// The break and the one space or tab that marks it are both removed, so a
	// value can be folded mid-word and any surviving space is the author's.
	require.Equal(t, "A value split mid-word and continued with a tab", listed[0].Title)
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// A description too long for a title column still fits the one it goes in, and
// what fits must arrive whole. This is the other half of the truncation
// question: refusing what does not fit is only right if what does fit survives.
func TestICalImportKeepsALongDescriptionWhole(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Long description")

	const size = 200000
	ics := wrap(vevent("UID:desc@example.com", "SUMMARY:Long notes",
		"DESCRIPTION:"+strings.Repeat("B", size),
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)

	var evt struct {
		Memo string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Len(t, evt.Memo, size, "the description must arrive at its full length or not at all")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// A URL a Go string carries happily is not one the column behind it can hold:
// it is latin1, so a perfectly ordinary internationalised address costs the
// whole event rather than just the property.
func TestICalImportRefusesAnEventWhoseURLTheColumnCannotHold(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Non-latin1 URL")

	ics := wrap(vevent("UID:jp@example.com", "SUMMARY:Meeting",
		"URL:https://example.com/日本語",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importResult{Failed: 1}, importOK(t, calURL, tt.AccessToken, ics))
	require.Empty(t, listHostile(t, calURL, tt.AccessToken))
}

// A rule the expander cannot honour must not collapse into a single occurrence
// or be stored as a boundary the calendar cannot represent. Either way the
// event is accounted for and nothing lands halfway.
func TestICalImportRefusesRecurrenceItCannotHonour(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct {
		name string
		rule string
		want importResult
	}{
		{"COUNT past the supported maximum", "FREQ=DAILY;COUNT=999999", importResult{Skipped: 1}},
		{"INTERVAL past the supported maximum", "FREQ=DAILY;INTERVAL=99999", importResult{Skipped: 1}},
		{"INTERVAL of zero", "FREQ=DAILY;INTERVAL=0", importResult{Skipped: 1}},
		{"negative INTERVAL", "FREQ=DAILY;INTERVAL=-1", importResult{Skipped: 1}},
		{"COUNT past what an int can hold", "FREQ=DAILY;COUNT=99999999999999999999", importResult{Skipped: 1}},
		{"nothing that parses as a rule", "=;;==;FREQ", importResult{Skipped: 1}},
		{"an unsupported ordinal BYDAY", "FREQ=MONTHLY;BYDAY=2MO", importResult{Skipped: 1}},
		// Accepted by the rule reader, then rejected by the store: the last of
		// a thousand yearly occurrences at a nine-hundred-year interval lands
		// past every date the column can name.
		{"a series ending past the end of time", "FREQ=YEARLY;COUNT=1000;INTERVAL=999", importResult{Failed: 1}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Rule "+c.name)
			ics := wrap(vevent("UID:r@example.com", "SUMMARY:Rule",
				"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z", "RRULE:"+c.rule))
			require.Equal(t, c.want, importOK(t, calURL, tt.AccessToken, ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken),
				"an event whose rule was refused must not land as a single occurrence")
		})
	}
}

// A rule that is absurd but representable is imported, and then has to stay
// bounded when it is read back: a thousand occurrences nine hundred years
// apart is one occurrence in any window a reader can ask for.
func TestICalImportedAbsurdButValidRuleStaysBoundedWhenListed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Sparse series")

	ics := wrap(vevent("UID:sparse@example.com", "SUMMARY:Sparse",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
		"RRULE:FREQ=DAILY;COUNT=1000;INTERVAL=999"))
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1, "one occurrence falls in the window, whatever the series claims")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// An RRULE property with no value is not a recurrence. It has to import as the
// single event it describes rather than being read as a rule with no fields.
func TestICalImportReadsAnEmptyRRuleAsNoRecurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Empty rule")

	ics := wrap(vevent("UID:empty@example.com", "SUMMARY:No rule",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z", "RRULE:"))
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	require.False(t, listed[0].IsRecurrence)
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// An event the file does not date is skipped rather than filed at whatever
// zero time the parser was left holding, which would put it at the start of
// the epoch where nobody looking for it would find it.
func TestICalImportSkipsEventsItCannotDate(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, dtstart string }{
		{"a start that is not a date at all", "not-a-date"},
		{"a date that does not exist", "20261332T256199Z"},
		// Year one is the zero value of a Go time, so a start that parses to
		// it cannot be told apart from one that never parsed.
		{"the first instant Go can represent", "00010101T000000Z"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Undated "+c.name)
			ics := wrap(vevent("UID:u@example.com", "SUMMARY:Undated", "DTSTART:"+c.dtstart))
			require.Equal(t, importResult{Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// Two events the store refuses outright, each for a reason the file gave it.
// Both are counted, and neither leaves a row behind.
func TestICalImportRefusesEventsTheCalendarCannotHold(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, ics string }{
		{
			// end_at >= start_at is a constraint on the table, so an inverted
			// event cannot be written however it got here.
			name: "an event that ends before it starts",
			ics: wrap(vevent("UID:inv@example.com", "SUMMARY:Inverted",
				"DTSTART:20260601T100000Z", "DTEND:20260601T090000Z")),
		},
		{
			// No DTEND, so the importer adds an hour -- which lands past the
			// last instant the column can name.
			name: "an event in the last hour of the last representable year",
			ics:  wrap(vevent("UID:far@example.com", "SUMMARY:Far future", "DTSTART:99991231T235959Z")),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Refused "+c.name)
			require.Equal(t, importResult{Failed: 1}, importOK(t, calURL, tt.AccessToken, c.ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// A TZID names a zone the server may not have, and the name comes from the
// file. It has to resolve to UTC -- the same zone the times were read in --
// rather than being stored as whatever string arrived, and a name shaped like
// a path must not be looked up as one.
func TestICalImportFallsBackToUTCForAZoneItDoesNotKnow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, tzid string }{
		{"a zone that does not exist", "Mars/Olympus"},
		{"a zone name shaped like a path", "../../../../etc/passwd"},
		{"an empty zone name", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Zone "+c.name)
			param := ""
			if c.tzid != "" {
				param = ";TZID=" + c.tzid
			}
			ics := wrap(vevent("UID:tz@example.com", "SUMMARY:Zoned",
				"DTSTART"+param+":20260601T100000",
				"DTEND"+param+":20260601T110000"))
			require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1)
			require.Equal(t, "2026-06-01T10:00:00Z", listed[0].StartAt,
				"the wall clock is read as UTC, so it must be stored as the same instant")

			var evt struct {
				Timezone string `json:"timezone"`
			}
			helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
			require.Equal(t, "UTC", evt.Timezone,
				"an unresolvable name must not be kept as the event's zone")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// Two events sharing a UID are two events. Folding them together on the way in
// would lose one, and a file is free to repeat a UID however badly.
func TestICalImportKeepsBothEventsThatShareAUID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Duplicate UIDs")

	ics := wrap(
		vevent("UID:dup@example.com", "SUMMARY:One",
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z") +
			vevent("UID:dup@example.com", "SUMMARY:Two",
				"DTSTART:20260602T100000Z", "DTEND:20260602T110000Z"))
	require.Equal(t, importResult{Imported: 2}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 2)
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// EXDATE is a list, and one unreadable entry in it must not take the readable
// ones down with it -- nor may an entry naming an occurrence the series never
// has remove one that it does.
func TestICalImportHonoursOnlyTheExdatesItCanRead(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	t.Run("an exclusion matching no occurrence changes nothing", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Exdate miss")
		ics := wrap(vevent("UID:x1@example.com", "SUMMARY:Holes",
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
			"RRULE:FREQ=DAILY;COUNT=3", "EXDATE:20301231T235959Z"))
		require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Equal(t, []string{
			"2026-06-01T10:00:00Z", "2026-06-02T10:00:00Z", "2026-06-03T10:00:00Z",
		}, startsOf(listed))
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})

	t.Run("junk beside a real exclusion leaves the real one standing", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Exdate junk")
		ics := wrap(vevent("UID:x2@example.com", "SUMMARY:Holes",
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
			"RRULE:FREQ=DAILY;COUNT=3", "EXDATE:nonsense,,,20260602T100000Z"))
		require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Equal(t, []string{"2026-06-01T10:00:00Z", "2026-06-03T10:00:00Z"}, startsOf(listed),
			"the readable exclusion must still cancel its occurrence")
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})
}

// A changed occurrence names the series it departs from. With nothing in the
// file to attach it to, importing it as an event of its own would put a
// duplicate beside whatever it was meant to replace, so it is skipped and
// counted.
func TestICalImportSkipsAChangedOccurrenceWithNothingToAttachTo(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	t.Run("no event in the file carries the UID", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Orphan override")
		ics := wrap(vevent("UID:orphan@example.com", "SUMMARY:Orphan",
			"RECURRENCE-ID:20260601T100000Z",
			"DTSTART:20260601T120000Z", "DTEND:20260601T130000Z"))
		require.Equal(t, importResult{Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))
		require.Empty(t, listHostile(t, calURL, tt.AccessToken))
	})

	t.Run("the event carrying the UID is not a series", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Override of a single event")
		ics := wrap(
			vevent("UID:solo@example.com", "SUMMARY:Solo",
				"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z") +
				vevent("UID:solo@example.com", "SUMMARY:Override",
					"RECURRENCE-ID:20260601T100000Z",
					"DTSTART:20260601T120000Z", "DTEND:20260601T130000Z"))
		require.Equal(t, importResult{Imported: 1, Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Len(t, listed, 1)
		require.Equal(t, "Solo", listed[0].Title)
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})
}

// Text from the file reaches a screen, so what a value can contain matters.
// Each of these has to arrive as one value, stay readable, and stay removable.
func TestICalImportKeepsHostileTextInOneValue(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, summary, want string }{
		{
			// The escapes are the property's own, so what they produce belongs
			// inside the value -- the second DTSTART here is text, not a
			// property, and must not move the event to 1970.
			name:    "escapes that look like they end the property",
			summary: `line one\nDTSTART:19700101T000000Z\;still text`,
			want:    "line one\nDTSTART:19700101T000000Z;still text",
		},
		{"a four-byte character", "\U0001F4C5 emoji", "\U0001F4C5 emoji"},
		{"an unpaired escape at the end of the value", `trailing backslash \`, `trailing backslash \`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Text "+c.name)
			ics := wrap(vevent("UID:txt@example.com", "SUMMARY:"+c.summary,
				"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
			require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1)
			require.Equal(t, c.want, listed[0].Title)
			require.Equal(t, "2026-06-01T10:00:00Z", listed[0].StartAt,
				"nothing inside a value may reach the event's own properties")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// Control characters are not text a property may contain, and nothing on the
// way in removes them: what the file said is what the title, location and
// notes end up holding, byte for byte.
//
// What is pinned here is the part that is defensible either way -- the event
// lands as one row, reads back exactly as it arrived, and can be deleted. It
// does NOT pin what leaves through the export, which today carries the same
// bytes straight back out into a .ics file.
func TestICalImportDoesNotFilterControlCharactersOutOfText(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Control characters")

	const title = "before\x00after\x1b[31mred\x07"
	ics := wrap(vevent("UID:ctl@example.com",
		"SUMMARY:"+title,
		"LOCATION:room\x1b[2Jcleared",
		"DESCRIPTION:notes\x00cut",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	require.Equal(t, title, listed[0].Title,
		"whatever is stored must be what the file said, not a silently shortened copy of it")

	var evt struct {
		Location string `json:"location"`
		Memo     string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Equal(t, "room\x1b[2Jcleared", evt.Location)
	require.Equal(t, "notes\x00cut", evt.Memo,
		"a NUL must not end the value early")

	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// A value carrying a real newline has to be escaped again on the way out, or
// the export would hand the next reader a file where one event's title has
// become another event's properties.
func TestICalExportReEscapesTextImportedWithNewlines(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Re-escape")

	ics := wrap(vevent("UID:esc@example.com",
		`SUMMARY:one\ntwo\;three`,
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importResult{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	exported := fetchICS(t, calURL, tt.AccessToken)
	require.Contains(t, exported, `SUMMARY:one\ntwo\;three`)
	require.NotContains(t, exported, "SUMMARY:one\r\ntwo",
		"a newline inside a value must never be written as a line break")

	deleteEverything(t, calURL, tt.AccessToken, listHostile(t, calURL, tt.AccessToken))
}

// A JSON body may carry an unpaired surrogate, which is not text any Go string
// can hold. The decoder substitutes the replacement character, and what
// matters here is that the event lands as one readable row rather than as a
// broken byte sequence in the title column.
func TestICalImportAcceptsABodyWithAnUnpairedSurrogate(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Surrogate")

	status, raw := importRawJSON(t, calURL, tt.AccessToken,
		`{"ics":"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:s@example.com\r\nSUMMARY:lone\ud800surrogate\r\n`+
			`DTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}`)
	require.Equal(t, http.StatusOK, status, "body: %s", cut(string(raw), 300))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	require.Equal(t, "lone�surrogate", listed[0].Title)
	require.True(t, utf8.ValidString(listed[0].Title), "what is stored must be text")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// The whole file, at a size a person could plausibly upload, mixing every
// outcome the importer has. Every event has to be accounted for exactly once,
// the calendar has to hold exactly what the response claimed, and all of it
// has to be removable.
func TestICalImportAccountsForEveryEventInALargeHostileFile(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Large hostile")

	const groups = 150
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	for i := range groups {
		// Importable.
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:ok-%d@example.com\r\nSUMMARY:Good %d\r\n"+
			"DTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\nEND:VEVENT\r\n", i, i)
		// No title and no date: nothing to import.
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:bare-%d@example.com\r\nEND:VEVENT\r\n", i)
		// A rule the expander cannot honour.
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:rule-%d@example.com\r\nSUMMARY:Rule %d\r\n"+
			"DTSTART:20260601T100000Z\r\nRRULE:FREQ=DAILY;COUNT=999999\r\nEND:VEVENT\r\n", i, i)
		// Refused by the store.
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:inv-%d@example.com\r\nSUMMARY:Inverted %d\r\n"+
			"DTSTART:20260601T100000Z\r\nDTEND:20260601T090000Z\r\nEND:VEVENT\r\n", i, i)
		// A changed occurrence with no series in the file.
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:orphan-%d@example.com\r\nSUMMARY:Orphan %d\r\n"+
			"RECURRENCE-ID:20260601T100000Z\r\nDTSTART:20260601T120000Z\r\nEND:VEVENT\r\n", i, i)
	}
	b.WriteString("END:VCALENDAR\r\n")

	res := importOK(t, calURL, tt.AccessToken, b.String())
	require.Equal(t, groups*5, res.Imported+res.Skipped+res.Failed+res.Truncated,
		"every VEVENT in the file must be accounted for exactly once")
	require.Equal(t, importResult{Imported: groups, Skipped: groups * 3, Failed: groups}, res)

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, res.Imported,
		"the calendar must hold exactly what the response said it imported")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

func TestICalImportProbe3(t *testing.T) {
	bootstrap(t)
	tt := helpers.NewTenant(t, testServerURL)

	// Q3: is an event the store refuses rolled back cleanly, and does it leave
	// a log entry behind?
	calURL := hostileCalendar(t, tt, "Probe3 rollback")
	ics := wrap(
		vevent("UID:p1@example.com", "SUMMARY:First", "DTSTART:20260601T100000Z", "DTEND:20260601T110000Z") +
			vevent("UID:p2@example.com", "SUMMARY:"+strings.Repeat("A", 200000),
				"DTSTART:20260601T120000Z", "DTEND:20260601T130000Z") +
			vevent("UID:p3@example.com", "SUMMARY:Third", "DTSTART:20260601T140000Z", "DTEND:20260601T150000Z"))
	res := importOK(t, calURL, tt.AccessToken, ics)
	t.Logf("PROBE3 mixed-file result: %+v", res)
	listed := listHostile(t, calURL, tt.AccessToken)
	t.Logf("PROBE3 rows on calendar: %d", len(listed))
	for _, e := range listed {
		t.Logf("PROBE3   %s", e.Title)
	}
	var feed struct {
		Items []struct {
			Action  string `json:"action"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=50", tt.AccessToken, nil, &feed)
	t.Logf("PROBE3 activity entries: %d", len(feed.Items))
	for _, i := range feed.Items {
		t.Logf("PROBE3   %s %q", i.Action, cut(i.Summary, 40))
	}

	// A Windows zone name, which is what Outlook writes.
	calURL2 := hostileCalendar(t, tt, "Probe3 windows zone")
	ics2 := wrap(vevent("UID:w@example.com", "SUMMARY:Outlook",
		"DTSTART;TZID=Tokyo Standard Time:20260601T100000",
		"DTEND;TZID=Tokyo Standard Time:20260601T110000"))
	t.Logf("PROBE3 windows zone result: %+v", importOK(t, calURL2, tt.AccessToken, ics2))
	for _, e := range listHostile(t, calURL2, tt.AccessToken) {
		t.Logf("PROBE3   start=%s title=%s", e.StartAt, e.Title)
	}

	// ORGANIZER and ATTENDEE.
	calURL3 := hostileCalendar(t, tt, "Probe3 attendees")
	ics3 := wrap(vevent("UID:at@example.com", "SUMMARY:Meeting",
		"ORGANIZER;CN=Someone Else:mailto:boss@example.com",
		"ATTENDEE;CN=A:mailto:a@example.com",
		"ATTENDEE;CN=B:mailto:b@example.com",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	t.Logf("PROBE3 attendee result: %+v", importOK(t, calURL3, tt.AccessToken, ics3))
	l3 := listHostile(t, calURL3, tt.AccessToken)
	if len(l3) == 1 {
		var evt struct {
			Participants []string `json:"participants"`
			OwnerID      string   `json:"ownerId"`
			CreatedBy    string   `json:"createdBy"`
			Attendees    []any    `json:"attendees"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL3+"/events/"+l3[0].ID, tt.AccessToken, nil, &evt)
		t.Logf("PROBE3 participants=%v attendees=%d owner==importer:%v",
			evt.Participants, len(evt.Attendees), evt.OwnerID == tt.UserID)
	}

	// The same file twice.
	calURL4 := hostileCalendar(t, tt, "Probe3 twice")
	same := wrap(oneGoodEvent())
	t.Logf("PROBE3 first import: %+v", importOK(t, calURL4, tt.AccessToken, same))
	t.Logf("PROBE3 second import: %+v", importOK(t, calURL4, tt.AccessToken, same))
	t.Logf("PROBE3 rows after importing the same file twice: %d", len(listHostile(t, calURL4, tt.AccessToken)))
}
