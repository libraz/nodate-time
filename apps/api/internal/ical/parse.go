package ical

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// MaxExdates bounds the distinct EXDATE values one VEVENT keeps. Every
// listing and export reads a series' whole list.
const MaxExdates = 1000

// MaxDescriptionRunes is the memo length the events API accepts. The lenient
// parser cuts a longer DESCRIPTION to it.
const MaxDescriptionRunes = 16000

// MaxAlarmMinutes is the longest reminder an event stores, in minutes.
const MaxAlarmMinutes = 10080

// ErrInvalid is wrapped by every error a strict Parse returns.
var ErrInvalid = errors.New("ical: invalid calendar data")

// ParseOptions select how Parse treats a value it cannot read.
type ParseOptions struct {
	// Strict fails on an unreadable value of a property the parser reads, a
	// malformed content line or an unterminated VEVENT. Otherwise such a value
	// is dropped and the event kept. Properties the parser does not read are
	// ignored either way.
	Strict bool
}

// ParsedEvent is one VEVENT as read from a file.
type ParsedEvent struct {
	UID, Summary, Location, Description, URL string
	// Class and Transp are upper-cased as written; unrecognised values are
	// left for the caller to map.
	Class, Transp string
	Start, End    time.Time
	AllDay        bool
	// TZID is the zone DTSTART (else DTEND) named, as written.
	TZID string
	// TZUnknown records that a dated property named a zone nothing here could
	// resolve, so its wall clock was read as UTC.
	TZUnknown bool
	// RRule is the RRULE value without its name.
	RRule string
	// Exdates are distinct, UTC and at most MaxExdates; TooManyExdates
	// records that the file named more.
	Exdates        []time.Time
	TooManyExdates bool
	// Duration is the DURATION property; BadDuration records one present but
	// not a duration.
	Duration                 time.Duration
	HasDuration, BadDuration bool
	// RecurrenceID names the occurrence this VEVENT replaces.
	RecurrenceID    time.Time
	HasRecurrenceID bool
	Alarms          []Alarm
}

// Alarm is one VALARM.
type Alarm struct {
	// Action is the upper-cased ACTION, "" when absent.
	Action string
	// Minutes is how long before the start the alarm fires, set when
	// HasOffset: the first TRIGGER that reads as a whole number of minutes at
	// or before the start.
	Minutes   int32
	HasOffset bool
}

// Zone is the IANA zone the event's times were read in: "" when the file
// named none, "UTC" when it named one nothing here resolves.
func (e ParsedEvent) Zone() string {
	if e.TZID == "" {
		return ""
	}
	if name, ok := ResolveTZID(e.TZID); ok {
		return name
	}
	return "UTC"
}

// FirstExpressibleAlarm is the reminder an event keeps: the first alarm whose
// trigger reads as an offset before the start, if the calendar can store that
// offset. A later alarm does not stand in for a first one that is too long.
func FirstExpressibleAlarm(alarms []Alarm) (int32, bool) {
	for _, a := range alarms {
		if !a.HasOffset {
			continue
		}
		if a.Minutes < 0 || a.Minutes > MaxAlarmMinutes {
			return 0, false
		}
		return a.Minutes, true
	}
	return 0, false
}

