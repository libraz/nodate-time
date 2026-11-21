package calendars

import (
	"strconv"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
)

type rawEvent struct {
	uid      string
	summary  string
	location string
	desc     string
	url      string
	// class is the CLASS property, which carries the same axis as an event's
	// visibility. An unrecognised value imports as the calendar default.
	class string
	// transp is the TRANSP property, the free/busy half of the availability
	// axis.
	transp string
	start  time.Time
	end    time.Time
	allDay bool
	tzid   string
	// tzUnknown records that a dated property named a zone nothing here could
	// resolve, so its wall clock was read as UTC. The event still imports --
	// there is nothing better to do with it -- but the import says how many it
	// placed that way, because the times are the only thing wrong and nobody
	// checks those.
	tzUnknown bool
	rrule     string
	// exdates are the occurrences the series cancels, distinct and at most
	// calimport.MaxExdates of them; tooManyExdates records that the file named more.
	exdates        []time.Time
	tooManyExdates bool
	// duration is the DURATION property, the alternative to DTEND.
	// badDuration records one that is present but not a duration.
	duration    time.Duration
	hasDuration bool
	badDuration bool
	// recurrenceID names the occurrence this entry replaces; when set the
	// entry is a changed occurrence of the series sharing its uid, not an
	// event of its own.
	recurrenceID    time.Time
	hasRecurrenceID bool
	// alarmMinutes is how long before the start the event's reminder fires,
	// taken from the first VALARM the event can express.
	alarmMinutes *int32
}

// noteUnknownZone records a TZID this server could not resolve. A property
// with no TZID at all is a floating time rather than a zone that failed, so
// there is nothing to report about it.
func (e *rawEvent) noteUnknownZone(tzid string) {
	if tzid == "" {
		return
	}
	if _, ok := resolveTZID(tzid); !ok {
		e.tzUnknown = true
	}
}

// resolveTZID maps a TZID from a file onto an IANA zone name, which is the
// only kind this server can look up. A Windows name -- what Outlook and
// everything speaking to Exchange write -- is mapped onto its IANA equivalent
// rather than being read as UTC.
//
// ok is false when the name means nothing here. The caller then reads the
// value as UTC, which is the only fallback available and the wrong instant for
// every zone that is not UTC, so the import counts how often it had to.
func resolveTZID(tzid string) (string, bool) {
	name := strings.Trim(strings.TrimSpace(tzid), `"`)
	if name == "" {
		return "", false
	}
	// "Local" resolves against whichever machine is running the server, which
	// has nothing to do with the calendar the file came from.
	if strings.EqualFold(name, "Local") {
		return "", false
	}
	if _, err := time.LoadLocation(name); err == nil {
		return name, true
	}
	if iana, ok := windowsZones[name]; ok {
		return iana, true
	}
	return "", false
}

// parseICSTime parses a DTSTART/DTEND value. Wall-clock values carrying a TZID
// are anchored in that zone; UTC values end in Z; floating values (no TZID, no
// Z) are treated as UTC. A TZID nothing can resolve falls back to UTC. A date
// is a day, not an instant, so it is UTC midnight whatever zone is named.
func parseICSTime(value, tzid string, allDay bool) (time.Time, error) {
	// The T and Z separators are read in either case, like the names around
	// them.
	value = strings.ToUpper(strings.TrimSpace(value))
	if allDay {
		return time.Parse("20060102", value)
	}
	name, _ := resolveTZID(tzid)
	loc := loadLocationOrUTC(name)
	if strings.HasSuffix(value, "Z") {
		return time.Parse("20060102T150405Z", value)
	}
	return time.ParseInLocation("20060102T150405", value, loc)
}

func unfoldICS(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// A file separated by bare CR is one long line otherwise, so no component
	// in it is ever seen and the whole thing imports as nothing at all.
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	return text
}

// containsICSComponent reports whether the body holds a line this parser would
// recognise as opening a component. A body with none imported nothing because
// there was nothing in it to read, which is worth saying: every counter
// reading zero is also what a calendar containing no events answers with.
func containsICSComponent(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "BEGIN:VCALENDAR") || strings.Contains(upper, "BEGIN:VEVENT")
}

