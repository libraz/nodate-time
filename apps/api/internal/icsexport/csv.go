package icsexport

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// RenderCSV writes one row per rendered event, masters before changed
// occurrences.
func RenderCSV(series []ical.Series) []byte {
	var b bytes.Buffer
	b.WriteString("Subject,Start Date,Start Time,End Date,End Time,All Day,Location,Description,URL\r\n")
	for _, s := range series {
		if s.Master != nil {
			writeCSVRow(&b, *s.Master)
		}
		for _, o := range s.Overrides {
			writeCSVRow(&b, o)
		}
	}
	return b.Bytes()
}

func writeCSVRow(b *bytes.Buffer, e ical.Event) {
	// Timed events render in their own zone; an all-day event is dates, read
	// as UTC.
	loc := recurrence.LoadLocation(e.Timezone)
	if e.AllDay {
		loc = time.UTC
	}
	start := e.Start.In(loc)
	end := e.End.In(loc)
	// The stored all-day end is the exclusive midnight after the last day;
	// the inclusive last day is what spreadsheets and importers expect.
	endDate := end.Format("2006-01-02")
	if e.AllDay && end.After(start) {
		endDate = end.AddDate(0, 0, -1).Format("2006-01-02")
	}
	location, memo, url := e.Location, e.Description, e.URL
	if e.Masked {
		location, memo, url = "", "", ""
	}
	fields := []string{
		csvEscape(e.Title),
		start.Format("2006-01-02"),
		start.Format("15:04:05"),
		endDate,
		end.Format("15:04:05"),
		fmt.Sprintf("%t", e.AllDay),
		csvEscape(location),
		csvEscape(memo),
		csvEscape(url),
	}
	b.WriteString(strings.Join(fields, ","))
	b.WriteString("\r\n")
}

// csvEscape renders one CSV text field.
//
// A spreadsheet reads a cell starting with = + - @ (or a tab or CR ahead of
// one) as a formula, so such a field is prefixed with a quote, which
// spreadsheets show as text. Control characters go as in the iCalendar export.
func csvEscape(s string) string {
	s = ical.DropForbiddenControl(s)
	if s != "" && strings.IndexByte("=+-@\t\r", s[0]) >= 0 {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
