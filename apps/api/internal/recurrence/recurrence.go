package recurrence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Rule represents a recurrence pattern for calendar events, in the canonical
// shape sql/core/PROTOCOL.md §Recurrence defines. The bounds and field
// combinations stated here are the ones Validate enforces; every field of a
// rule that passes it changes what Expand produces.
type Rule struct {
	Freq       string   `json:"freq"`                 // daily, weekly, monthly, yearly
	Interval   int      `json:"interval"`             // repeat every N freq units (1-999)
	ByDay      []string `json:"byDay,omitempty"`      // weekly: set of MO..SU; monthly: exactly one, with bySetPos
	ByMonthDay []int    `json:"byMonthDay,omitempty"` // monthly only: days 1-31, each clamped to the month's last day
	BySetPos   []int    `json:"bySetPos,omitempty"`   // monthly only: one position (1-5 or -1) of byDay within the month
	Wkst       string   `json:"wkst,omitempty"`       // week start, MO or SU; absent reads as MO. Matters for weekly with interval > 1
	Until      *string  `json:"until,omitempty"`      // RFC 3339 instant, or a date meaning the end of that day in the event's zone; not with count
	Count      int      `json:"count,omitempty"`      // max occurrences, the first included (1-1000; 0 means unbounded); not with until
}

// ErrInvalidRule is a rule the expander would not honour as written.
var ErrInvalidRule = errors.New("recurrence: invalid rule")

// Validate reports whether every field of the rule is one Expand honours.
// eventStart is the series' first occurrence, in the event's own location: it
// is always occurrence #1, so the only series-level refusal is an until that
// ends before it.
func (r *Rule) Validate(eventStart time.Time) error {
	switch r.Freq {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return ErrInvalidRule
	}
	if r.Interval < 1 || r.Interval > 999 || r.Count < 0 || r.Count > 1000 {
		return ErrInvalidRule
	}
	if len(r.ByDay) > 7 || len(r.ByMonthDay) > 31 || len(r.BySetPos) > 1 {
		return ErrInvalidRule
	}
	if hasRepeat(r.ByDay) || hasRepeat(r.ByMonthDay) {
		return ErrInvalidRule
	}
	for _, d := range r.ByDay {
		if _, ok := weekdayByCode[d]; !ok {
			return ErrInvalidRule
		}
	}
	for _, d := range r.ByMonthDay {
		if d < 1 || d > 31 {
			return ErrInvalidRule
		}
	}
	for _, p := range r.BySetPos {
		if p != -1 && (p < 1 || p > 5) {
			return ErrInvalidRule
		}
	}
	if r.Wkst != "" && r.Wkst != "MO" && r.Wkst != "SU" {
		return ErrInvalidRule
	}
	if len(r.BySetPos) > 0 && len(r.ByDay) == 0 {
		return ErrInvalidRule
	}
	switch r.Freq {
	case "daily", "yearly":
		if len(r.ByDay) > 0 || len(r.ByMonthDay) > 0 || len(r.BySetPos) > 0 {
			return ErrInvalidRule
		}
	case "weekly":
		if len(r.ByMonthDay) > 0 || len(r.BySetPos) > 0 {
			return ErrInvalidRule
		}
	case "monthly":
		// Either the Nth weekday (one byDay with bySetPos) or days of the month.
		if len(r.ByDay) > 0 && (len(r.BySetPos) == 0 || len(r.ByDay) != 1 || len(r.ByMonthDay) > 0) {
			return ErrInvalidRule
		}
	}
	// RFC 5545 allows one end condition per rule, and Expand would stop at
	// whichever came first while an export could only name one of them.
	if r.Count > 0 && r.Until != nil {
		return ErrInvalidRule
	}
	if r.Until != nil {
		until, ok := untilInstant(*r.Until, eventStart.Location())
		if !ok || eventStart.After(until) {
			return ErrInvalidRule
		}
	}
	return nil
}

