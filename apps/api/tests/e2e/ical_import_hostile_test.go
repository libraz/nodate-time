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

// importReport is the whole import response. It is wider than importResult
// because these tests are the ones that care what the file did to the counters
// the rest of the suite never sees: how much of it was placed in a zone the
// server could not resolve, how much of the failure the file itself caused,
// and whether the body was read at all.
type importReport struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	// Rejected is the part of Failed the file caused, so a report of
	// {Failed: 1, Rejected: 1} is one event no retry can ever land.
	Rejected int `json:"rejected"`
	// Duplicates is what the calendar already held, which is a different thing
	// from an event the parser could not use.
	Duplicates       int  `json:"duplicates"`
	Truncated        int  `json:"truncated"`
	UnknownTimezones int  `json:"unknownTimezones"`
	Unreadable       bool `json:"unreadable"`
}

func importICS(t *testing.T, calURL, token, ics string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost, calURL+"/import", token,
		map[string]any{"ics": ics})
}

// importOK posts a file and requires the endpoint to answer with a result
// rather than an error, which is the first half of the bar on its own.
func importOK(t *testing.T, calURL, token, ics string) importReport {
	t.Helper()
	status, raw := importICS(t, calURL, token, ics)
	require.Equal(t, http.StatusOK, status,
		"a file the parser cannot use is the caller's problem to be told about, not the server's to fall over on: %s",
		cut(string(raw), 400))
	var res importReport
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
		want importReport
	}{
		{
			name: "truncated mid-property",
			ics:  "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a@example.com\r\nSUMMARY:Half a summ",
			want: importReport{},
		},
		{
			name: "VEVENT never closed",
			ics: "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a@example.com\r\nSUMMARY:Open\r\n" +
				"DTSTART:20260601T100000Z\r\n",
			want: importReport{},
		},
		{
			// The wrapper carries nothing the importer reads, so a fragment of
			// a file -- what a copy-paste produces -- still imports.
			name: "no VCALENDAR wrapper",
			ics:  oneGoodEvent(),
			want: importReport{Imported: 1},
		},
		{
			name: "byte order mark at the start of the file",
			ics:  "\ufeff" + wrap(oneGoodEvent()),
			want: importReport{Imported: 1},
		},
		{
			name: "LF line endings",
			ics:  strings.ReplaceAll(wrap(oneGoodEvent()), "\r\n", "\n"),
			want: importReport{Imported: 1},
		},
		{
			name: "line endings mixed within one file",
			ics: "BEGIN:VCALENDAR\nBEGIN:VEVENT\r\nUID:m@example.com\nSUMMARY:Mixed\r\n" +
				"DTSTART:20260601T100000Z\nDTEND:20260601T110000Z\r\nEND:VEVENT\nEND:VCALENDAR\r\n",
			want: importReport{Imported: 1},
		},
		{
			name: "END:VEVENT before any BEGIN",
			ics:  wrap("END:VEVENT\r\n" + oneGoodEvent()),
			want: importReport{Imported: 1},
		},
		{
			// The unclosed first event is abandoned rather than merged into the
			// second, which would give the survivor properties from both.
			name: "VEVENT opened twice",
			ics: wrap("BEGIN:VEVENT\r\nUID:n1@example.com\r\nSUMMARY:Outer\r\n" +
				"DTSTART:20260601T100000Z\r\n" + oneGoodEvent()),
			want: importReport{Imported: 1},
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
// the BEGIN:VEVENT rather than at the top, each contain a complete event that
// nothing in the file makes ambiguous. Both shapes come out of real exporters,
// and both used to import nothing at all: the whole body read as one line, or
// the component line failed to match because of a mark that carries no text.
func TestICalImportReadsFilesWrittenWithBareCROrAStrayBOM(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, ics string }{
		{"bare CR line endings", strings.ReplaceAll(wrap(oneGoodEvent()), "\r\n", "\r")},
		{"byte order mark against BEGIN:VEVENT", wrap("\ufeff" + oneGoodEvent())},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Readable "+c.name)
			require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, c.ics),
				"the file describes one event and nothing about it is ambiguous")

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1)
			require.Equal(t, "Fine", listed[0].Title)
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// A body the parser recognises nothing in reports that, rather than answering
// with the all-zero result that a calendar containing no events also gets.
// Both imported nothing; only one of them is worth telling the caller about,
// and every counter reading zero cannot say which happened.
func TestICalImportSaysWhenItRecognisedNothingInTheBody(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct {
		name string
		ics  string
		want importReport
	}{
		{
			name: "a body that is not a calendar at all",
			ics:  "Dear diary,\r\n\r\nI meant to export the calendar and exported this.\r\n",
			want: importReport{Unreadable: true},
		},
		{
			// Component names are written in upper case; this parser reads them
			// that way, so a lower-cased file is one it cannot use -- which is
			// the answer to give rather than a clean import of nothing.
			name: "a calendar whose component names are lower case",
			ics:  strings.ToLower(wrap(oneGoodEvent())),
			want: importReport{Unreadable: true},
		},
		{
			// A calendar with nothing in it is not unreadable: it was read, and
			// it said there was nothing to import.
			name: "a calendar with no events in it",
			ics:  wrap(""),
			want: importReport{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Unread "+c.name)
			require.Equal(t, c.want, importOK(t, calURL, tt.AccessToken, c.ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken),
				"nothing may land on the calendar either way")
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
//
// The refusal is counted as one the file caused. Nothing about uploading it
// again shortens the value, so reporting it as a plain failure would invite a
// retry that can only end the same way.
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
			require.Equal(t, importReport{Failed: 1, Rejected: 1},
				importOK(t, calURL, tt.AccessToken, c.ics))
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
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)

	var evt struct {
		Memo string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Len(t, evt.Memo, size, "the description must arrive at its full length or not at all")
	deleteEverything(t, calURL, tt.AccessToken, listed)
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
		want importReport
	}{
		{"COUNT past the supported maximum", "FREQ=DAILY;COUNT=999999", importReport{Skipped: 1}},
		{"INTERVAL past the supported maximum", "FREQ=DAILY;INTERVAL=99999", importReport{Skipped: 1}},
		{"INTERVAL of zero", "FREQ=DAILY;INTERVAL=0", importReport{Skipped: 1}},
		{"negative INTERVAL", "FREQ=DAILY;INTERVAL=-1", importReport{Skipped: 1}},
		{"COUNT past what an int can hold", "FREQ=DAILY;COUNT=99999999999999999999", importReport{Skipped: 1}},
		{"nothing that parses as a rule", "=;;==;FREQ", importReport{Skipped: 1}},
		{"an unsupported ordinal BYDAY", "FREQ=MONTHLY;BYDAY=2MO", importReport{Skipped: 1}},
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

	cases := []struct{ name, rule string }{
		{"a thousand days nine hundred years apart", "FREQ=DAILY;COUNT=1000;INTERVAL=999"},
		// The same shape by year, whose last occurrence falls past every date
		// the boundary column can name. The series is stored by the rule it
		// carries, not by that date, so it imports like any other.
		{"a thousand years nine hundred years apart", "FREQ=YEARLY;COUNT=1000;INTERVAL=999"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Sparse "+c.name)
			ics := wrap(vevent("UID:sparse@example.com", "SUMMARY:Sparse",
				"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z", "RRULE:"+c.rule))
			require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1,
				"one occurrence falls in the window, whatever the series claims")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
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
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
			require.Equal(t, importReport{Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// Two events the store refuses outright, each for a reason the file gave it.
// Both are counted as refusals the file caused rather than as plain failures:
// a second upload of the same bytes gets the same answer, and "failed" on its
// own is what invites one.
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
			require.Equal(t, importReport{Failed: 1, Rejected: 1},
				importOK(t, calURL, tt.AccessToken, c.ics))
			require.Empty(t, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// A TZID names a zone the server may not have, and the name comes from the
// file. It has to resolve to UTC -- the same zone the times were read in --
// rather than being stored as whatever string arrived, and a name shaped like
// a path must not be looked up as one.
//
// UTC is the only fallback available, and it is the wrong instant for every
// zone that is not UTC, so the import says how many events it placed that way.
// A default nobody is told about is the defect; one that reports itself is
// something its reader can go and check.
func TestICalImportCountsTheEventsWhoseZoneItCouldNotResolve(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct {
		name string
		tzid string
		want importReport
	}{
		{"a zone that does not exist", "Mars/Olympus", importReport{Imported: 1, UnknownTimezones: 1}},
		{"a zone name shaped like a path", "../../../../etc/passwd", importReport{Imported: 1, UnknownTimezones: 1}},
		// "Local" resolves against whichever machine happens to be running the
		// server, which has nothing to do with the calendar the file came from.
		{"the name of the zone the server itself runs in", "Local", importReport{Imported: 1, UnknownTimezones: 1}},
		// No TZID at all is a floating time, not a zone that failed to resolve:
		// there is nothing here the file got wrong to report.
		{"an empty zone name", "", importReport{Imported: 1}},
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
			require.Equal(t, c.want, importOK(t, calURL, tt.AccessToken, ics))

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
//
// The UID is also what recognition matches on, and only one event on a
// calendar can hold a given one. The second of these therefore imports without
// an identity rather than being mistaken for the first: an event that arrives
// twice can be deleted, and one that never arrives cannot be got back.
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
	require.Equal(t, importReport{Imported: 2}, importOK(t, calURL, tt.AccessToken, ics))

	// What the second one costs: it is the one without an identity, so a
	// re-upload recognises the first and takes another copy of it.
	require.Equal(t, importReport{Imported: 1, Duplicates: 1},
		importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 3)
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
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
		require.Equal(t, importReport{Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))
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
		require.Equal(t, importReport{Imported: 1, Skipped: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
			require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1)
			require.Equal(t, c.want, listed[0].Title)
			require.Equal(t, "2026-06-01T10:00:00Z", listed[0].StartAt,
				"nothing inside a value may reach the event's own properties")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// Control characters are not text a property may contain. RFC 5545 allows no
// C0 character but HTAB inside a value, and what one does instead of showing
// is up to whatever renders it: a terminal executes an escape sequence rather
// than printing it, so a title carrying one runs a command in the shell of
// anyone who cats or greps the file.
//
// They are dropped on the way in, and the export is incapable of writing one
// out. Both halves are needed: the way in is the only place the value can
// still be refused, and the way out is the only thing that answers for rows
// this import did not write.
func TestICalImportDropsControlCharactersAndTheExportNeverWritesThem(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Control characters")

	ics := wrap(vevent("UID:ctl@example.com",
		"SUMMARY:before\x00after\x1b[31mred\x07",
		"LOCATION:room\x1b[2Jcleared",
		"DESCRIPTION:notes\x00cut\ttabbed",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics),
		"the characters are not worth the event they arrived on")

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	require.Equal(t, "beforeafter[31mred", listed[0].Title,
		"what a terminal would have executed is gone; the text around it is untouched")

	var evt struct {
		Location string `json:"location"`
		Memo     string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Equal(t, "room[2Jcleared", evt.Location)
	require.Equal(t, "notescut\ttabbed", evt.Memo,
		"a NUL is dropped like the rest; a tab is text a value may contain, so it stays")

	exported := fetchICS(t, calURL, tt.AccessToken)
	require.Equal(t, -1, strings.IndexFunc(exported, func(r rune) bool {
		// CR and LF are the line breaks the file is made of; a tab is legal
		// inside a value. Everything else below a space is not text at all.
		return r == 0x7f || (r < 0x20 && r != '\r' && r != '\n' && r != '\t')
	}), "the file handed to the next client must be readable text")

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
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

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
	// Rejected is counted inside Failed, so it stays out of this sum. The rest
	// are the outcomes an event can have, and every VEVENT in the file has
	// exactly one of them.
	require.Equal(t, groups*5,
		res.Imported+res.Skipped+res.Failed+res.Duplicates+res.Truncated,
		"every VEVENT in the file must be accounted for exactly once")
	require.Equal(t, importReport{
		Imported: groups, Skipped: groups * 3, Failed: groups, Rejected: groups,
	}, res)

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, res.Imported,
		"the calendar must hold exactly what the response said it imported")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// Each event is its own transaction, so one the store refuses has to leave
// nothing at all: no row, and no entry in the log either. A refusal that
// still wrote its log entry would put a creation in the activity feed for an
// event that does not exist, which nobody could then open or delete.
//
// The events either side of it are already committed. That is the per-event
// contract working as intended, and the response counts all three.
func TestICalImportLeavesNoTraceOfAnEventItRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Refused mid-file")

	ics := wrap(
		vevent("UID:p1@example.com", "SUMMARY:First",
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z") +
			vevent("UID:p2@example.com", "SUMMARY:"+strings.Repeat("A", 200000),
				"DTSTART:20260601T120000Z", "DTEND:20260601T130000Z") +
			vevent("UID:p3@example.com", "SUMMARY:Third",
				"DTSTART:20260601T140000Z", "DTEND:20260601T150000Z"))
	require.Equal(t, importReport{Imported: 2, Failed: 1, Rejected: 1},
		importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 2)

	var feed struct {
		Items []struct {
			Action  string `json:"action"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=50", tt.AccessToken, nil, &feed)
	created := 0
	for _, i := range feed.Items {
		if i.Action == "calendar.event.created" {
			created++
		}
	}
	require.Equal(t, 2, created,
		"the refused event must not appear in the feed as something that happened")

	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// Every file Outlook writes names its zone the way Windows does, and those
// names mean nothing to the zone database. Read as UTC, the wall clock in the
// file becomes an instant nine hours from where its author put it -- reported
// as a clean import, with only the times wrong, which is the kind of wrong
// nobody checks for.
//
// The three below also settle what the name itself cannot say: "Standard Time"
// is part of the name all year, so two of these fall in the summer half of a
// zone that changes offset and must land on the summer one.
func TestICalImportPlacesAWindowsZoneNameWhereItsAuthorMeant(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, tzid, wantStart, wantZone string }{
		{"a zone that does not change offset", "Tokyo Standard Time",
			"2026-06-01T01:00:00Z", "Asia/Tokyo"},
		{"a zone on summer time in June", "W. Europe Standard Time",
			"2026-06-01T08:00:00Z", "Europe/Berlin"},
		{"a zone on daylight time in June", "Eastern Standard Time",
			"2026-06-01T14:00:00Z", "America/New_York"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Windows zone "+c.name)
			ics := wrap(vevent("UID:w@example.com", "SUMMARY:Outlook",
				"DTSTART;TZID="+c.tzid+":20260601T100000",
				"DTEND;TZID="+c.tzid+":20260601T110000"))
			require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics),
				"a name this common is not an unknown zone")

			listed := listHostile(t, calURL, tt.AccessToken)
			require.Len(t, listed, 1)
			require.Equal(t, c.wantStart, listed[0].StartAt,
				"10:00 written in %s is that instant, not the same digits in UTC", c.tzid)

			var evt struct {
				Timezone string `json:"timezone"`
			}
			helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
			require.Equal(t, c.wantZone, evt.Timezone,
				"the event keeps the zone its wall clock belongs to, so a later edit reads back the same")
			deleteEverything(t, calURL, tt.AccessToken, listed)
		})
	}
}

// A file naming a Windows zone almost always carries the VTIMEZONE component
// that defines it. Nothing here reads that component -- the offsets in it are
// driven by their own recurrence rules, and taking only the standard one would
// be wrong for half of every year in every zone that changes -- so what places
// the event is the name, and the presence of a definition must not disturb it.
func TestICalImportIgnoresAVTimezoneDefinitionWithoutBeingConfusedByIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "VTIMEZONE")

	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Tokyo Standard Time\r\n" +
		"BEGIN:STANDARD\r\nDTSTART:16010101T000000\r\n" +
		"TZOFFSETFROM:+0900\r\nTZOFFSETTO:+0900\r\nSUMMARY:Not an event\r\n" +
		"END:STANDARD\r\nEND:VTIMEZONE\r\n" +
		vevent("UID:vtz@example.com", "SUMMARY:Outlook",
			"DTSTART;TZID=Tokyo Standard Time:20260601T100000",
			"DTEND;TZID=Tokyo Standard Time:20260601T110000") +
		"END:VCALENDAR\r\n"
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics),
		"the definition is one component, not an event of its own")

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	require.Equal(t, "Outlook", listed[0].Title,
		"nothing inside the definition may reach the event")
	require.Equal(t, "2026-06-01T01:00:00Z", listed[0].StartAt)
	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// Who an event is for is dropped on the way in, and nothing says so.
//
// The half that is right: a file cannot name the owner. Whoever ran the
// import owns what it created, so an attacker cannot file events onto another
// member's layer by writing an ORGANIZER line.
//
// The half that is wrong: every ATTENDEE goes the same way, silently. A
// meeting imported from another calendar arrives with nobody invited to it,
// counted as a clean import, and the only way to find out is to open the event
// and notice who is missing.
func TestICalImportDropsOrganizerAndAttendees(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Attendees")

	ics := wrap(vevent("UID:at@example.com", "SUMMARY:Meeting",
		"ORGANIZER;CN=Someone Else:mailto:boss@example.com",
		"ATTENDEE;CN=A:mailto:a@example.com",
		"ATTENDEE;CN=B:mailto:b@example.com",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics),
		"nothing counts the people the file named and the import did not keep")

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)

	var evt struct {
		Participants []string `json:"participants"`
		Attendees    []any    `json:"attendees"`
		OwnerID      string   `json:"ownerId"`
		CreatedBy    string   `json:"createdBy"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Empty(t, evt.Participants)
	require.Empty(t, evt.Attendees)
	require.Equal(t, tt.UserID, evt.OwnerID,
		"the file must not be able to say whose layer its events sit on")
	require.Equal(t, tt.UserID, evt.CreatedBy)

	deleteEverything(t, calURL, tt.AccessToken, listed)
}

// Uploading the same file again is what a person does when they are not sure
// the first attempt worked, and it used to leave two copies of everything --
// both reported as clean imports, with nothing to say the calendar had seen
// them before.
//
// A recognised event is counted apart from the ones the parser could not use.
// A clean re-upload reporting "skipped: 40" reads as a failure to whoever just
// uploaded it; "already here: 40" is the same fact and the opposite feeling.
func TestICalImportRecognisesEventsTheCalendarAlreadyHas(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	t.Run("the same file twice leaves one copy", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Twice")
		file := wrap(oneGoodEvent())
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, file))
		require.Equal(t, importReport{Duplicates: 1}, importOK(t, calURL, tt.AccessToken, file),
			"the second import must recognise what the file called the event")

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Len(t, listed, 1, "the same file twice is still one event")
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})

	// A changed occurrence is written with an upsert, so it would apply itself
	// over the existing row while its series was left alone -- half the file
	// landing, which is worse than either taking all of it or none.
	t.Run("a series is recognised together with its changed occurrences", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Twice with an override")
		series := vevent("UID:series@example.com", "SUMMARY:Standup",
			"DTSTART:20260601T100000Z", "DTEND:20260601T101500Z", "RRULE:FREQ=DAILY;COUNT=3")
		moved := func(hour string) string {
			return vevent("UID:series@example.com", "SUMMARY:Standup moved",
				"RECURRENCE-ID:20260602T100000Z",
				"DTSTART:20260602T"+hour+"0000Z", "DTEND:20260602T"+hour+"1500Z")
		}
		require.Equal(t, importReport{Imported: 2},
			importOK(t, calURL, tt.AccessToken, wrap(series+moved("14"))))

		// The same series, with its changed occurrence somewhere else. Neither
		// half may be taken: the series is already here, so its departures from
		// it are too.
		require.Equal(t, importReport{Duplicates: 2},
			importOK(t, calURL, tt.AccessToken, wrap(series+moved("16"))))

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Equal(t, []string{
			"2026-06-01T10:00:00Z", "2026-06-02T14:00:00Z", "2026-06-03T10:00:00Z",
		}, startsOf(listed),
			"the occurrence must stay where the import that took it put it")
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})

	// Recognition rests on the UID, which is the only thing in the file that
	// names the event. Without one there is nothing to match, and the honest
	// answer is the duplicate it always was: an event that arrives twice can be
	// deleted, and one that never arrives cannot be got back.
	t.Run("an event the file does not name is imported again", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Twice unnamed")
		file := wrap(vevent("SUMMARY:Nameless",
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, file))
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, file),
			"nothing in the file identifies it, so nothing can recognise it")

		listed := listHostile(t, calURL, tt.AccessToken)
		require.Len(t, listed, 2)
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})

	// Deleting an event and importing the file again is how someone puts back
	// what they removed by mistake. Recognising a deleted row would make the
	// deletion permanent, from a screen that never said so.
	t.Run("an event deleted here can be imported again", func(t *testing.T) {
		calURL := hostileCalendar(t, tt, "Deleted then imported")
		file := wrap(oneGoodEvent())
		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, file))
		deleteEverything(t, calURL, tt.AccessToken, listHostile(t, calURL, tt.AccessToken))

		require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, file),
			"an event the calendar no longer holds is not one it already has")
		listed := listHostile(t, calURL, tt.AccessToken)
		require.Len(t, listed, 1)
		deleteEverything(t, calURL, tt.AccessToken, listed)
	})
}

// importedAlarmOffset imports one event carrying a single VALARM and returns
// the reminder it ended up with, or nil for none.
func importedAlarmOffset(t *testing.T, calURL, token, trigger string) *int {
	t.Helper()
	ics := wrap(vevent("UID:al@example.com", "SUMMARY:Alarmed",
		"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
		"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Ring",
		"TRIGGER:"+trigger, "END:VALARM"))
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, token, ics))

	listed := listHostile(t, calURL, token)
	require.Len(t, listed, 1)
	var evt struct {
		NotificationOffset *int `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, token, nil, &evt)
	return evt.NotificationOffset
}

// A reminder this product cannot show is left off rather than snapped to a
// neighbouring value, and a trigger that names an instant, fires after the
// start, or does not parse leaves the event with none. The event itself still
// imports: an alarm is not a reason to lose what it was attached to.
func TestICalImportKeepsOnlyAlarmsItCanShow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	fifteen := 15
	cases := []struct {
		name    string
		trigger string
		want    *int
	}{
		{"a supported offset", "-PT15M", &fifteen},
		{"an offset the picker cannot show", "-PT7M", nil},
		{"more minutes than an int can hold", "-PT99999999999999999999M", nil},
		{"a four-hundred-digit duration", "-P" + strings.Repeat("9", 400) + "D", nil},
		{"weeks combined with days, which the grammar forbids", "-P1W2D", nil},
		{"a fractional hour", "-PT0.5H", nil},
		{"units with no numbers", "PTMMM", nil},
		{"an empty trigger", "", nil},
		{"a trigger after the start", "PT15M", nil},
		{"an absolute trigger", "20260601T090000Z", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Alarm "+c.name)
			require.Equal(t, c.want, importedAlarmOffset(t, calURL, tt.AccessToken, c.trigger))
			deleteEverything(t, calURL, tt.AccessToken, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// A value that is not a duration leaves no reminder, on the parser's own
// stated terms: one that fires at the wrong time is worse than one the import
// did not take.
//
// A bare "P" names no duration at all, and a value repeating a unit is not one
// the grammar allows. Both used to produce an alarm -- at the moment the event
// starts, and at the sum of the repeated parts -- so a file that asked for no
// reminder rang one anyway.
func TestICalImportTakesNoReminderFromAValueThatIsNotADuration(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	cases := []struct{ name, trigger string }{
		{"a duration with no units at all", "P"},
		{"a duration with a time part but no units in it", "-PT"},
		// The parts were added together, and the sum was kept whenever it
		// happened to land on a value the picker can show -- five ones here.
		{"a duration repeating a unit", "-PT1M1M1M1M1M"},
		// The grammar orders the units it allows, and a value that names them
		// backwards is as much a typo as one that repeats them.
		{"a duration naming its units out of order", "-PT1S30M"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calURL := hostileCalendar(t, tt, "Not a duration "+c.name)
			require.Nil(t, importedAlarmOffset(t, calURL, tt.AccessToken, c.trigger),
				"a trigger that does not parse must leave no reminder")
			deleteEverything(t, calURL, tt.AccessToken, listHostile(t, calURL, tt.AccessToken))
		})
	}
}

// A VALARM carries property names the event uses. Read flat, its DESCRIPTION
// would land on the event as the note saying what the event is about --
// replacing whatever the file said. An alarm that is never closed keeps the
// parser inside it to the end of the event, which is the case where a flat
// read would show.
func TestICalImportDoesNotLetAnUnclosedAlarmOverwriteTheEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Unclosed alarm")

	ics := wrap("BEGIN:VEVENT\r\nUID:un@example.com\r\nSUMMARY:Unclosed alarm\r\n" +
		"DESCRIPTION:What the event is about\r\n" +
		"DTSTART:20260601T100000Z\r\nDTEND:20260601T110000Z\r\n" +
		"BEGIN:VALARM\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Ring the bell\r\nEND:VEVENT\r\n")
	require.Equal(t, importReport{Imported: 1}, importOK(t, calURL, tt.AccessToken, ics))

	listed := listHostile(t, calURL, tt.AccessToken)
	require.Len(t, listed, 1)
	var evt struct {
		Memo string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &evt)
	require.Equal(t, "What the event is about", evt.Memo,
		"the alarm's own description must not become the event's note")
	deleteEverything(t, calURL, tt.AccessToken, listed)
}