// ContainsComponent reports whether text holds a line Parse would recognise as
// opening a component. A body with none imported nothing because there was
// nothing to read, which differs from a calendar holding no events.
func ContainsComponent(text []byte) bool {
	upper := strings.ToUpper(string(text))
	return strings.Contains(upper, "BEGIN:VCALENDAR") || strings.Contains(upper, "BEGIN:VEVENT")
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// Parse reads every VEVENT in text. Without Strict it never fails.
func Parse(text []byte, opts ParseOptions) ([]ParsedEvent, error) {
	lines := strings.Split(unfold(string(text)), "\n")
	var events []ParsedEvent
	var cur *ParsedEvent
	// exdateSeen dedupes the current event's EXDATE values as they are read,
	// so a repeated value costs nothing and the count is of distinct ones.
	var exdateSeen map[int64]bool
	// A VALARM repeats property names the event uses; read flat, its
	// DESCRIPTION would replace the event's.
	inAlarm := false
	for _, line := range lines {
		// Some exporters write a byte order mark against a component's first
		// line, where it keeps the line from matching anything.
		line = strings.TrimPrefix(line, "\ufeff")
		switch {
		case strings.EqualFold(line, "BEGIN:VEVENT"):
			if cur != nil && opts.Strict {
				return nil, invalid("VEVENT opened inside another")
			}
			cur = &ParsedEvent{}
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
			cur.Alarms = append(cur.Alarms, Alarm{})
		case cur != nil && strings.EqualFold(line, "END:VALARM"):
			inAlarm = false
		case cur != nil && inAlarm:
			name, params, value, ok := splitContentLine(line)
			if !ok {
				if opts.Strict && line != "" {
					return nil, invalid("malformed content line %q", line)
				}
				continue
			}
			a := &cur.Alarms[len(cur.Alarms)-1]
			switch strings.ToUpper(name) {
			case "ACTION":
				a.Action = strings.ToUpper(strings.TrimSpace(value))
			case "TRIGGER":
				m, usable, valid := parseTrigger(value, params)
				if opts.Strict && !valid {
					return nil, invalid("TRIGGER %q", value)
				}
				if usable && !a.HasOffset {
					a.Minutes, a.HasOffset = m, true
				}
			}
		case cur != nil:
			if err := readProperty(cur, line, exdateSeen, opts.Strict); err != nil {
				return nil, err
			}
		}
	}
	if cur != nil && opts.Strict {
		return nil, invalid("VEVENT not closed")
	}
	return events, nil
}

// readProperty applies one content line of a VEVENT to cur.
func readProperty(cur *ParsedEvent, line string, exdateSeen map[int64]bool, strict bool) error {
	name, params, value, ok := splitContentLine(line)
	if !ok {
		if strict && line != "" {
			return invalid("malformed content line %q", line)
		}
		return nil
	}
	// A control character survives storage and the export writes it back out,
	// so what leaves the parser is text.
	val := DropForbiddenControl(value)
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
	readTime := func(v string) (time.Time, bool, error) {
		t, err := parseTime(v, tzid, isDate)
		if err != nil {
			if strict {
				return t, false, invalid("%s %q", key, v)
			}
			return t, false, nil
		}
		return t, true, nil
	}
	switch key {
	case "UID":
		cur.UID = val
	case "SUMMARY":
		cur.Summary = unescapeText(val)
	case "LOCATION":
		cur.Location = unescapeText(val)
	case "DESCRIPTION":
		cur.Description = unescapeText(val)
		if !strict {
			cur.Description = truncateRunes(cur.Description, MaxDescriptionRunes)
		}
	case "URL":
		cur.URL = val
	case "CLASS":
		cur.Class = strings.ToUpper(strings.TrimSpace(val))
	case "TRANSP":
		cur.Transp = strings.ToUpper(strings.TrimSpace(val))
	case "RRULE":
		cur.RRule = val
	case "DTSTART":
		cur.AllDay = isDate
		if tzid != "" {
			cur.TZID = tzid
		}
		cur.noteUnknownZone(tzid)
		t, ok, err := readTime(val)
		if err != nil {
			return err
		}
		if ok {
			cur.Start = t
		}
	case "DTEND":
		if cur.TZID == "" && tzid != "" {
			cur.TZID = tzid
		}
		cur.noteUnknownZone(tzid)
		t, ok, err := readTime(val)
		if err != nil {
			return err
		}
		if ok {
			cur.End = t
		}
	case "DURATION":
		d, ok := parseDuration(strings.TrimPrefix(strings.TrimSpace(strings.ToUpper(val)), "+"))
		if strict && !ok {
			return invalid("DURATION %q", val)
		}
		cur.Duration = d
		cur.HasDuration = true
		cur.BadDuration = !ok
	case "EXDATE":
		// Multi-valued and repeatable. Every value is an occurrence the series
		// does not have; dropping one resurrects it.
		for _, v := range strings.Split(val, ",") {
			if cur.TooManyExdates {
				break
			}
			t, ok, err := readTime(v)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			key := t.UnixMilli()
			if exdateSeen[key] {
				continue
			}
			if len(cur.Exdates) >= MaxExdates {
				cur.TooManyExdates = true
				break
			}
			exdateSeen[key] = true
			cur.Exdates = append(cur.Exdates, t.UTC())
		}
	case "RECURRENCE-ID":
		t, ok, err := readTime(val)
		if err != nil {
			return err
		}
		if ok {
			cur.RecurrenceID = t.UTC()
			cur.HasRecurrenceID = true
		}
	}
	return nil
}

// noteUnknownZone records a TZID nothing here resolves. No TZID at all is a
// floating time rather than a zone that failed.
func (e *ParsedEvent) noteUnknownZone(tzid string) {
	if tzid == "" {
		return
	}
	if _, ok := ResolveTZID(tzid); !ok {
		e.TZUnknown = true
	}
}

// ResolveTZID maps a TZID from a file onto an IANA zone name. A Windows name,
// which Outlook and Exchange write, maps onto its IANA equivalent. ok is false
// when the name means nothing here; the caller then reads the value as UTC.
func ResolveTZID(tzid string) (string, bool) {
	name := strings.Trim(strings.TrimSpace(tzid), `"`)
	if name == "" {
		return "", false
	}
	// "Local" is whichever machine runs the server, not the file's calendar.
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

// parseTime parses a DTSTART/DTEND-shaped value. A TZID anchors a wall clock
// in that zone; a Z suffix is UTC; floating values and unresolvable TZIDs read
// as UTC. A date is UTC midnight whatever zone is named.
func parseTime(value, tzid string, allDay bool) (time.Time, error) {
	// The T and Z separators are read in either case, like the names.
	value = strings.ToUpper(strings.TrimSpace(value))
	if allDay {
		return time.Parse("20060102", value)
	}
	name, _ := ResolveTZID(tzid)
	loc := recurrence.LoadLocation(name)
	if strings.HasSuffix(value, "Z") {
		return time.Parse("20060102T150405Z", value)
	}
	return time.ParseInLocation("20060102T150405", value, loc)
}

// splitContentLine splits a content line into its name, parameters and value
// per RFC 5545 §3.1: the value starts at the first colon outside a quoted
// parameter value. ok is false for a line with no value.
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

// parseTrigger reads a TRIGGER as minutes before the start. usable is false
// for anything the stored offset cannot express: an absolute instant, a
// trigger relative to the end, one after the start or one with seconds. valid
// is false only when the value is not a trigger at all.
func parseTrigger(value string, params []string) (minutes int32, usable, valid bool) {
	for _, p := range params {
		if strings.EqualFold(p, "VALUE=DATE-TIME") {
			v := strings.ToUpper(strings.TrimSpace(value))
			_, err := time.Parse("20060102T150405Z", v)
			if err != nil {
				_, err = time.Parse("20060102T150405", v)
			}
			return 0, false, err == nil
		}
	}
	relatedEnd := false
	for _, p := range params {
		if strings.EqualFold(p, "RELATED=END") {
			relatedEnd = true
		}
	}
	value = strings.TrimSpace(strings.ToUpper(value))
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimLeft(value, "+-")
	d, ok := parseDuration(value)
	switch {
	case !ok:
		return 0, false, false
	case relatedEnd:
		return 0, false, true
	case d == 0:
		return 0, true, true
	case !negative, d%time.Minute != 0:
		// After the start is a reminder for something already under way; a
		// rounded one is a different reminder.
		return 0, false, true
	}
	return int32(d / time.Minute), true, true
}

// The units a duration may name, in grammar order. Weeks stand alone; the
// rest run from days to seconds, each at most once.
const (
	durNoUnit = iota
	durWeek
	durDay
	durHour
	durMinute
	durSecond
)

// maxDuration is the longest span a Duration holds, which bounds the sums.
const maxDuration = time.Duration(1<<63 - 1)

// parseDuration parses the unsigned part of an RFC 5545 duration, following
// the grammar rather than adding up whatever units a malformed value carries.
func parseDuration(s string) (time.Duration, bool) {
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
		// Refused rather than wrapped round to an unrelated offset.
		if time.Duration(n) > maxDuration/size {
			return 0, false
		}
		part := time.Duration(n) * size
		if total > maxDuration-part {
			return 0, false
		}
		total += part
	}
	if num != "" || last == durNoUnit {
		return 0, false
	}
	return total, true
}