func hasRepeat[T comparable](values []T) bool {
	seen := make(map[T]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

// untilInstant reads an until as the last instant the series may start at.
func untilInstant(until string, loc *time.Location) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, until); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02", until, loc); err == nil {
		// Date-only until means "through the end of that day" in the
		// event's own location, not a UTC instant.
		return t.AddDate(0, 0, 1).Add(-time.Second), true
	}
	return time.Time{}, false
}

// OccursAt reports whether the series produces an occurrence starting exactly
// at the given instant, anchored in tz the way ExpandInZone is.
func OccursAt(rule *Rule, eventStart, eventEnd time.Time, tz string, at time.Time) bool {
	for _, occ := range ExpandInZone(rule, eventStart, eventEnd, at, at.Add(time.Millisecond), tz) {
		if occ.StartAt.Equal(at) {
			return true
		}
	}
	return false
}

// Occurrence represents a single expanded instance of a recurring event.
type Occurrence struct {
	StartAt time.Time
	EndAt   time.Time
}

// ParseRule parses a JSON recurrence rule. It returns (nil, nil) if data is
// empty or null, and an error for anything that is not a canonical rule
// object: a key outside the grammar, a null value, a scalar where an array
// belongs, or a type mismatch. An absent interval reads as 1; an interval
// that is present is kept as written so Validate can refuse a bad one.
func ParseRule(data json.RawMessage) (*Rule, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("recurrence: rule is not an object: %w", err)
	}
	for key, value := range members {
		if string(value) == "null" {
			return nil, fmt.Errorf("recurrence: %s must not be null", key)
		}
	}
	var r Rule
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("recurrence: decode rule: %w", err)
	}
	if _, ok := members["interval"]; !ok {
		r.Interval = 1
	}
	return &r, nil
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

