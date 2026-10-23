package calendars

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// zoneTransition is one change of a zone's UTC offset.
type zoneTransition struct {
	at       time.Time
	from, to int
	name     string
	dst      bool
}

// onset is the transition's local start, read in the offset it replaces,
// which is how a VTIMEZONE DTSTART names it.
func (t zoneTransition) onset() time.Time {
	return t.at.Add(time.Duration(t.from) * time.Second).UTC()
}

// writeVTimezone writes the VTIMEZONE a TZID refers to, covering from onward.
//
// The tz database has no public form of its rules, so they are recovered from
// the offsets themselves: transitions are found by scanning, and the yearly
// pattern the most recent years share becomes an RRULE that carries the zone
// past the scanned range. Transitions before that pattern took hold are
// written one by one.
func writeVTimezone(b *strings.Builder, zone string, from, now time.Time) {
	loc := loadLocationOrUTC(zone)
	scanStart := time.Date(from.UTC().Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
	lastYear := now.UTC().Year() + 2 + ruleSampleYears
	scanEnd := time.Date(lastYear+1, 1, 1, 0, 0, 0, 0, time.UTC)
	transitions := findTransitions(loc, scanStart, scanEnd)

	writeFolded(b, "BEGIN:VTIMEZONE")
	writeFolded(b, "TZID:"+zone)
	name, off := scanStart.In(loc).Zone()
	writeZoneComponent(b, zoneTransition{
		at: scanStart, from: off, to: off, name: name, dst: scanStart.In(loc).IsDST(),
	}, "")

	ruleFrom, rules := yearlyRules(transitions, lastYear)
	for _, t := range transitions {
		if t.onset().Year() >= ruleFrom {
			break
		}
		writeZoneComponent(b, t, "")
	}
	j := 0
	for _, t := range transitions {
		if t.onset().Year() == ruleFrom {
			writeZoneComponent(b, t, rules[j])
			j++
		}
	}
	writeFolded(b, "END:VTIMEZONE")
}

func writeZoneComponent(b *strings.Builder, t zoneTransition, rrule string) {
	kind := "STANDARD"
	if t.dst {
		kind = "DAYLIGHT"
	}
	writeFolded(b, "BEGIN:"+kind)
	writeFolded(b, "DTSTART:"+t.onset().Format("20060102T150405"))
	writeFolded(b, "TZOFFSETFROM:"+icsOffset(t.from))
	writeFolded(b, "TZOFFSETTO:"+icsOffset(t.to))
	if t.name != "" {
		writeFolded(b, "TZNAME:"+icsEscape(t.name))
	}
	if rrule != "" {
		writeFolded(b, "RRULE:"+rrule)
	}
	writeFolded(b, "END:"+kind)
}

// icsOffset renders a UTC offset in seconds as RFC 5545 utc-offset.
func icsOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	s := fmt.Sprintf("%s%02d%02d", sign, seconds/3600, seconds/60%60)
	if seconds%60 != 0 {
		s += fmt.Sprintf("%02d", seconds%60)
	}
	return s
}

// findTransitions lists the offset changes in [start, end). It steps a day at
// a time and bisects to the second, so two changes within one day are missed;
// no zone in use has those.
func findTransitions(loc *time.Location, start, end time.Time) []zoneTransition {
	var out []zoneTransition
	state := func(t time.Time) (int, bool) {
		lt := t.In(loc)
		_, off := lt.Zone()
		return off, lt.IsDST()
	}
	prev := start
	prevOff, prevDST := state(prev)
	for prev.Before(end) {
		next := prev.Add(24 * time.Hour)
		off, dst := state(next)
		if off != prevOff || dst != prevDST {
			lo, hi := prev, next
			for hi.Sub(lo) > time.Second {
				mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Second)
				if mid.Equal(lo) {
					mid = lo.Add(time.Second)
				}
				if o, d := state(mid); o == prevOff && d == prevDST {
					lo = mid
				} else {
					hi = mid
				}
			}
			name, _ := hi.In(loc).Zone()
			out = append(out, zoneTransition{at: hi, from: prevOff, to: off, name: name, dst: dst})
			prevOff, prevDST = off, dst
		}
		prev = next
	}
	return out
}

