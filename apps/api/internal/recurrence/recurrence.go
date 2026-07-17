package recurrence

import (
	"encoding/json"
	"time"
)

// Rule represents a recurrence pattern for calendar events.
type Rule struct {
	Freq       string   `json:"freq"`                 // daily, weekly, monthly, yearly
	Interval   int      `json:"interval"`             // repeat every N freq units (1-99)
	ByDay      []string `json:"byDay,omitempty"`      // MO,TU,WE,TH,FR,SA,SU (weeks start on Sunday: WKST=SU)
	ByMonthDay int      `json:"byMonthDay,omitempty"` // 1-31
	BySetPos   int      `json:"bySetPos,omitempty"`   // Nth weekday of month (1-5)
	Until      *string  `json:"until,omitempty"`      // ISO 8601 date string
	Count      int      `json:"count,omitempty"`      // max occurrences (1-365)
}

// Occurrence represents a single expanded instance of a recurring event.
type Occurrence struct {
	StartAt time.Time
	EndAt   time.Time
}

// ParseRule parses a JSON recurrence rule. Returns nil if data is nil or null.
func ParseRule(data json.RawMessage) *Rule {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var r Rule
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}
	if r.Interval < 1 {
		r.Interval = 1
	}
	return &r
}

// ComputeEnd calculates the effective end date for a recurrence rule,
// used for efficient SQL range queries. eventEnd is the master event's end,
// so the returned boundary covers the full duration of the last occurrence
// (an event longer than a day would otherwise drop out of window queries).
// A date-only until is interpreted as end-of-day in eventStart's location.
func ComputeEnd(rule *Rule, eventStart, eventEnd time.Time) time.Time {
	if rule == nil {
		return eventEnd
	}
	duration := eventEnd.Sub(eventStart)
	if duration < 0 {
		duration = 0
	}
	if rule.Until != nil {
		if t, err := time.Parse(time.RFC3339, *rule.Until); err == nil {
			return t.Add(duration)
		}
		if t, err := time.ParseInLocation("2006-01-02", *rule.Until, eventStart.Location()); err == nil {
			return t.AddDate(0, 0, 1).Add(duration)
		}
	}
	if rule.Count > 0 {
		return computeNthOccurrence(rule, eventStart, rule.Count).Add(duration)
	}
	// Infinite recurrence: use far-future sentinel
	return time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
}

// ComputeEndInZone is ComputeEnd anchored in the event's IANA timezone, with the
// result normalized to UTC. Used to populate the recurrence_end column so SQL
// range queries select the right master events regardless of DST.
func ComputeEndInZone(rule *Rule, eventStart, eventEnd time.Time, tz string) time.Time {
	loc := loadLocation(tz)
	return ComputeEnd(rule, eventStart.In(loc), eventEnd.In(loc)).UTC()
}

// maxExpansionIterations bounds how many candidates Expand will ever step
// through, protecting against pathological rules (e.g. daily forever queried
// over a decade-wide window) regardless of the requested window size.
const maxExpansionIterations = 10000

// loadLocation resolves an IANA timezone name, falling back to UTC for empty or
// unknown values so callers never have to nil-check.
func loadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

// ExpandInZone expands occurrences while anchoring the recurrence in the event's
// IANA timezone, so daily/weekly/monthly steps preserve the wall-clock time
// across DST transitions. The returned occurrences are normalized back to UTC
// for storage and serialization. tz may be empty (treated as UTC).
func ExpandInZone(rule *Rule, eventStart, eventEnd, windowStart, windowEnd time.Time, tz string) []Occurrence {
	loc := loadLocation(tz)
	occ := Expand(rule, eventStart.In(loc), eventEnd.In(loc), windowStart, windowEnd)
	for i := range occ {
		occ[i].StartAt = occ[i].StartAt.UTC()
		occ[i].EndAt = occ[i].EndAt.UTC()
	}
	return occ
}

