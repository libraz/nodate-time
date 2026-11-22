package calendars

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestExportDispositionIsASCIIAndCarriesTheName(t *testing.T) {
	for name, want := range map[string]string{
		"家族 予定":     "家族_予定.ics",
		"a\r\nb\"c": "a__b_c.ics",
		"":          "calendar.ics",
	} {
		got := exportDisposition(name, "ics")
		for _, r := range got {
			if r < 0x20 || r > 0x7e {
				t.Fatalf("header for %q is not printable ASCII: %q", name, got)
			}
		}
		i := strings.Index(got, "filename*=UTF-8''")
		if i < 0 {
			t.Fatalf("header for %q has no filename*: %q", name, got)
		}
		decoded, err := url.PathUnescape(got[i+len("filename*=UTF-8''"):])
		if err != nil || decoded != want {
			t.Errorf("filename* for %q decodes to %q (%v), want %q", name, decoded, err, want)
		}
		if !strings.Contains(got, `filename="`) {
			t.Errorf("header for %q has no ASCII fallback: %q", name, got)
		}
	}
}

// from/to are calendar days in the reader's zone, and the inclusive end is
// the end of that local day even when it is 25 hours long.
func TestExportWindowIsLocalDays(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	w, err := parseExportWindow("2026-10-01", "2026-10-31", tokyo)
	if err != nil {
		t.Fatal(err)
	}
	if !w.start.Equal(time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)) ||
		!w.end.Equal(time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("window = %s..%s, want Tokyo days", w.start.UTC(), w.end.UTC())
	}
	if got := w.header(); got != "2026-10-01/2026-11-01" {
		t.Errorf("header = %q", got)
	}

	ny, _ := time.LoadLocation("America/New_York")
	w, err = parseExportWindow("2026-11-01", "2026-11-01", ny)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.end.Sub(w.start); got != 25*time.Hour {
		t.Errorf("the day DST ends spans %s, want 25h", got)
	}
}
