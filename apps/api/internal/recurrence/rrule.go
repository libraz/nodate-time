package recurrence

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// The RRULE codec is the only place a rule crosses the iCalendar boundary.
//
// Expand clamps a day of the month the month does not have to its last day,
// where RFC 5545 skips the month. FormatRRule writes such a rule in the RFC
// form that clamps the same way, and ParseRRule maps that form back while
// refusing the plain one, so an occurrence set never changes on the way
// through in either direction.

// ParseRRule maps an RFC 5545 RRULE value onto the rule Expand reproduces
// exactly, or reports false for a rule it would expand to other dates.
//
// start is DTSTART in the event's own location: an UNTIL without Z is read
// there, and its weekday and day of month take part in the checks.
func ParseRRule(value string, start time.Time) (*Rule, bool) {
	rule := Rule{Interval: 1}
	wkst := "MO" // the RFC default
	var byDay, byMonthDay []string
	setPos, byMonth := 0, 0
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, false
		}
		switch strings.ToUpper(key) {
		case "FREQ":
			switch f := strings.ToLower(val); f {
			case "daily", "weekly", "monthly", "yearly":
				rule.Freq = f
			default:
				return nil, false
			}
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 999 {
				return nil, false
			}
			rule.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 1000 {
				return nil, false
			}
			rule.Count = n
		case "UNTIL":
			until, ok := parseUntil(val, start.Location())
			if !ok {
				return nil, false
			}
			rule.Until = &until
		case "BYDAY":
			byDay = splitList(val)
		case "BYMONTHDAY":
			byMonthDay = splitList(val)
		case "BYSETPOS":
			n, err := strconv.Atoi(val)
			if err != nil || n == 0 || n < -1 || n > 5 {
				return nil, false
			}
			setPos = n
		case "BYMONTH":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 12 {
				return nil, false
			}
			byMonth = n
		case "WKST":
			if val != "" {
				wkst = strings.ToUpper(val)
			}
		default:
			return nil, false
		}
	}
	if rule.Freq == "" || !rule.readByDay(byDay, &setPos) ||
		!rule.readMonthDays(byMonthDay, setPos, byMonth, start) {
		return nil, false
	}
	if !weekStartAgrees(rule, wkst, start.Weekday()) || rule.Validate(start) != nil {
		return nil, false
	}
	return &rule, true
}

func splitList(val string) []string {
	out := strings.Split(val, ",")
	for i := range out {
		out[i] = strings.ToUpper(strings.TrimSpace(out[i]))
	}
	return out
}

// readByDay takes BYDAY. An ordinal entry such as 2MO is the Nth weekday of
// the month, which is one weekday with a set position; it is consumed from
// setPos's place so the two encodings land on the same rule.
func (r *Rule) readByDay(byDay []string, setPos *int) bool {
	for _, d := range byDay {
		code := strings.TrimLeft(d, "+-0123456789")
		if _, ok := weekdayByCode[code]; !ok {
			return false
		}
		if code == d {
			r.ByDay = append(r.ByDay, code)
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(d[:len(d)-len(code)], "+"))
		if err != nil || n < 1 || n > 5 || r.Freq != "monthly" || len(byDay) != 1 || *setPos != 0 {
			return false
		}
		r.ByDay = []string{code}
		r.BySetPos = n
		return true
	}
	if *setPos > 0 {
		r.BySetPos = *setPos
		*setPos = 0
	}
	return true
}

// readMonthDays takes BYMONTHDAY, a negative BYSETPOS and BYMONTH, which are
// accepted only in the shapes FormatRRule writes for a clamped day and as a
// plain day every month has. A day from 29 on, named outright or implied by
// DTSTART, is skipped by the RFC in short months and clamped by Expand.
func (r *Rule) readMonthDays(byMonthDay []string, setPos, byMonth int, start time.Time) bool {
	days := make([]int, 0, len(byMonthDay))
	for _, v := range byMonthDay {
		n, err := strconv.Atoi(v)
		if err != nil {
			return false
		}
		days = append(days, n)
	}
	sort.Ints(days)
	clamped := 0
	switch {
	case len(days) == 1 && days[0] == -1 && setPos == 0:
		clamped = 31
	case len(days) >= 2 && setPos == -1 && days[0] == 28 && days[len(days)-1] <= 31:
		for i, d := range days {
			if d != 28+i {
				return false
			}
		}
		clamped = days[len(days)-1]
	case setPos != 0:
		return false
	}
	switch r.Freq {
	case "monthly":
		switch {
		case byMonth != 0:
			return false
		case clamped != 0:
			r.ByMonthDay = clamped
		case len(days) == 1 && days[0] >= 1 && days[0] <= 28:
			r.ByMonthDay = days[0]
		case len(days) > 0:
			return false
		case len(r.ByDay) == 0 && start.Day() > 28:
			return false
		}
	case "yearly":
		leapDay := start.Month() == time.February && start.Day() == 29
		if leapDay {
			return byMonth == 2 && clamped >= 29
		}
		return byMonth == 0 && len(days) == 0
	default:
		return byMonth == 0 && len(days) == 0
	}
	return true
}