// Expand generates all occurrences of a recurring event within the given window.
// The first occurrence is at eventStart. Duration is preserved from the original
// event. Recurrence math is performed in eventStart's location, so callers that
// need DST-correct wall-clock behavior should pass eventStart in the event's
// timezone (see ExpandInZone).
func Expand(rule *Rule, eventStart, eventEnd, windowStart, windowEnd time.Time) []Occurrence {
	if rule == nil {
		return nil
	}

	duration := eventEnd.Sub(eventStart)
	var results []Occurrence

	// All-day events are stored as [midnight, midnight) wall-clock in the
	// event's location. Track the calendar-day span instead of the absolute
	// duration so a DST transition does not stretch an occurrence into an
	// extra day (a 23h spring-forward day plus fixed 24h would end at 01:00).
	allDaySpan := 0
	if isWallMidnight(eventStart) && isWallMidnight(eventEnd) && eventEnd.After(eventStart) {
		allDaySpan = calendarDaySpan(eventStart, eventEnd)
	}

	var untilTime time.Time
	if rule.Until != nil {
		if t, err := time.Parse(time.RFC3339, *rule.Until); err == nil {
			untilTime = t
		} else if t, err := time.ParseInLocation("2006-01-02", *rule.Until, eventStart.Location()); err == nil {
			// Date-only until means "through the end of that day" in the
			// event's own location, not a UTC instant.
			untilTime = t.AddDate(0, 0, 1).Add(-time.Second)
		}
	}

	iterator := newIterator(rule, eventStart)
	occurrenceOrdinal := fastForwardInitialStep(rule, eventStart, duration, windowStart)
	iterator.step = occurrenceOrdinal
	emittedCandidates := 0
	scannedCandidates := 0
	for scannedCandidates < maxExpansionIterations && emittedCandidates < maxExpansionIterations {
		scannedCandidates++
		candidate := iterator.next()
		if candidate.IsZero() {
			break
		}
		occurrenceOrdinal++

		// Check until boundary
		if !untilTime.IsZero() && candidate.After(untilTime) {
			break
		}

		// Past the query window
		if candidate.After(windowEnd) || candidate.Equal(windowEnd) {
			break
		}

		if rule.Count > 0 && occurrenceOrdinal > rule.Count {
			break
		}
		emittedCandidates++

		candidateEnd := candidate.Add(duration)
		if allDaySpan > 0 {
			candidateEnd = candidate.AddDate(0, 0, allDaySpan)
		}
		// Check overlap with window
		if candidateEnd.After(windowStart) {
			results = append(results, Occurrence{StartAt: candidate, EndAt: candidateEnd})
		}
	}

	return results
}

// isWallMidnight reports whether t sits exactly at wall-clock midnight in its
// own location.
func isWallMidnight(t time.Time) bool {
	return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}

