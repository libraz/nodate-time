// Package ical is the iCalendar codec the export, the import and CalDAV
// share. It renders series to RFC 5545 text and parses VEVENTs back; it reads
// no database and decides no visibility, so the same input always renders the
// same bytes.
//
// A series renders as one VEVENT carrying its rule and cancellations,
// followed by one VEVENT per changed occurrence under the same UID. Timed
// values are written in the event's own zone with a VTIMEZONE defining it, so
// a rule expands where the server expands it.
package ical

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// RendererVersion names the renderer's output. Any change to what Render or
// ContentHash produce for the same input raises it, which invalidates every
// CalDAV sync token and ETag derived from the old output.
const RendererVersion = "2"

// Event is one VEVENT's worth of values: a series' master or one changed
// occurrence.
type Event struct {
	Title, Location, Description, URL string
	// Start and End are the bounds; an all-day event's are UTC midnights.
	Start, End time.Time
	AllDay     bool
	// Timezone is the IANA zone a timed event is written in; one that does not
	// load reads as UTC.
	Timezone string
	// Class is the CLASS value to write, or "" to write none.
	Class       string
	Transparent bool
	// Tentative writes STATUS:TENTATIVE: the time is held but not settled.
	Tentative bool
	// AlarmMinutes is how long before the start the reminder fires; nil
	// writes no VALARM.
	AlarmMinutes *int32
	// OriginalStart is the occurrence a changed occurrence replaces; zero on a
	// master.
	OriginalStart time.Time
	// Masked withholds the location, description and URL.
	Masked    bool
	UpdatedAt time.Time
}

// Series is one recurring or single event as one calendar resource. Master
// is nil when only changed occurrences are rendered.
type Series struct {
	PublicID []byte
	// AllDay and Timezone are the series' own value type and zone, which every
	// RECURRENCE-ID follows whatever the occurrence has become.
	AllDay   bool
	Timezone string
	// Rule and Exdates are written on the master only.
	Rule      *recurrence.Rule
	Exdates   recurrence.ExceptionList
	Master    *Event
	Overrides []Event
}

// RenderOptions are the values Render takes from outside the series.
type RenderOptions struct {
	// Now bounds the VTIMEZONE scan and is the DTSTAMP when Stamp is nil.
	Now time.Time
	// Stamp, when set, is the DTSTAMP of a series' VEVENTs.
	Stamp func(Series) time.Time
	// UID, when set, names a series; otherwise DefaultUID does.
	UID          func(Series) string
	AppName      string
	CalendarName string
}

// DefaultUID is the UID a series is known by unless something else names it.
func DefaultUID(publicID []byte) string {
	return hex.EncodeToString(publicID) + "@nodate-time"
}

func (o RenderOptions) uid(s Series) string {
	if o.UID != nil {
		return o.UID(s)
	}
	return DefaultUID(s.PublicID)
}

func (o RenderOptions) stamp(s Series) time.Time {
	if o.Stamp != nil {
		return o.Stamp(s)
	}
	return o.Now
}

// Render writes series as one VCALENDAR.
func Render(series []Series, opts RenderOptions) []byte {
	var b strings.Builder
	writeFolded(&b, "BEGIN:VCALENDAR")
	writeFolded(&b, "VERSION:2.0")
	// Escaped like any other TEXT value: the name comes from configuration,
	// and an unescaped separator in it would end the property early.
	writeFolded(&b, "PRODID:-//"+escapeText(opts.AppName)+"//EN")
	writeFolded(&b, "CALSCALE:GREGORIAN")
	writeFolded(&b, "X-WR-CALNAME:"+escapeText(opts.CalendarName))
	zones := zonesUsed(series)
	zoneNames := make([]string, 0, len(zones))
	for z := range zones {
		zoneNames = append(zoneNames, z)
	}
	sort.Strings(zoneNames)
	for _, z := range zoneNames {
		writeVTimezone(&b, z, zones[z], opts.Now)
	}
	for _, s := range series {
		stamp := formatUTC(opts.stamp(s))
		writeSeries(&b, s, opts.uid(s), &stamp)
	}
	writeFolded(&b, "END:VCALENDAR")
	return []byte(b.String())
}

