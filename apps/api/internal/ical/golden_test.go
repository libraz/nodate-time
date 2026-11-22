package ical

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

var goldenNow = time.Date(2026, 11, 22, 3, 4, 5, 0, time.UTC)

func goldenID(n byte) []byte {
	return []byte{n, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, n}
}

func goldenCompare(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
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
		t.Errorf("%s differs from the golden file\n--- got ---\n%s", name, got)
	}
}

func minutes(m int32) *int32 { return &m }

// goldenSeries covers every shape the renderer writes: all-day, zoned, a
// tentative UTC event, a zoned series with a moved and an all-day changed
// occurrence and every stored EXDATE form, an all-day series with a timed
// changed occurrence, masked details, folding of long and non-ASCII lines,
// and a series rendered from its changed occurrence alone.
func goldenSeries() []Series {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	ny, _ := time.LoadLocation("America/New_York")

	allDayStart := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	zonedStart := time.Date(2026, 6, 1, 10, 0, 0, 0, tokyo)
	utcStart := time.Date(2026, 3, 2, 15, 30, 0, 0, time.UTC)
	seriesStart := time.Date(2026, 3, 2, 9, 0, 0, 0, ny)
	movedOriginal := time.Date(2026, 3, 9, 9, 0, 0, 0, ny)
	offsiteOriginal := time.Date(2026, 3, 16, 9, 0, 0, 0, ny)
	binsStart := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	maskedStart := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	longStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	orphanOriginal := time.Date(2026, 4, 6, 9, 0, 0, 0, tokyo)

	return []Series{
		{
			PublicID: goldenID(1), AllDay: true, Timezone: "America/Los_Angeles",
			Master: &Event{
				Title: "Holiday", AllDay: true, Timezone: "America/Los_Angeles",
				Start: allDayStart, End: allDayStart.AddDate(0, 0, 1), Class: "PUBLIC", Transparent: true,
			},
		},
		{
			PublicID: goldenID(2), Timezone: "Asia/Tokyo",
			Master: &Event{
				Title: "Clinic, room; 4", Timezone: "Asia/Tokyo",
				Location: "東京都渋谷区", Description: "line1\r\nline2\\end", URL: "https://example.test/a,b;c",
				Start: zonedStart.UTC(), End: zonedStart.Add(90 * time.Minute).UTC(),
				Class: "PRIVATE", AlarmMinutes: minutes(30),
			},
		},
		{
			PublicID: goldenID(3), Timezone: "UTC",
			Master: &Event{
				Title: "UTC call", Timezone: "UTC", Start: utcStart, End: utcStart.Add(time.Hour),
				Class: "CONFIDENTIAL", AlarmMinutes: minutes(0), Tentative: true,
			},
		},
		{
			PublicID: goldenID(4), Timezone: "America/New_York",
			Rule: &recurrence.Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}},
			Exdates: exceptionsOf(
				time.Date(2026, 3, 23, 9, 0, 0, 0, ny).UTC().Format(time.RFC3339),
				"2026-03-30",
				"2026-04-06T09:00:00",
				"unreadable",
			),
			Master: &Event{
				Title: "Standup", Timezone: "America/New_York",
				Start: seriesStart.UTC(), End: seriesStart.Add(15 * time.Minute).UTC(), AlarmMinutes: minutes(1440),
			},
			Overrides: []Event{
				{
					Title: "Standup (moved)", Location: "Room 7", Timezone: "America/New_York",
					Start: movedOriginal.Add(2 * time.Hour).UTC(), End: movedOriginal.Add(3 * time.Hour).UTC(),
					OriginalStart: movedOriginal.UTC(), AlarmMinutes: minutes(1440),
				},
				{
					Title: "Standup off-site", AllDay: true, Timezone: "America/New_York",
					Start: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC),
					OriginalStart: offsiteOriginal.UTC(), AlarmMinutes: minutes(1440),
				},
			},
		},
		{
			PublicID: goldenID(5), AllDay: true, Timezone: "Asia/Tokyo",
			Rule:    &recurrence.Rule{Freq: "weekly", Interval: 2, ByDay: []string{"MO"}, Count: 10},
			Exdates: exceptionsOf("2026-01-19", "2026-02-02T00:00:00Z"),
			Master: &Event{
				Title: "Bins", AllDay: true, Timezone: "Asia/Tokyo",
				Start: binsStart, End: binsStart.AddDate(0, 0, 1),
			},
			Overrides: []Event{{
				Title: "Bins (late)", Timezone: "Asia/Tokyo",
				Start: time.Date(2026, 2, 16, 9, 0, 0, 0, tokyo).UTC(), End: time.Date(2026, 2, 16, 10, 0, 0, 0, tokyo).UTC(),
				OriginalStart: time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC),
			}},
		},
		{
			PublicID: goldenID(6), Timezone: "Europe/London",
			Master: &Event{
				Title: "Planning", Timezone: "Europe/London",
				Location: "Hidden room", Description: "hidden memo", URL: "https://hidden.test",
				Start: maskedStart, End: maskedStart.Add(time.Hour), Class: "PUBLIC", Masked: true,
			},
		},
		{
			PublicID: goldenID(7), Timezone: "UTC",
			Master: &Event{
				Title: strings.Repeat("長い題名🙂", 12), Timezone: "UTC",
				Description: strings.Repeat("a", 70) + " " + strings.Repeat("b", 90) + "\x1b[31m",
				Start:       longStart, End: longStart.Add(time.Hour),
			},
		},
		{
			PublicID: goldenID(8), Timezone: "Asia/Tokyo",
			Overrides: []Event{{
				Title: "Visible change", Timezone: "Asia/Tokyo",
				Start: orphanOriginal.Add(time.Hour).UTC(), End: orphanOriginal.Add(2 * time.Hour).UTC(),
				OriginalStart: orphanOriginal.UTC(),
			}},
		},
	}
}

func TestRenderGolden(t *testing.T) {
	out := Render(goldenSeries(), RenderOptions{
		Now: goldenNow, AppName: "Nodate Time; \"beta\"", CalendarName: "家族の予定, 2026",
	})
	goldenCompare(t, "export.ics", out)
}

func TestRenderGoldenEmpty(t *testing.T) {
	out := Render(nil, RenderOptions{Now: goldenNow, AppName: "Nodate Time", CalendarName: "Empty"})
	goldenCompare(t, "export-empty.ics", out)
}