// calendarDaySpan counts calendar days between two wall-clock midnights in the
// same location, tolerating DST offsets that make a day 23 or 25 hours long.
func calendarDaySpan(start, end time.Time) int {
	days := int((end.Sub(start) + 12*time.Hour) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	return days
}

func fastForwardInitialStep(rule *Rule, eventStart time.Time, duration time.Duration, windowStart time.Time) int {
	if rule.Freq != "daily" || rule.Interval <= 0 || !windowStart.After(eventStart) {
		return 0
	}

	approxDays := int(windowStart.Sub(eventStart.Add(duration)).Hours() / 24)
	step := approxDays / rule.Interval
	if step < 0 {
		step = 0
	}

	for step > 0 {
		prev := eventStart.AddDate(0, 0, (step-1)*rule.Interval)
		if !prev.Add(duration).After(windowStart) {
			break
		}
		step--
	}
	for {
		candidate := eventStart.AddDate(0, 0, step*rule.Interval)
		if candidate.Add(duration).After(windowStart) {
			break
		}
		step++
	}
	return step
}

type iterator struct {
	rule    *Rule
	start   time.Time
	current time.Time
	step    int
	started bool
}

func newIterator(rule *Rule, start time.Time) *iterator {
	return &iterator{rule: rule, start: start, current: start, step: 0}
}

func (it *iterator) next() time.Time {
	for {
		var candidate time.Time

		switch it.rule.Freq {
		case "daily":
			candidate = it.start.AddDate(0, 0, it.step*it.rule.Interval)
			it.step++

		case "weekly":
			if len(it.rule.ByDay) == 0 {
				candidate = it.start.AddDate(0, 0, it.step*7*it.rule.Interval)
				it.step++
			} else {
				candidate = it.nextWeeklyByDay()
				if candidate.IsZero() {
					return time.Time{}
				}
			}

		case "monthly":
			if it.rule.BySetPos > 0 && len(it.rule.ByDay) > 0 {
				candidate = it.nextMonthlyBySetPos()
			} else if it.rule.ByMonthDay > 0 {
				candidate = it.nextMonthlyByDay()
			} else {
				candidate = it.nextMonthlyByDay()
			}
			if candidate.IsZero() {
				return time.Time{}
			}

		case "yearly":
			candidate = addYearsClamped(it.start, it.step*it.rule.Interval)
			it.step++

		default:
			return time.Time{}
		}

		return candidate
	}
}

// addYearsClamped advances t by the given number of years, clamping the day to
// the last day of the month so a Feb 29 anchor lands on Feb 28 in non-leap
// years instead of rolling over to Mar 1 (mirrors the monthly clamping).
func addYearsClamped(t time.Time, years int) time.Time {
	year := t.Year() + years
	day := t.Day()
	lastDay := time.Date(year, t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, t.Month(), day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// nextWeeklyByDay iterates weekly byDay occurrences. The week starts on Sunday
// (WKST=SU): with interval > 1, the skipped weeks are counted from each Sunday
// boundary. This intentionally diverges from the RFC 5545 default of WKST=MO
// and is part of the documented API contract for recurrence rules.
func (it *iterator) nextWeeklyByDay() time.Time {
	dayMap := map[string]time.Weekday{
		"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
		"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
	}

	targetDays := make(map[time.Weekday]bool)
	for _, d := range it.rule.ByDay {
		if wd, ok := dayMap[d]; ok {
			targetDays[wd] = true
		}
	}
	if len(targetDays) == 0 {
		return time.Time{}
	}

	if !it.started {
		it.started = true
		it.current = it.start
		if targetDays[it.current.Weekday()] {
			return it.current
		}
	}

	// Find the week start (Sunday) of the current position
	for i := 0; i < 1000; i++ {
		it.current = it.current.AddDate(0, 0, 1)

		// When we cross into a new week (Sunday), apply the interval
		if it.current.Weekday() == time.Sunday {
			if it.rule.Interval > 1 {
				it.current = it.current.AddDate(0, 0, (it.rule.Interval-1)*7)
			}
		}

		if targetDays[it.current.Weekday()] {
			return it.current
		}
	}
	return time.Time{}
}

func (it *iterator) nextMonthlyByDay() time.Time {
	day := it.rule.ByMonthDay
	if day == 0 {
		day = it.start.Day()
	}

	base := it.start
	candidate := time.Date(base.Year(), base.Month(), 1, base.Hour(), base.Minute(), base.Second(), 0, base.Location())
	candidate = candidate.AddDate(0, it.step*it.rule.Interval, 0)
	it.step++

	// Clamp to last day of month
	lastDay := time.Date(candidate.Year(), candidate.Month()+1, 0, 0, 0, 0, 0, candidate.Location()).Day()
	d := day
	if d > lastDay {
		d = lastDay
	}
	return time.Date(candidate.Year(), candidate.Month(), d, base.Hour(), base.Minute(), base.Second(), 0, base.Location())
}

func (it *iterator) nextMonthlyBySetPos() time.Time {
	dayMap := map[string]time.Weekday{
		"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
		"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
	}

	targetDay, ok := dayMap[it.rule.ByDay[0]]
	if !ok {
		return time.Time{}
	}

	base := it.start
	for skipped := 0; skipped < maxExpansionIterations; skipped++ {
		monthStart := time.Date(base.Year(), base.Month(), 1, base.Hour(), base.Minute(), base.Second(), 0, base.Location())
		monthStart = monthStart.AddDate(0, it.step*it.rule.Interval, 0)
		it.step++

		candidate := nthWeekdayOfMonth(monthStart.Year(), monthStart.Month(), targetDay, it.rule.BySetPos, base.Location(), base.Hour(), base.Minute(), base.Second())
		if !candidate.IsZero() {
			return candidate
		}
	}
	return time.Time{}
}

func nthWeekdayOfMonth(year int, month time.Month, weekday time.Weekday, n int, loc *time.Location, hour, min, sec int) time.Time {
	first := time.Date(year, month, 1, hour, min, sec, 0, loc)
	// Find the first occurrence of the target weekday
	offset := int(weekday) - int(first.Weekday())
	if offset < 0 {
		offset += 7
	}
	firstOccurrence := first.AddDate(0, 0, offset)

	// Get the Nth occurrence
	result := firstOccurrence.AddDate(0, 0, (n-1)*7)

	// Verify it's still in the same month
	if result.Month() != month {
		return time.Time{}
	}
	return result
}

// computeNthOccurrence returns the start of the nth occurrence; callers add the
// event duration to obtain the series end boundary.
func computeNthOccurrence(rule *Rule, start time.Time, n int) time.Time {
	last := start
	r := *rule
	r.Count = 0
	r.Until = nil
	it := newIterator(&r, start)
	for i := 0; i < n; i++ {
		t := it.next()
		if t.IsZero() {
			break
		}
		last = t
	}
	return last
}
