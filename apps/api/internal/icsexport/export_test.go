package icsexport

import (
	"database/sql"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

func goldenID(n byte) []byte {
	return []byte{n, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, n}
}

func occurrence(row generated.CalendarEvent, start, end time.Time, masked bool) ical.Series {
	e := event(row, start, end, masked)
	return ical.Series{PublicID: row.PublicID, AllDay: row.AllDay, Timezone: row.Timezone, Master: &e}
}

// goldenCSVSeries are expanded occurrences, the shape the CSV export renders.
func goldenCSVSeries() []ical.Series {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	null := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	day := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	timed := time.Date(2026, 6, 1, 10, 0, 0, 0, tokyo)
	return []ical.Series{
		occurrence(generated.CalendarEvent{PublicID: goldenID(1), Title: "Holiday", AllDay: true, Timezone: "America/Los_Angeles"},
			day, day.AddDate(0, 0, 1), false),
		occurrence(generated.CalendarEvent{PublicID: goldenID(2), Title: "Trip", AllDay: true, Timezone: "Asia/Tokyo"},
			day, day.AddDate(0, 0, 3), false),
		occurrence(generated.CalendarEvent{PublicID: goldenID(3), Title: "Clinic, \"room\"", Timezone: "Asia/Tokyo",
			Location: null("東京都渋谷区"), Memo: null("line1\nline2"), URL: null("https://example.test/a,b")},
			timed.UTC(), timed.Add(90*time.Minute).UTC(), false),
		occurrence(generated.CalendarEvent{PublicID: goldenID(4), Title: "=SUM(A1)", Timezone: "UTC", Memo: null("-1\x1b")},
			timed.UTC(), timed.UTC(), false),
		occurrence(generated.CalendarEvent{PublicID: goldenID(5), Title: "Planning", Timezone: "Unknown/Zone",
			Location: null("Hidden"), Memo: null("hidden"), URL: null("https://hidden.test")},
			timed.UTC(), timed.Add(time.Hour).UTC(), true),
	}
}

func TestRenderCSVGolden(t *testing.T) {
	got := RenderCSV(goldenCSVSeries())
	path := filepath.Join("testdata", "export.csv")
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Errorf("export.csv differs from the golden file\n--- got ---\n%s", got)
	}
}

// A one-day all-day event reads as the same start and end date, since the
// stored end is the exclusive midnight after the last day.
func TestRenderCSVAllDayEndDateIsInclusive(t *testing.T) {
	start := time.Date(2025, 6, 24, 0, 0, 0, 0, time.UTC)
	out := string(RenderCSV([]ical.Series{occurrence(generated.CalendarEvent{
		Title: "Concert", AllDay: true, Timezone: "Asia/Tokyo",
	}, start, start.AddDate(0, 0, 1), false)}))
	if !strings.Contains(out, "Concert,2025-06-24,00:00:00,2025-06-24,00:00:00,true,") {
		t.Errorf("a one-day all-day event should read as 2025-06-24 to 2025-06-24, got:\n%s", out)
	}
	if strings.Contains(out, "2025-06-25") {
		t.Errorf("all-day end date must not show the exclusive midnight day:\n%s", out)
	}
}

func TestRenderCSVMaskedLeavesNoDetails(t *testing.T) {
	start := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	out := string(RenderCSV([]ical.Series{occurrence(generated.CalendarEvent{
		Title: "Planning", Timezone: "UTC",
		Location: sql.NullString{String: "Room 4", Valid: true},
		Memo:     sql.NullString{String: "agenda", Valid: true},
		URL:      sql.NullString{String: "https://example.test/x", Valid: true},
	}, start, start.Add(time.Hour), true)}))
	if strings.Contains(out, "Room 4") || strings.Contains(out, "agenda") || strings.Contains(out, "example.test") {
		t.Errorf("masked CSV leaks details:\n%s", out)
	}
}

