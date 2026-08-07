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
			return clampEnd(t.Add(duration))
		}
		if t, err := time.ParseInLocation("2006-01-02", *rule.Until, eventStart.Location()); err == nil {
			return clampEnd(t.AddDate(0, 0, 1).Add(duration))
		}
	}
	if rule.Count > 0 {
		return clampEnd(computeNthOccurrence(rule, eventStart, rule.Count).Add(duration))
	}
	return farFutureEnd
}

// farFutureEnd is the boundary recorded for a series that does not end within
// reach. It is deliberately the same answer for two different questions.
//
// recurrence_end exists so a range query can skip series that cannot reach the
// window; it is not the series' last occurrence, which the expander works out
// from the rule. A series running to the year 300,000 and one running forever
// are therefore the same series to everything that reads this column, and
// answering both the same way is what lets the column stay storable.
var farFutureEnd = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

// clampEnd keeps a computed boundary inside what the column can hold.
//
// interval and count are validated independently -- up to 999 and up to 1000 --
// so their product is not. A yearly rule at both limits puts the last
// occurrence past the year 1,000,000, and DATETIME stops at 9999: the insert
// fails and a rule the validator had just accepted comes back as a server
// error. Weekly and monthly overflow too.
//
// Daily does not: 999,000 days is the year 4762, inside the column. That is
// worth knowing before choosing a case to test with, because the one
// frequency a reader is most likely to reach for is the one that hides this.
//
// Clamping does not narrow what the user asked for. The occurrences still come
// from the rule, and a window past the sentinel already finds no infinite
// series either, so this makes the two behave alike rather than making one of
// them fail.
func clampEnd(t time.Time) time.Time {
	if t.After(farFutureEnd) {
		return farFutureEnd
	}
	return t
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

// LoadLocation resolves an IANA timezone name, falling back to UTC for empty or
// unknown values so callers never have to nil-check.
//
// Anything that names an occurrence has to agree with the expander on which
// zone the recurrence lives in, so the resolution — fallback included — is
// shared rather than repeated.
func LoadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func loadLocation(tz string) *time.Location { return LoadLocation(tz) }

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
		// A series never begins before its own start. A rule naming a day the
		// anchor is already past — byMonthDay 5 on a start of the 25th, or the
		// first Monday of a month that began mid-month — walks the anchor's own
		// month first, and that candidate is not an occurrence: emitting it
		// invents an instance the user never scheduled and, worse, counts
		// against a `count` limit so the real last occurrence is dropped.
		if candidate.Before(eventStart) {
			continue
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
		// Check overlap with window. An occurrence with no duration is a marker
		// at a moment rather than an empty span, so it belongs to the window
		// that opens on it -- otherwise one landing exactly on a day boundary
		// is too late for the day that closes there and too early for the day
		// that opens there, and shows up in neither. This mirrors the same
		// allowance in ListCalendarEventsByCalendarAndRange, which the
		// non-recurring events go through.
		zeroLength := candidateEnd.Equal(candidate)
		if candidateEnd.After(windowStart) || (zeroLength && !candidate.Before(windowStart)) {
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
		// The same allowance the overlap check makes: an occurrence with no
		// duration is a point, so one landing exactly on the window start is
		// inside the window. Skipping it here would step the iterator past it
		// before the overlap check ever saw it, which is a harder failure to
		// find than the check being wrong -- the occurrence is not rejected,
		// it is never offered.
		if candidate.Add(duration).After(windowStart) ||
			(duration == 0 && !candidate.Before(windowStart)) {
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

// weekdayByCode maps the two-letter day codes a rule carries onto weekdays.
// It is fixed, so it is built once rather than per candidate: the iterators
// below are called once for every date the expander steps through, and a
// window of a year over a daily series steps through a few hundred.
var weekdayByCode = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
	"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// nextWeeklyByDay iterates weekly byDay occurrences. The week starts on Sunday
// (WKST=SU): with interval > 1, the skipped weeks are counted from each Sunday
// boundary. This intentionally diverges from the RFC 5545 default of WKST=MO
// and is part of the documented API contract for recurrence rules.
func (it *iterator) nextWeeklyByDay() time.Time {
	targetDays := make(map[time.Weekday]bool)
	for _, d := range it.rule.ByDay {
		if wd, ok := weekdayByCode[d]; ok {
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
	targetDay, ok := weekdayByCode[it.rule.ByDay[0]]
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
	found := 0
	for scanned := 0; found < n && scanned < maxExpansionIterations; scanned++ {
		t := it.next()
		if t.IsZero() {
			break
		}
		// Skip the same pre-start candidates Expand skips, or the boundary
		// this computes lands one occurrence short of where the series
		// actually ends and range queries stop returning it.
		if t.Before(start) {
			continue
		}
		last = t
		found++
	}
	return last
}

// Exceptions is the set of occurrences a recurring series skips.
//
// The shared contract allows exactly two ways to depart from a rule, and
// this is the one for cancelling: the start the occurrence would have had
// is listed here, and the expander drops it. Changing an occurrence is the
// other way, and it is a row -- never an entry here.
//
// Storing a cancellation both ways would give a consumer two places to
// look before it could say whether an occurrence happens, and the two
// answers drift apart the first time a writer updates one and not the
// other.
type Exceptions []time.Time

// ParseExceptions reads the stored exclusion list. Entries that are not
// valid timestamps are dropped rather than failing the read: an unreadable
// entry cannot identify an occurrence to skip, and refusing to render the
// series because of one would hide every occurrence instead of one.
func ParseExceptions(data *json.RawMessage) Exceptions {
	if data == nil || len(*data) == 0 || string(*data) == "null" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal(*data, &raw); err != nil {
		return nil
	}
	out := make(Exceptions, 0, len(raw))
	for _, s := range raw {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			out = append(out, t.UTC())
		}
	}
	return out
}

// Contains reports whether the given occurrence start is excluded.
//
// Comparison is by instant, not by calendar day: an all-day series and a
// timed one both anchor on an exact start, and matching on the date alone
// would cancel the wrong occurrence for any rule that fires more than once
// a day.
func (e Exceptions) Contains(start time.Time) bool {
	target := start.UTC().UnixMilli()
	for _, ex := range e {
		if ex.UnixMilli() == target {
			return true
		}
	}
	return false
}

// With returns the list with start added, unchanged if it is already
// present. Cancelling the same occurrence twice must not grow the column
// without bound.
func (e Exceptions) With(start time.Time) Exceptions {
	if e.Contains(start) {
		return e
	}
	return append(append(Exceptions{}, e...), start.UTC())
}

// Without returns the list with start removed, which is what restoring a
// cancelled occurrence does.
func (e Exceptions) Without(start time.Time) Exceptions {
	target := start.UTC().UnixMilli()
	out := make(Exceptions, 0, len(e))
	for _, ex := range e {
		if ex.UnixMilli() != target {
			out = append(out, ex)
		}
	}
	return out
}

// MarshalColumn renders the list for storage. An empty list is stored as
// NULL rather than "[]" so "this series has no exclusions" has one
// representation instead of two.
func (e Exceptions) MarshalColumn() (*json.RawMessage, error) {
	if len(e) == 0 {
		return nil, nil
	}
	raw := make([]string, 0, len(e))
	for _, ex := range e {
		raw = append(raw, ex.UTC().Format(time.RFC3339))
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	msg := json.RawMessage(body)
	return &msg, nil
}