// maxExpansionIterations bounds how many occurrences one Expand call produces,
// protecting against pathological rules (e.g. daily forever queried over a
// decade-wide window) regardless of the requested window size.
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
		untilTime, _ = untilInstant(*rule.Until, eventStart.Location())
	}

	iterator := newIterator(rule, eventStart)
	occurrenceOrdinal := fastForward(rule, iterator, eventStart, duration, windowStart)
	// The cap bounds the occurrences one window can produce. Candidates before
	// the window are bounded separately: fast-forward leaves only a handful,
	// except under a count limit, which bounds them by itself.
	inWindow, beforeWindow := 0, 0
	for inWindow < maxExpansionIterations && beforeWindow < maxExpansionIterations {
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
			inWindow++
		} else {
			beforeWindow++
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

// fastForward positions the iterator just before the window, so the work
// Expand does follows the window rather than the age of the series. It
// returns how many occurrences it skipped. Only daily steps map one-to-one
// onto occurrences, so other frequencies skip only when no count limit needs
// that number -- and a count limit bounds the walk on its own.
func fastForward(rule *Rule, it *iterator, eventStart time.Time, duration time.Duration, windowStart time.Time) int {
	if rule.Freq == "daily" {
		it.step = fastForwardInitialStep(rule, eventStart, duration, windowStart)
		return it.step
	}
	if rule.Count > 0 || !windowStart.After(eventStart) {
		return 0
	}
	// Nothing starting before this can reach the window, whatever a DST
	// transition or an all-day span does to the duration.
	horizon := windowStart.Add(-duration - 48*time.Hour)
	fastForwardPeriods(it, horizon)
	return 0
}

// maxPeriodDays is the longest a period of each frequency can be, so a step
// count estimated from it never overshoots.
var maxPeriodDays = map[string]int{"weekly": 7, "monthly": 31, "yearly": 366}

// fastForwardPeriods skips the periods that lie wholly before horizon: the
// iterator resumes at the last period whose start is not after it.
func fastForwardPeriods(it *iterator, horizon time.Time) {
	interval := it.interval()
	span := maxPeriodDays[it.rule.Freq] * interval
	if span <= 0 {
		return
	}
	step := int(horizon.Sub(it.x.periodStart(0)).Hours()/24) / span
	if step < 0 {
		step = 0
	}
	for !it.x.periodStart((step + 1) * interval).After(horizon) {
		step++
	}
	it.step = step
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

// iterator yields a rule's occurrences in order, one period of the rule's
// frequency at a time. The start is always the first occurrence, whether or
// not the rule would produce it.
type iterator struct {
	rule *Rule
	x    expansion
	step int // index of the next period to read, in units of interval
	buf  []time.Time
	pos  int
}

func newIterator(rule *Rule, start time.Time) *iterator {
	return &iterator{rule: rule, x: newExpansion(rule, start)}
}

func (it *iterator) interval() int {
	if it.rule.Interval < 1 {
		return 1
	}
	return it.rule.Interval
}

// next returns the next occurrence start, or the zero time when the rule
// yields no more within the scan bound.
func (it *iterator) next() time.Time {
	for empty := 0; it.pos >= len(it.buf); empty++ {
		if empty >= maxExpansionIterations {
			return time.Time{}
		}
		candidates := it.x.period(it.step * it.interval())
		if it.step == 0 {
			// The start is occurrence #1 and counts toward count; the rule's
			// own candidates in its first period only count if they follow it.
			kept := []time.Time{it.x.anchor}
			for _, c := range candidates {
				if c.After(it.x.anchor) {
					kept = append(kept, c)
				}
			}
			candidates = kept
		}
		it.step++
		it.buf, it.pos = candidates, 0
	}
	t := it.buf[it.pos]
	it.pos++
	return t
}

// weekdayByCode maps the two-letter day codes a rule carries onto weekdays.
var weekdayByCode = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
	"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// expansion is a rule resolved against its anchor: the weekday codes decoded
// and the anchor's week located under wkst.
type expansion struct {
	freq       string
	anchor     time.Time
	byDay      []time.Weekday
	byMonthDay []int
	bySetPos   []int
	// weekStart is local midnight on the first day of the anchor's week, the
	// week counted from wkst (MO when absent).
	weekStart time.Time
}

func newExpansion(r *Rule, anchor time.Time) expansion {
	wkst := time.Monday
	if r.Wkst == "SU" {
		wkst = time.Sunday
	}
	var byDay []time.Weekday
	for _, code := range r.ByDay {
		if d, ok := weekdayByCode[code]; ok {
			byDay = append(byDay, d)
		}
	}
	back := (int(anchor.Weekday()) - int(wkst) + 7) % 7
	y, m, d := anchor.Date()
	return expansion{
		freq:       r.Freq,
		anchor:     anchor,
		byDay:      byDay,
		byMonthDay: r.ByMonthDay,
		bySetPos:   r.BySetPos,
		weekStart:  time.Date(y, m, d-back, 0, 0, 0, 0, anchor.Location()),
	}
}

// period holds the occurrences of the period `offset` freq-units after the
// anchor's, in order. Every occurrence is computed from the anchor, never
// from the previous one, so each clamps independently: Jan 31, Feb 28,
// Mar 31 rather than a series pinned to the 28th.
func (x expansion) period(offset int) []time.Time {
	switch {
	case x.freq == "weekly" && len(x.byDay) > 0:
		return x.weekPeriod(offset)
	case x.freq == "monthly" && (len(x.byMonthDay) > 0 || len(x.byDay) > 0):
		return x.monthPeriod(offset)
	}
	return []time.Time{x.stepFromAnchor(offset)}
}

// periodStart is the earliest instant the period `offset` units after the
// anchor's can hold, and increases with offset.
func (x expansion) periodStart(offset int) time.Time {
	switch {
	case x.freq == "weekly" && len(x.byDay) > 0:
		y, m, d := x.weekStart.Date()
		return time.Date(y, m, d+offset*7, 0, 0, 0, 0, x.weekStart.Location())
	case x.freq == "monthly" && (len(x.byMonthDay) > 0 || len(x.byDay) > 0):
		year, month := monthAt(x.anchor, offset)
		return time.Date(year, month, 1, 0, 0, 0, 0, x.anchor.Location())
	}
	return x.stepFromAnchor(offset)
}

func (x expansion) stepFromAnchor(offset int) time.Time {
	switch x.freq {
	case "daily":
		return addDaysWallClock(x.anchor, offset)
	case "weekly":
		return addDaysWallClock(x.anchor, offset*7)
	case "monthly":
		return addMonthsClamped(x.anchor, offset)
	}
	return addMonthsClamped(x.anchor, offset*12)
}

// weekPeriod holds the listed weekdays of the week `offset` weeks after the
// anchor's, in the order wkst puts them.
func (x expansion) weekPeriod(offset int) []time.Time {
	y, m, d := x.weekStart.Date()
	var out []time.Time
	for i := range 7 {
		day := atAnchorClock(x.anchor, y, m, d+offset*7+i)
		for _, wd := range x.byDay {
			if day.Weekday() == wd {
				out = append(out, day)
				break
			}
		}
	}
	return out
}

// monthPeriod holds the days of the month `offset` months after the anchor's
// that the rule selects. A byMonthDay the month does not have is clamped to
// its last day; a bySetPos position it does not have selects nothing.
func (x expansion) monthPeriod(offset int) []time.Time {
	year, month := monthAt(x.anchor, offset)
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	var days []int
	if len(x.byMonthDay) > 0 {
		seen := map[int]bool{}
		for _, md := range x.byMonthDay {
			day := min(md, last)
			if !seen[day] {
				seen[day] = true
				days = append(days, day)
			}
		}
		sort.Ints(days)
	} else {
		for day := 1; day <= last; day++ {
			days = append(days, day)
		}
	}
	if len(x.byDay) > 0 {
		kept := days[:0:0]
		for _, day := range days {
			wd := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday()
			for _, want := range x.byDay {
				if wd == want {
					kept = append(kept, day)
					break
				}
			}
		}
		days = kept
	}
	if len(x.bySetPos) > 0 {
		days = selectPosition(days, x.bySetPos[0])
	}

	out := make([]time.Time, 0, len(days))
	for _, day := range days {
		out = append(out, atAnchorClock(x.anchor, year, month, day))
	}
	return out
}

// selectPosition picks the 1-based position from the start of set, or from
// its end when negative. A position the set does not reach selects nothing.
func selectPosition(set []int, pos int) []int {
	i := pos - 1
	if pos < 0 {
		i = len(set) + pos
	}
	if i < 0 || i >= len(set) {
		return nil
	}
	return []int{set[i]}
}

// monthAt is the calendar month `offset` months after t's.
func monthAt(t time.Time, offset int) (int, time.Month) {
	total := int(t.Month()) - 1 + offset
	return t.Year() + floorDiv(total, 12), time.Month(floorMod(total, 12) + 1)
}

// atAnchorClock is the given local day at the anchor's wall-clock time.
func atAnchorClock(anchor time.Time, year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
}

// addDaysWallClock advances the wall clock by whole days in the event's own
// location, so a 09:00 meeting stays at 09:00 across a DST change.
func addDaysWallClock(t time.Time, days int) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+days, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// addMonthsClamped advances by whole months, clamping the day to the length
// of the target month. AddDate would roll Jan 31 plus one month over to
// Mar 3 and skip February.
func addMonthsClamped(t time.Time, months int) time.Time {
	year, month := monthAt(t, months)
	d := min(t.Day(), time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day())
	return time.Date(year, month, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int) int { return a - floorDiv(a, b)*b }

// computeNthOccurrence returns the start of the nth occurrence; callers add the
// event duration to obtain the series end boundary.
func computeNthOccurrence(rule *Rule, start time.Time, n int) time.Time {
	last := start
	r := *rule
	r.Count = 0
	r.Until = nil
	it := newIterator(&r, start)
	for found := 0; found < n; found++ {
		t := it.next()
		if t.IsZero() {
			break
		}
		last = t
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