func TestCSVEscapeNeutralizesFormulasAndControls(t *testing.T) {
	cases := map[string]string{
		`=HYPERLINK("http://x")`: `"'=HYPERLINK(""http://x"")"`,
		"+1":                     "'+1",
		"-2":                     "'-2",
		"@SUM(A1)":               "'@SUM(A1)",
		"\t=1":                   "'\t=1",
		"a\x1b[31mb":             "a[31mb",
		"a\rb":                   "ab",
		"a,b":                    `"a,b"`,
		"line1\nline2":           "\"line1\nline2\"",
		"plain":                  "plain",
	}
	for in, want := range cases {
		if got := csvEscape(in); got != want {
			t.Errorf("csvEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// A row's visibility and show-as map onto CLASS and TRANSP; the calendar
// default writes no CLASS, and only free is transparent.
func TestEventMapsTheRow(t *testing.T) {
	classes := map[generated.CalendarEventsVisibility]string{
		generated.CalendarEventsVisibilityDefault:      "",
		generated.CalendarEventsVisibilityPublic:       "PUBLIC",
		generated.CalendarEventsVisibilityPrivate:      "PRIVATE",
		generated.CalendarEventsVisibilityConfidential: "CONFIDENTIAL",
	}
	for v, want := range classes {
		if got := event(generated.CalendarEvent{Visibility: v}, time.Time{}, time.Time{}, false).Class; got != want {
			t.Errorf("class(%q) = %q, want %q", v, got, want)
		}
	}
	for _, s := range []generated.CalendarEventsShowAs{
		generated.CalendarEventsShowAsBusy, generated.CalendarEventsShowAsTentative, generated.CalendarEventsShowAsOof,
	} {
		if event(generated.CalendarEvent{ShowAs: s}, time.Time{}, time.Time{}, false).Transparent {
			t.Errorf("%q must read as taken", s)
		}
	}
	e := event(generated.CalendarEvent{
		ShowAs:             generated.CalendarEventsShowAsFree,
		NotificationOffset: sql.NullInt32{Int32: 15, Valid: true},
	}, time.Time{}, time.Time{}, false)
	if !e.Transparent || e.AlarmMinutes == nil || *e.AlarmMinutes != 15 {
		t.Errorf("event = %+v", e)
	}
}

// An override with attendee rows of its own replaces the series' attendees,
// even when none of them is enabled; one with none inherits them.
func TestAttendeesFollowTheReplacementRule(t *testing.T) {
	a := attendeeIndex{
		byEvent: map[uint32][]eventacl.Attendee{
			1: {{UserID: 10, Enabled: true}, {UserID: 11, Enabled: true}},
			2: {{UserID: 12, Enabled: true}},
		},
		withRows: map[uint32]bool{2: true, 3: true},
	}
	inherit := generated.CalendarEvent{ID: 4}
	replaced := generated.CalendarEvent{ID: 2}
	emptied := generated.CalendarEvent{ID: 3}
	if got := a.of(1, nil); len(got) != 2 {
		t.Errorf("the series keeps its own attendees, got %v", got)
	}
	if got := a.of(1, &inherit); len(got) != 2 {
		t.Errorf("an override with no rows inherits, got %v", got)
	}
	if got := a.of(1, &replaced); len(got) != 1 || got[0] != 12 {
		t.Errorf("an override with rows replaces, got %v", got)
	}
	if got := a.of(1, &emptied); len(got) != 0 {
		t.Errorf("an override whose rows are all disabled replaces with nobody, got %v", got)
	}
}

// A series renders the rows its reader may see, each judged on its own, and
// refuses once the bound is passed.
func TestBuilderJudgesEachRow(t *testing.T) {
	rule := json.RawMessage(`{"freq":"daily","interval":1}`)
	start := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	head := generated.CalendarEvent{
		ID: 1, PublicID: goldenID(1), Title: "Series", Timezone: "UTC", OwnerUserID: 7,
		Visibility:     generated.CalendarEventsVisibilityConfidential,
		StartAt:        sql.NullTime{Time: start, Valid: true},
		EndAt:          sql.NullTime{Time: start.Add(time.Hour), Valid: true},
		RecurrenceRule: &rule,
	}
	child := func(id uint32, day int, v generated.CalendarEventsVisibility) generated.CalendarEvent {
		at := start.AddDate(0, 0, day)
		return generated.CalendarEvent{
			ID: id, PublicID: goldenID(byte(id)), Title: "Changed", Timezone: "UTC", OwnerUserID: 7, Visibility: v,
			StartAt:                 sql.NullTime{Time: at.Add(time.Hour), Valid: true},
			EndAt:                   sql.NullTime{Time: at.Add(2 * time.Hour), Valid: true},
			RecurrenceOriginalStart: sql.NullTime{Time: at, Valid: true},
			RecurrenceParentID:      sql.NullInt32{Int32: 1, Valid: true},
		}
	}
	overrides := []generated.CalendarEvent{
		child(2, 1, generated.CalendarEventsVisibilityPublic),
		child(3, 2, generated.CalendarEventsVisibilityConfidential),
		child(4, 3, generated.CalendarEventsVisibilityPrivate),
	}
	b := &builder{viewer: eventacl.Reader{UserID: 8, IsMember: true}, attendees: attendeeIndex{}}
	s, ok, err := b.series(head, overrides, true)
	if err != nil || !ok {
		t.Fatalf("series = %v, %v", ok, err)
	}
	if s.Master != nil || len(s.Overrides) != 2 || s.Rule == nil {
		t.Fatalf("a member sees the public and private changes only: %+v", s)
	}
	if s.Overrides[0].Masked || !s.Overrides[1].Masked {
		t.Errorf("the private change is masked, the public one is not: %+v", s.Overrides)
	}
	if string(s.PublicID) != string(head.PublicID) {
		t.Error("changed occurrences are named by their series")
	}

	b = &builder{viewer: eventacl.Reader{UserID: 7, IsMember: true}, attendees: attendeeIndex{}, entries: MaxEntries - 3}
	if _, _, err := b.series(head, overrides, true); err != ErrTooLarge {
		t.Errorf("four rows past a bound of three: err = %v", err)
	}
}