// splitContentLine splits a content line into its name, parameters and value
// per RFC 5545 §3.1: the value starts at the first colon outside a quoted
// parameter value, and parameters split on semicolons outside quotes. ok is
// false for a line with no value at all.
func splitContentLine(line string) (name string, params []string, value string, ok bool) {
	inQuote := false
	segStart := 0
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == ';' || c == ':':
			seg := line[segStart:i]
			if segStart == 0 {
				name = seg
			} else {
				params = append(params, seg)
			}
			segStart = i + 1
			if c == ':' {
				return name, params, line[i+1:], true
			}
		}
	}
	return "", nil, "", false
}

func parseICS(text string) []rawEvent {
	text = unfoldICS(text)
	lines := strings.Split(text, "\n")
	var events []rawEvent
	var cur *rawEvent
	// exdateSeen dedupes the current event's EXDATE values as they are read,
	// so a repeated value costs nothing and the count is of distinct ones.
	var exdateSeen map[int64]bool
	// A VALARM is a component nested inside the event, and it carries property
	// names the event itself uses. Read flat, its DESCRIPTION would land on the
	// event as its memo, replacing whatever the file said the event was about.
	inAlarm := false
	for _, line := range lines {
		// A byte order mark belongs at the head of the file, but some exporters
		// write one against a component's first line, where it carries no text
		// and keeps the line from matching anything.
		line = strings.TrimPrefix(line, "\ufeff")
		// Component names are case-insensitive like every other name.
		switch {
		case strings.EqualFold(line, "BEGIN:VEVENT"):
			cur = &rawEvent{}
			exdateSeen = map[int64]bool{}
			inAlarm = false
		case strings.EqualFold(line, "END:VEVENT"):
			if cur != nil {
				events = append(events, *cur)
				cur = nil
			}
			inAlarm = false
		case cur != nil && strings.EqualFold(line, "BEGIN:VALARM"):
			inAlarm = true
		case cur != nil && strings.EqualFold(line, "END:VALARM"):
			inAlarm = false
		case cur != nil && inAlarm:
			name, params, value, ok := splitContentLine(line)
			if !ok || !strings.EqualFold(name, "TRIGGER") || cur.alarmMinutes != nil {
				// The first usable alarm wins. An event may carry several, and
				// there is one reminder to keep.
				continue
			}
			if m, ok := parseTriggerMinutes(value, params); ok {
				cur.alarmMinutes = &m
			}
		case cur != nil:
			name, params, value, ok := splitContentLine(line)
			if !ok {
				continue
			}
			// Whatever the file put in the value, what leaves this parser is
			// text. A control character is not: it survives storage and the
			// export writes it back out, handing the next reader a file whose
			// title runs a command in their terminal.
			val := dropForbiddenControl(value)
			key := strings.ToUpper(name)
			isDate := false
			tzid := ""
			for _, p := range params {
				if strings.EqualFold(p, "VALUE=DATE") {
					isDate = true
				}
				if len(p) > 5 && strings.EqualFold(p[:5], "TZID=") {
					tzid = strings.Trim(p[5:], `"`)
				}
			}
			switch key {
			case "UID":
				cur.uid = val
			case "SUMMARY":
				cur.summary = unescapeICS(val)
			case "LOCATION":
				cur.location = unescapeICS(val)
			case "DESCRIPTION":
				cur.desc = truncateRunes(unescapeICS(val), calimport.MaxMemoRunes)
			case "URL":
				cur.url = val
			case "CLASS":
				cur.class = strings.ToUpper(strings.TrimSpace(val))
			case "TRANSP":
				cur.transp = strings.ToUpper(strings.TrimSpace(val))
			case "RRULE":
				cur.rrule = val
			case "DTSTART":
				cur.allDay = isDate
				if tzid != "" {
					cur.tzid = tzid
				}
				cur.noteUnknownZone(tzid)
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.start = t
				}
			case "DTEND":
				if cur.tzid == "" && tzid != "" {
					cur.tzid = tzid
				}
				cur.noteUnknownZone(tzid)
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.end = t
				}
			case "DURATION":
				d, ok := parseICSDuration(strings.TrimPrefix(strings.TrimSpace(strings.ToUpper(val)), "+"))
				cur.duration = d
				cur.hasDuration = true
				cur.badDuration = !ok
			case "EXDATE":
				// EXDATE is multi-valued and may appear more than once. Every
				// value names an occurrence the series does not have, so
				// dropping them resurrects occurrences the author cancelled.
				for _, v := range strings.Split(val, ",") {
					if cur.tooManyExdates {
						break
					}
					t, err := parseICSTime(v, tzid, isDate)
					if err != nil {
						continue
					}
					key := t.UnixMilli()
					if exdateSeen[key] {
						continue
					}
					if len(cur.exdates) >= calimport.MaxExdates {
						cur.tooManyExdates = true
						break
					}
					exdateSeen[key] = true
					cur.exdates = append(cur.exdates, t.UTC())
				}
			case "RECURRENCE-ID":
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.recurrenceID = t.UTC()
					cur.hasRecurrenceID = true
				}
			}
		}
	}
	return events
}

