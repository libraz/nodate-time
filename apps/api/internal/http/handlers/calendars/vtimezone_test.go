package calendars

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// vtzOnset is one offset change a VTIMEZONE describes.
type vtzOnset struct {
	at time.Time
	to int
}

func parseICSOffset(t *testing.T, s string) int {
	t.Helper()
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[3:5])
	if err1 != nil || err2 != nil {
		t.Fatalf("bad offset %q", s)
	}
	return sign * (h*3600 + m*60)
}

// vtzOnsets reads every offset change the VTIMEZONE text names, expanding its
// RRULEs through lastYear, independently of the code that wrote it.
func vtzOnsets(t *testing.T, text string, lastYear int) []vtzOnset {
	t.Helper()
	var out []vtzOnset
	var start time.Time
	var from, to int
	var rrule string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n ", ""), "\r\n") {
		key, val, _ := strings.Cut(line, ":")
		switch key {
		case "BEGIN":
			start, from, to, rrule = time.Time{}, 0, 0, ""
		case "DTSTART":
			var err error
			start, err = time.Parse("20060102T150405", val)
			if err != nil {
				t.Fatalf("bad DTSTART %q", val)
			}
		case "TZOFFSETFROM":
			from = parseICSOffset(t, val)
		case "TZOFFSETTO":
			to = parseICSOffset(t, val)
		case "RRULE":
			rrule = val
		case "END":
			if val != "STANDARD" && val != "DAYLIGHT" {
				continue
			}
			local := []time.Time{start}
			if rrule != "" {
				local = nil
				for y := start.Year(); y <= lastYear; y++ {
					if d, ok := yearlyRuleDay(t, rrule, y); ok {
						local = append(local, time.Date(y, d.Month(), d.Day(),
							start.Hour(), start.Minute(), start.Second(), 0, time.UTC))
					}
				}
			}
			for _, l := range local {
				out = append(out, vtzOnset{at: l.Add(-time.Duration(from) * time.Second), to: to})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// yearlyRuleDay evaluates the yearly rule shapes the exporter writes, per RFC
// 5545: BYMONTH with an ordinal BYDAY (2SU, -1SU), or BYMONTH with a
// BYMONTHDAY list and a plain BYDAY.
func yearlyRuleDay(t *testing.T, rule string, year int) (time.Time, bool) {
	t.Helper()
	var month int
	var byDay string
	var monthDays []int
	for _, part := range strings.Split(rule, ";") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "BYMONTH":
			month, _ = strconv.Atoi(v)
		case "BYDAY":
			byDay = v
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				n, _ := strconv.Atoi(d)
				monthDays = append(monthDays, n)
			}
		}
	}
	code := strings.TrimLeft(byDay, "+-0123456789")
	wd := -1
	for i, c := range icsWeekdayCodes {
		if c == code {
			wd = i
		}
	}
	if month == 0 || wd < 0 {
		t.Fatalf("unexpected rule %q", rule)
	}
	var matches []time.Time
	for d := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC); d.Month() == time.Month(month); d = d.AddDate(0, 0, 1) {
		if int(d.Weekday()) != wd {
			continue
		}
		if monthDays != nil && !containsInt(monthDays, d.Day()) {
			continue
		}
		matches = append(matches, d)
	}
	if ord := byDay[:len(byDay)-len(code)]; ord != "" {
		n, _ := strconv.Atoi(ord)
		if n < 0 {
			n += len(matches) + 1
		}
		if n < 1 || n > len(matches) {
			return time.Time{}, false
		}
		return matches[n-1], true
	}
	if len(matches) != 1 {
		return time.Time{}, false
	}
	return matches[0], true
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// A zone whose rule is "weekday on or after day N" has to be carried past the
// scanned years with that rule, not with an ordinal that happened to fit the
// years scanned. Read back on its own terms, the VTIMEZONE has to give the tz
// database's offset for every instant, years after the export.
func TestVTimezoneCarriesOnOrAfterRulesPastTheScan(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for _, zone := range []string{"America/Santiago", "Asia/Jerusalem", "America/New_York", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Skipf("tz database unavailable: %v", err)
			}
			var b strings.Builder
			writeVTimezone(&b, zone, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), now)
			onsets := vtzOnsets(t, b.String(), 2031)
			for at := time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC); at.Year() < 2031; at = at.Add(6 * time.Hour) {
				got := 0
				found := false
				for _, o := range onsets {
					if o.at.After(at) {
						break
					}
					got, found = o.to, true
				}
				_, want := at.In(loc).Zone()
				if !found || got != want {
					t.Fatalf("%s: VTIMEZONE gives offset %d, tz database %d\n%s", at, got, want, b.String())
				}
			}
		})
	}
}
