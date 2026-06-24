package recurrence

import (
	"encoding/json"
	"time"
)

// Rule represents a recurrence pattern for calendar events.
type Rule struct {
	Freq       string   `json:"freq"`                 // daily, weekly, monthly, yearly
	Interval   int      `json:"interval"`             // repeat every N freq units (1-99)
	ByDay      []string `json:"byDay,omitempty"`      // MO,TU,WE,TH,FR,SA,SU
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
// used for efficient SQL range queries.
func ComputeEnd(rule *Rule, eventStart time.Time) time.Time {
	if rule == nil {
		return eventStart
	}
	if rule.Until != nil {
		if t, err := time.Parse(time.RFC3339, *rule.Until); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", *rule.Until); err == nil {
			return t.Add(24 * time.Hour)
		}
	}
	if rule.Count > 0 {
		return computeNthOccurrence(rule, eventStart, rule.Count)
	}
	// Infinite recurrence: use far-future sentinel
	return time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
}

// ComputeEndInZone is ComputeEnd anchored in the event's IANA timezone, with the
// result normalized to UTC. Used to populate the recurrence_end column so SQL
// range queries select the right master events regardless of DST.
func ComputeEndInZone(rule *Rule, eventStart time.Time, tz string) time.Time {
	loc := loadLocation(tz)
	return ComputeEnd(rule, eventStart.In(loc)).UTC()
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
	count := 0
	hardCap := rule.Count
	if hardCap <= 0 || hardCap > maxExpansionIterations {
		hardCap = maxExpansionIterations // safety limit for unbounded recurrence
	}

	var untilTime time.Time
	if rule.Until != nil {
		if t, err := time.Parse(time.RFC3339, *rule.Until); err == nil {
			untilTime = t
		} else if t, err := time.Parse("2006-01-02", *rule.Until); err == nil {
			untilTime = t.Add(24 * time.Hour)
		}
	}

	iterator := newIterator(rule, eventStart)
	iterations := 0
	for iterations < maxExpansionIterations {
		iterations++
		candidate := iterator.next()
		if candidate.IsZero() {
			break
		}

		// Check until boundary
		if !untilTime.IsZero() && candidate.After(untilTime) {
			break
		}

		// Past the query window
		if candidate.After(windowEnd) || candidate.Equal(windowEnd) {
			break
		}

		count++
		if count > hardCap {
			break
		}

		candidateEnd := candidate.Add(duration)
		// Check overlap with window
		if candidateEnd.After(windowStart) {
			results = append(results, Occurrence{StartAt: candidate, EndAt: candidateEnd})
		}
	}

	return results
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
			candidate = it.start.AddDate(it.step*it.rule.Interval, 0, 0)
			it.step++

		default:
			return time.Time{}
		}

		return candidate
	}
}

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
	monthStart := time.Date(base.Year(), base.Month(), 1, base.Hour(), base.Minute(), base.Second(), 0, base.Location())
	monthStart = monthStart.AddDate(0, it.step*it.rule.Interval, 0)
	it.step++

	return nthWeekdayOfMonth(monthStart.Year(), monthStart.Month(), targetDay, it.rule.BySetPos, base.Location(), base.Hour(), base.Minute(), base.Second())
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
	return last.Add(24 * time.Hour) // buffer for end-of-day
}