// ruleSampleYears is how far past the near future the yearly pattern is read.
// The tz database projects a zone's current rule forward, and twelve years
// put a fixed date on every weekday, which is what tells "the first Sunday on
// or after the 2nd" apart from "the first Sunday".
const ruleSampleYears = 12

var icsWeekdayCodes = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// transitionRule renders the yearly rule a transition follows, counted from
// the start of its month (2SU) or, when last is set, from the end (-1SU).
func transitionRule(t zoneTransition, last bool) string {
	on := t.onset()
	ord := (on.Day()-1)/7 + 1
	if last {
		daysInMonth := time.Date(on.Year(), on.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		ord = -((daysInMonth-on.Day())/7 + 1)
	}
	return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(on.Month()), ord, icsWeekdayCodes[on.Weekday()])
}

// sameShape reports whether two transitions differ only in their year.
func sameShape(a, b zoneTransition, last bool) bool {
	ao, bo := a.onset(), b.onset()
	return a.from == b.from && a.to == b.to && a.name == b.name && a.dst == b.dst &&
		ao.Hour() == bo.Hour() && ao.Minute() == bo.Minute() && ao.Second() == bo.Second() &&
		transitionRule(a, last) == transitionRule(b, last)
}

// yearlyRules finds the earliest year from which every year through lastYear
// repeats lastYear's transitions, and the RRULE for each of them. A zone with
// no transitions in lastYear, or none that repeat, has no rule: its last
// written transition then holds from there on.
func yearlyRules(transitions []zoneTransition, lastYear int) (int, []string) {
	byYear := map[int][]zoneTransition{}
	for _, t := range transitions {
		y := t.onset().Year()
		byYear[y] = append(byYear[y], t)
	}
	ref := byYear[lastYear]
	if len(ref) == 0 {
		return lastYear + 1, nil
	}
	rules := make([]string, len(ref))
	from := 0
	for i, r := range ref {
		best, bestRule := lastYear+1, ""
		for _, last := range []bool{false, true} {
			y := lastYear
			for {
				ts := byYear[y-1]
				if len(ts) != len(ref) || !sameShape(ts[i], r, last) {
					break
				}
				y--
			}
			if y < best {
				best, bestRule = y, transitionRule(r, last)
			}
		}
		if y, rule := onOrAfterRule(byYear, ref, i, lastYear); y < best {
			best, bestRule = y, rule
		}
		rules[i] = bestRule
		if best > from {
			from = best
		}
	}
	return from, rules
}

// onOrAfterRule follows ref[i] back while each year's transition falls on the
// same weekday of the same month inside one seven-day stretch, the shape of a
// tz rule such as "Sunday on or after the 2nd", and renders that stretch as
// the RFC 5545 on-or-after form. An ordinal rule is this form too, so it only
// wins where no ordinal fits as many years.
func onOrAfterRule(byYear map[int][]zoneTransition, ref []zoneTransition, i, lastYear int) (int, string) {
	r := ref[i]
	ro := r.onset()
	lo, hi := ro.Day(), ro.Day()
	y := lastYear
	for {
		ts := byYear[y-1]
		if len(ts) != len(ref) {
			break
		}
		t, o := ts[i], ts[i].onset()
		if t.from != r.from || t.to != r.to || t.name != r.name || t.dst != r.dst ||
			o.Month() != ro.Month() || o.Weekday() != ro.Weekday() ||
			o.Hour() != ro.Hour() || o.Minute() != ro.Minute() || o.Second() != ro.Second() {
			break
		}
		if max(hi, o.Day())-min(lo, o.Day()) > 6 {
			break
		}
		lo, hi = min(lo, o.Day()), max(hi, o.Day())
		y--
	}
	// The stretch has to fit the month in every year, February included.
	if lo+6 > time.Date(2023, ro.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return lastYear + 1, ""
	}
	days := make([]string, 7)
	for d := range days {
		days[d] = strconv.Itoa(lo + d)
	}
	return y, fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYMONTHDAY=%s;BYDAY=%s",
		int(ro.Month()), strings.Join(days, ","), icsWeekdayCodes[ro.Weekday()])
}