// parseUntil maps an RRULE UNTIL value onto the rule's until: a date stays a
// date, an instant becomes RFC 3339, and a local time is read in loc.
func parseUntil(val string, loc *time.Location) (string, bool) {
	if t, err := time.Parse("20060102T150405Z", val); err == nil {
		return t.UTC().Format(time.RFC3339), true
	}
	if t, err := time.ParseInLocation("20060102T150405", val, loc); err == nil {
		return t.Format(time.RFC3339), true
	}
	if _, err := time.Parse("20060102", val); err == nil {
		return val[:4] + "-" + val[4:6] + "-" + val[6:8], true
	}
	return "", false
}

// weekStartAgrees reports whether a rule expands the same under week start
// wkst as under the expander's Sunday. Only a weekly rule with an interval
// and BYDAY groups days into weeks; the grouping differs exactly when the
// days in play -- BYDAY and DTSTART's own -- fall on both sides of the
// stretch from Sunday up to wkst.
func weekStartAgrees(rule Rule, wkst string, startDay time.Weekday) bool {
	if wkst == "SU" || rule.Freq != "weekly" || rule.Interval <= 1 || len(rule.ByDay) == 0 {
		return true
	}
	w, ok := weekdayByCode[wkst]
	if !ok {
		return false
	}
	inside, outside := false, false
	mark := func(d time.Weekday) {
		if d < w {
			inside = true
		} else {
			outside = true
		}
	}
	mark(startDay)
	for _, code := range rule.ByDay {
		if d, ok := weekdayByCode[code]; ok {
			mark(d)
		}
	}
	return !(inside && outside)
}

// FormatRRule renders the rule as an RFC 5545 RRULE value that expands from
// start to the dates Expand produces. start is DTSTART in the event's own
// location; allDay selects the value type UNTIL is written in.
//
// WKST is always written out: the expander is fixed to a Sunday week start,
// which differs from the RFC default, and a weekly rule with an interval
// would land on different days in a client that assumed Monday.
func FormatRRule(r *Rule, start time.Time, allDay bool) string {
	if r == nil || r.Freq == "" {
		return ""
	}
	parts := []string{"FREQ=" + strings.ToUpper(r.Freq)}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	switch {
	case r.Freq == "monthly" && r.BySetPos > 0 && len(r.ByDay) > 0:
		parts = append(parts, "BYDAY="+strings.Join(r.ByDay, ","), "BYSETPOS="+strconv.Itoa(r.BySetPos))
	case r.Freq == "monthly":
		day := r.ByMonthDay
		if day == 0 {
			day = start.Day()
		}
		switch {
		case day == 31:
			parts = append(parts, "BYMONTHDAY=-1")
		case day > 28:
			parts = append(parts, "BYMONTHDAY="+clampedDays(day), "BYSETPOS=-1")
		case r.ByMonthDay > 0:
			parts = append(parts, "BYMONTHDAY="+strconv.Itoa(r.ByMonthDay))
		}
	case r.Freq == "yearly" && start.Month() == time.February && start.Day() == 29:
		parts = append(parts, "BYMONTH=2", "BYMONTHDAY="+clampedDays(29), "BYSETPOS=-1")
	case len(r.ByDay) > 0:
		parts = append(parts, "BYDAY="+strings.Join(r.ByDay, ","))
	}
	count, until := r.Count, r.Until
	if count > 0 && until != nil {
		// A rule stored before both were refused: name the one Expand stops at.
		if u, ok := untilInstant(*until, start.Location()); ok && computeNthOccurrence(r, start, count).After(u) {
			count = 0
		} else {
			until = nil
		}
	}
	if count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(count))
	}
	if u := formatUntil(until, start.Location(), allDay); u != "" {
		parts = append(parts, "UNTIL="+u)
	}
	parts = append(parts, "WKST=SU")
	return strings.Join(parts, ";")
}

// clampedDays lists 28 through day, which with BYSETPOS=-1 picks day or the
// month's last day, whichever comes first.
func clampedDays(day int) string {
	days := make([]string, 0, day-27)
	for d := 28; d <= day; d++ {
		days = append(days, strconv.Itoa(d))
	}
	return strings.Join(days, ",")
}

// formatUntil renders the end boundary in the value type DTSTART uses: a date
// for an all-day series, a UTC instant for a timed one. Mixing the two is what
// makes some clients drop the boundary and repeat forever.
func formatUntil(until *string, loc *time.Location, allDay bool) string {
	if until == nil || *until == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, *until); err == nil {
		if allDay {
			return t.In(loc).Format("20060102")
		}
		return t.UTC().Format("20060102T150405Z")
	}
	if t, err := time.ParseInLocation("2006-01-02", *until, loc); err == nil {
		if allDay {
			return t.Format("20060102")
		}
		return t.AddDate(0, 0, 1).Add(-time.Second).UTC().Format("20060102T150405Z")
	}
	return ""
}