// ContentHash identifies what Render writes for s: the first 16 bytes of the
// SHA-256 of its VEVENTs, in hex. DTSTAMP and VTIMEZONE are left out, so the
// hash moves only when the series' content does.
func ContentHash(s Series, opts RenderOptions) string {
	var b strings.Builder
	writeSeries(&b, s, opts.uid(s), nil)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// writeSeries writes the master, then each changed occurrence. stamp nil
// writes no DTSTAMP.
func writeSeries(b *strings.Builder, s Series, uid string, stamp *string) {
	if s.Master != nil {
		writeEvent(b, s, *s.Master, false, uid, stamp)
	}
	for _, o := range s.Overrides {
		writeEvent(b, s, o, true, uid, stamp)
	}
}

func writeEvent(b *strings.Builder, s Series, e Event, override bool, uid string, stamp *string) {
	writeFolded(b, "BEGIN:VEVENT")
	// A changed occurrence shares the series' UID and is told apart by its
	// RECURRENCE-ID; one of its own would make it a separate event.
	writeFolded(b, "UID:"+uid)
	if stamp != nil {
		writeFolded(b, "DTSTAMP:"+*stamp)
	}
	if e.AllDay {
		writeFolded(b, "DTSTART;VALUE=DATE:"+formatDate(e.Start))
		writeFolded(b, "DTEND;VALUE=DATE:"+formatDate(e.End))
	} else {
		zone := zoneOf(e.Timezone)
		writeFolded(b, timeProp("DTSTART", e.Start, zone))
		writeFolded(b, timeProp("DTEND", e.End, zone))
	}
	if override {
		writeFolded(b, recurrenceID(s, e.OriginalStart))
	} else if s.Rule != nil {
		start := e.Start.In(recurrence.LoadLocation(recurrence.ExpansionZone(e.AllDay, e.Timezone)))
		if rrule := recurrence.FormatRRule(s.Rule, start, e.AllDay); rrule != "" {
			writeFolded(b, "RRULE:"+rrule)
			if exdate := exdateProp(s.Exdates, e.AllDay, e.Timezone, e.Start); exdate != "" {
				writeFolded(b, exdate)
			}
		}
	}
	writeFolded(b, "SUMMARY:"+escapeText(e.Title))
	if !e.Masked {
		if e.Location != "" {
			writeFolded(b, "LOCATION:"+escapeText(e.Location))
		}
		if e.Description != "" {
			writeFolded(b, "DESCRIPTION:"+escapeText(e.Description))
		}
		if e.URL != "" {
			writeFolded(b, "URL:"+escapeURI(e.URL))
		}
	}
	if e.Class != "" {
		writeFolded(b, "CLASS:"+e.Class)
	}
	transp := "OPAQUE"
	if e.Transparent {
		transp = "TRANSPARENT"
	}
	writeFolded(b, "TRANSP:"+transp)
	if e.Tentative {
		writeFolded(b, "STATUS:TENTATIVE")
	}
	if e.AlarmMinutes != nil {
		// A reminder left out of the file is one the person who set it never
		// receives: only the client holding the calendar can raise it.
		writeFolded(b, "BEGIN:VALARM")
		writeFolded(b, "ACTION:DISPLAY")
		// DISPLAY alarms require a DESCRIPTION; the title is what to say.
		writeFolded(b, "DESCRIPTION:"+escapeText(e.Title))
		writeFolded(b, "TRIGGER:"+trigger(*e.AlarmMinutes))
		writeFolded(b, "END:VALARM")
	}
	writeFolded(b, "END:VEVENT")
}

// trigger renders a reminder as minutes before the start. Minutes throughout
// rather than the largest fitting unit: the value is stored in minutes.
func trigger(minutes int32) string {
	if minutes <= 0 {
		return "PT0S"
	}
	return "-PT" + strconv.Itoa(int(minutes)) + "M"
}

// zonesUsed returns each TZID the series reference with the earliest instant
// written in it, which is where its VTIMEZONE has to start.
func zonesUsed(series []Series) map[string]time.Time {
	zones := map[string]time.Time{}
	note := func(zone string, t time.Time) {
		if zone == "" {
			return
		}
		if first, ok := zones[zone]; !ok || t.Before(first) {
			zones[zone] = t
		}
	}
	for _, s := range series {
		if s.Master != nil && !s.Master.AllDay {
			note(zoneOf(s.Master.Timezone), s.Master.Start)
		}
		for _, o := range s.Overrides {
			if !o.AllDay {
				note(zoneOf(o.Timezone), o.Start)
			}
			if !s.AllDay {
				note(zoneOf(s.Timezone), o.OriginalStart)
			}
		}
	}
	return zones
}

// zoneOf is the TZID a timed value is written in, or "" when its zone is UTC
// -- or unknown, which the expander also reads as UTC -- and the Z form says so.
func zoneOf(tz string) string {
	loc := recurrence.LoadLocation(tz)
	if loc == time.UTC {
		return ""
	}
	return loc.String()
}

func formatUTC(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// formatDate renders an all-day bound as its date: the UTC date of the bound,
// whatever zone the event records.
func formatDate(t time.Time) string {
	return t.UTC().Format("20060102")
}

// timeProp renders a timed property as a wall clock in zone, or as a UTC
// instant when zone is "".
func timeProp(name string, t time.Time, zone string) string {
	if zone == "" {
		return name + ":" + formatUTC(t)
	}
	return name + ";TZID=" + zone + ":" + t.In(recurrence.LoadLocation(zone)).Format("20060102T150405")
}

// recurrenceID names the occurrence an override replaces. RFC 5545 requires
// it to match the series' DTSTART in value type and zone.
func recurrenceID(s Series, original time.Time) string {
	if s.AllDay {
		return "RECURRENCE-ID;VALUE=DATE:" + formatDate(original)
	}
	return timeProp("RECURRENCE-ID", original, zoneOf(s.Timezone))
}

// exdateProp renders the cancellations as EXDATE in the value type and zone
// DTSTART uses. start is the series' first occurrence. An element nothing here
// can read is left out of the file, though it stays in the stored list.
func exdateProp(ex recurrence.ExceptionList, allDay bool, tz string, start time.Time) string {
	zone := zoneOf(tz)
	loc := recurrence.LoadLocation(zone)
	clock := start.In(loc)
	var values []string
	for _, el := range ex {
		s, ok := exceptionString(el)
		if !ok {
			continue
		}
		date, dateErr := time.Parse("2006-01-02", s)
		instant, instantErr := time.Parse(time.RFC3339Nano, s)
		floating, floatingErr := time.ParseInLocation("2006-01-02T15:04:05", s, loc)
		switch {
		case allDay && dateErr == nil:
			values = append(values, formatDate(date))
		case allDay && instantErr == nil:
			values = append(values, formatDate(instant))
		case allDay && floatingErr == nil:
			values = append(values, floating.Format("20060102"))
		case allDay:
		case dateErr == nil:
			values = append(values, exdateClock(time.Date(date.Year(), date.Month(), date.Day(),
				clock.Hour(), clock.Minute(), clock.Second(), 0, loc), zone))
		case instantErr == nil:
			values = append(values, exdateClock(instant, zone))
		case floatingErr == nil:
			values = append(values, exdateClock(floating, zone))
		}
	}
	if len(values) == 0 {
		return ""
	}
	switch {
	case allDay:
		return "EXDATE;VALUE=DATE:" + strings.Join(values, ",")
	case zone != "":
		return "EXDATE;TZID=" + zone + ":" + strings.Join(values, ",")
	}
	return "EXDATE:" + strings.Join(values, ",")
}

// exdateClock renders one cancelled instant as a wall clock in zone, or as a
// UTC instant when the series has no zone.
func exdateClock(t time.Time, zone string) string {
	if zone != "" {
		return t.In(recurrence.LoadLocation(zone)).Format("20060102T150405")
	}
	return formatUTC(t)
}