// truncateRunes cuts s to at most n characters, never inside one.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func unescapeICS(s string) string {
	r := strings.NewReplacer(
		`\\`, "\\",
		`\;`, ";",
		`\,`, ",",
		`\n`, "\n",
		`\N`, "\n",
	)
	return r.Replace(s)
}

// parseTriggerMinutes reads a TRIGGER value as minutes before the start.
//
// Only a duration relative to the start is usable: an absolute trigger names
// an instant rather than an offset, and one relative to the end has no
// equivalent here. Both are reported as unusable rather than approximated,
// because a reminder that fires at the wrong time is worse than one the
// import says it did not take.
func parseTriggerMinutes(value string, params []string) (int32, bool) {
	for _, p := range params {
		if strings.EqualFold(p, "VALUE=DATE-TIME") || strings.EqualFold(p, "RELATED=END") {
			return 0, false
		}
	}
	value = strings.TrimSpace(strings.ToUpper(value))
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimLeft(value, "+-")
	d, ok := parseICSDuration(value)
	if !ok {
		return 0, false
	}
	if d == 0 {
		return 0, true
	}
	// A trigger after the start would be a reminder for a commitment already
	// under way, which this product has no way to express.
	if !negative {
		return 0, false
	}
	if d%time.Minute != 0 {
		return 0, false
	}
	return int32(d / time.Minute), true
}

// The units a duration may name, in the order the grammar puts them. Weeks
// stand alone; the rest run from days down to seconds, each at most once.
const (
	durNoUnit = iota
	durWeek
	durDay
	durHour
	durMinute
	durSecond
)

// maxICSDuration is the longest span a Duration can hold, which is what bounds
// the arithmetic below.
const maxICSDuration = time.Duration(1<<63 - 1)

// parseICSDuration parses the unsigned part of an RFC 5545 duration.
//
// The grammar is followed rather than approximated: each unit appears at most
// once, in order, weeks combine with nothing, and a value naming no unit at
// all is not a duration. Adding up whatever units a malformed value happens to
// carry would answer a file that asked for no reminder with one -- and a
// reminder at the wrong time is worse than one the import says it did not
// take.
func parseICSDuration(s string) (time.Duration, bool) {
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	s = s[1:]
	var total time.Duration
	inTime := false
	num := ""
	last := durNoUnit
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num += string(r)
			continue
		}
		if r == 'T' {
			if num != "" || inTime {
				return 0, false
			}
			inTime = true
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return 0, false
		}
		num = ""
		var unit int
		var size time.Duration
		switch {
		case r == 'W' && !inTime:
			unit, size = durWeek, 7*24*time.Hour
		case r == 'D' && !inTime:
			unit, size = durDay, 24*time.Hour
		case r == 'H' && inTime:
			unit, size = durHour, time.Hour
		case r == 'M' && inTime:
			unit, size = durMinute, time.Minute
		case r == 'S' && inTime:
			unit, size = durSecond, time.Second
		default:
			return 0, false
		}
		if unit <= last || last == durWeek {
			return 0, false
		}
		last = unit
		// More than a duration can hold is refused rather than allowed to wrap
		// round, which would land on some unrelated offset and read as an
		// interval someone chose.
		if time.Duration(n) > maxICSDuration/size {
			return 0, false
		}
		part := time.Duration(n) * size
		if total > maxICSDuration-part {
			return 0, false
		}
		total += part
	}
	if num != "" || last == durNoUnit {
		return 0, false
	}
	return total, true
}
