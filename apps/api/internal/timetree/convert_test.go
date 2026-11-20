package timetree

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

const testWSZone = "Asia/Tokyo"

var tokyo = func() *time.Location { l, _ := time.LoadLocation(testWSZone); return l }()

func jst(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, tokyo) }

func ms(t time.Time) int64 { return t.UnixMilli() }

func testConverter(m *MappingCalendar) *converter {
	labels := []ttLabel{{ID: "1", Name: "花子"}, {ID: "2", Name: "太郎"}, {ID: "3", Name: "花子"}, {ID: "4", Name: "unmapped"}}
	return newConverter(testWSZone, labels, m, "Ops@Example.com")
}

func timed(start, end time.Time) ttEvent {
	return ttEvent{ID: "e1", Title: "t", StartAt: ms(start), EndAt: ms(end), StartTimezone: "Asia/Tokyo"}
}

func allDay(start, end time.Time, tz string) ttEvent {
	return ttEvent{ID: "e1", Title: "t", AllDay: true, StartAt: ms(start), EndAt: ms(end), StartTimezone: tz}
}

func TestConvertAllDaySingleDayIsWorkspaceMidnightExclusive(t *testing.T) {
	d := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	c := testConverter(nil).convert(allDay(d, d, "UTC"))
	require.True(t, c.ok())
	assert.True(t, c.Event.AllDay)
	assert.Equal(t, testWSZone, c.Event.Timezone)
	assert.True(t, jst(2026, 4, 10, 0).Equal(c.Event.Start), "start %s", c.Event.Start)
	assert.True(t, jst(2026, 4, 11, 0).Equal(c.Event.End), "end %s", c.Event.End)
}

func TestConvertAllDayMultiDayIncludesTheLastDay(t *testing.T) {
	s := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	e := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	c := testConverter(nil).convert(allDay(s, e, "UTC"))
	assert.True(t, jst(2026, 4, 10, 0).Equal(c.Event.Start))
	assert.True(t, jst(2026, 4, 13, 0).Equal(c.Event.End))
}

func TestConvertAllDayReadsTheDateInTheStartTimezone(t *testing.T) {
	// 2026-04-09T15:00Z is 04-10 in Tokyo but 04-09 in UTC.
	d := time.Date(2026, 4, 9, 15, 0, 0, 0, time.UTC)
	c := testConverter(nil).convert(allDay(d, d, "Asia/Tokyo"))
	assert.True(t, jst(2026, 4, 10, 0).Equal(c.Event.Start))
	u := testConverter(nil).convert(allDay(d, d, "Not/AZone"))
	assert.True(t, jst(2026, 4, 9, 0).Equal(u.Event.Start), "an unreadable zone reads as UTC")
	e := testConverter(nil).convert(allDay(d, d, ""))
	assert.True(t, jst(2026, 4, 9, 0).Equal(e.Event.Start), "an empty zone reads as UTC")
}

func TestConvertAllDayWithoutEndLeavesTheDefaultToTheStore(t *testing.T) {
	ev := allDay(time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), time.Time{}, "UTC")
	ev.EndAt = 0
	c := testConverter(nil).convert(ev)
	assert.True(t, c.Event.End.IsZero())
}

func TestConvertTimedKeepsTheInstantAndItsZone(t *testing.T) {
	ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
	ev.StartTimezone = "America/New_York"
	ev.Title, ev.Note, ev.Location, ev.URL = "Meet", "memo", "room", "https://x.test"
	c := testConverter(nil).convert(ev)
	require.True(t, c.ok())
	assert.Equal(t, "America/New_York", c.Event.Timezone)
	assert.True(t, jst(2026, 4, 10, 10).Equal(c.Event.Start))
	assert.True(t, jst(2026, 4, 10, 11).Equal(c.Event.End))
	assert.Equal(t, "e1", c.Event.SourceUID)
	assert.Equal(t, "Meet", c.Event.Title)
	assert.Equal(t, "memo", c.Event.Memo)
	assert.Equal(t, "room", c.Event.Location)
	assert.Equal(t, "https://x.test", c.Event.URL)
	assert.Equal(t, "default", string(c.Event.Visibility))
	assert.Equal(t, "busy", string(c.Event.ShowAs))
	assert.False(t, c.Event.NotificationOffset.Valid)
}

func TestConvertTimedUTCAndUnknownZonesBecomeTheWorkspaceZone(t *testing.T) {
	for _, tz := range []string{"UTC", "Etc/UTC", "", "Bogus/Zone", "Local"} {
		ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
		ev.StartTimezone = tz
		c := testConverter(nil).convert(ev)
		assert.Equal(t, testWSZone, c.Event.Timezone, "zone %q", tz)
		assert.True(t, jst(2026, 4, 10, 10).Equal(c.Event.Start), "the instant is unchanged for %q", tz)
	}
}

func TestConvertTimedWithoutEndLeavesTheDefaultToTheStore(t *testing.T) {
	ev := timed(jst(2026, 4, 10, 10), time.Time{})
	ev.EndAt = 0
	assert.True(t, testConverter(nil).convert(ev).Event.End.IsZero())
}

func TestConvertEndBeforeStartIsRejected(t *testing.T) {
	c := testConverter(nil).convert(timed(jst(2026, 4, 10, 11), jst(2026, 4, 10, 10)))
	assert.Equal(t, rejectEndBefore, c.Reject)
	d := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	c = testConverter(nil).convert(allDay(d, d.AddDate(0, 0, -1), "UTC"))
	assert.Equal(t, rejectEndBefore, c.Reject)
}

func TestConvertStartMissingOrZeroIsSkipped(t *testing.T) {
	for _, start := range []int64{0, -5} {
		ev := ttEvent{ID: "e", Title: "t", StartAt: start}
		assert.Equal(t, skipNoStart, testConverter(nil).convert(ev).Skip)
	}
	var ev ttEvent
	require.NoError(t, json.Unmarshal([]byte(`{"id":"e","title":"t","start_at":null}`), &ev))
	assert.Equal(t, skipNoStart, testConverter(nil).convert(ev).Skip)
}

func TestConvertSkips(t *testing.T) {
	base := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
	cases := map[string]struct {
		mut  func(*ttEvent)
		want string
	}{
		"deleted":     {func(e *ttEvent) { e.DeletedAt = json.RawMessage(`1700000000000`) }, skipDeleted},
		"deactivated": {func(e *ttEvent) { e.DeactivatedAt = json.RawMessage(`1700000000000`) }, skipDeleted},
		"birthday":    {func(e *ttEvent) { e.Type = 1 }, skipBirthday},
		"memo":        {func(e *ttEvent) { e.Category = 2 }, skipMemo},
		"untitled":    {func(e *ttEvent) { e.Title = "" }, skipUntitled},
		"rdate":       {func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=DAILY", "RDATE:20260501T000000Z"} }, skipOtherRepeat},
		"exrule":      {func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=DAILY", "EXRULE:FREQ=WEEKLY"} }, skipOtherRepeat},
		"other row":   {func(e *ttEvent) { e.Recurrences = []string{"DTSTART:20260401"} }, skipOtherRepeat},
		"two rrules":  {func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=DAILY", "RRULE:FREQ=WEEKLY"} }, skipOtherRepeat},
		"unsupported": {func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=HOURLY"} }, skipUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := base
			tc.mut(&e)
			c := testConverter(nil).convert(e)
			assert.Equal(t, tc.want, c.Skip)
			assert.False(t, c.ok())
		})
	}
	nullDeleted := base
	nullDeleted.DeletedAt = json.RawMessage(`null`)
	assert.True(t, testConverter(nil).convert(nullDeleted).ok(), "a null deleted_at is not a deletion")
}

func TestConvertMemoIsTruncatedToTheStoreLimit(t *testing.T) {
	ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
	ev.Note = strings.Repeat("あ", calimport.MaxMemoRunes+1)
	c := testConverter(nil).convert(ev)
	assert.Equal(t, calimport.MaxMemoRunes, len([]rune(c.Event.Memo)))
	ev.Note = strings.Repeat("a", calimport.MaxMemoRunes)
	assert.Equal(t, ev.Note, testConverter(nil).convert(ev).Event.Memo)
}

func TestConvertOwnerFollowsTheLabel(t *testing.T) {
	m := &MappingCalendar{
		DefaultOwner: "Mgr@example.com",
		Labels:       map[string]string{"花子": "Hanako@Example.com", "太郎": "taro@example.com"},
	}
	cv := testConverter(m)
	own := func(label flexID) string {
		ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
		ev.LabelID = label
		return cv.convert(ev).Owner
	}
	assert.Equal(t, "hanako@example.com", own("1"))
	assert.Equal(t, "taro@example.com", own("2"))
	assert.Equal(t, "hanako@example.com", own("3"), "a second label with the same name has the same owner")
	assert.Equal(t, "mgr@example.com", own("4"), "an unmapped label falls back to default_owner")
	assert.Equal(t, "mgr@example.com", own(""), "no label falls back to default_owner")
	assert.Equal(t, "mgr@example.com", own("99"), "an unknown label id falls back to default_owner")
	assert.Equal(t, "ops@example.com", testConverter(&MappingCalendar{}).convert(timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))).Owner,
		"without default_owner the importer owns")
}

func TestConvertDefaultAttendeesJoinOnlyUnmappedEvents(t *testing.T) {
	m := &MappingCalendar{
		DefaultOwner:     "mgr@example.com",
		DefaultAttendees: []string{"Hanako@Example.com", "taro@example.com"},
		Labels:           map[string]string{"花子": "hanako@example.com"},
	}
	cv := testConverter(m)
	attendees := func(label flexID) []string {
		ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
		ev.LabelID = label
		return cv.convert(ev).Attendees
	}
	assert.Empty(t, attendees("1"), "a person's label is that person's event alone")
	assert.Equal(t, []string{"hanako@example.com", "taro@example.com"}, attendees("4"))
	assert.Equal(t, []string{"hanako@example.com", "taro@example.com"}, attendees(""))
	assert.Empty(t, testConverter(&MappingCalendar{}).convert(timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))).Attendees,
		"without default_attendees nobody joins")
}

func TestConvertLabelNamesAreMatchedExactly(t *testing.T) {
	m := &MappingCalendar{Labels: map[string]string{"ＡＢ": "a@example.com"}}
	cv := newConverter(testWSZone, []ttLabel{{ID: "1", Name: "AB"}, {ID: "2", Name: "ＡＢ"}}, m, "ops@example.com")
	ev := timed(jst(2026, 4, 10, 10), jst(2026, 4, 10, 11))
	ev.LabelID = "1"
	assert.Equal(t, "ops@example.com", cv.convert(ev).Owner)
	ev.LabelID = "2"
	assert.Equal(t, "a@example.com", cv.convert(ev).Owner)
}

// occurrenceStarts expands a converted series over April-May 2026 and returns
// its live occurrence starts in the workspace zone.
func occurrenceStarts(t *testing.T, ev calimport.Event) []time.Time {
	t.Helper()
	start := ev.Start.In(recurrence.LoadLocation(ev.Timezone))
	rule, ok := recurrence.ParseRRule(ev.RRule, start)
	require.True(t, ok, "rule %q", ev.RRule)
	end := ev.End
	if end.IsZero() {
		end = start.AddDate(0, 0, 1)
	}
	skipped := recurrence.Exceptions(ev.Exdates)
	var out []time.Time
	for _, o := range recurrence.ExpandInZone(rule, start, end, jst(2026, 4, 1, 0), jst(2026, 6, 1, 0), ev.Timezone) {
		if !skipped.Contains(o.StartAt) {
			out = append(out, o.StartAt.In(tokyo))
		}
	}
	return out
}

func TestConvertAllDayRecurrenceKeepsTheUntilDayAndDropsTheExdate(t *testing.T) {
	// Auckland midnight 04-06; the UNTIL instant is Auckland 04-27 00:00, which
	// is before the Tokyo-midnight occurrence of that day unless it is a date.
	s := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	ev := allDay(s, s, "Pacific/Auckland")
	ev.Recurrences = []string{"RRULE:FREQ=WEEKLY;UNTIL=20260426T120000Z", "EXDATE:20260413"}
	c := testConverter(nil).convert(ev)
	require.True(t, c.ok(), "%+v", c)
	assert.Equal(t, "FREQ=WEEKLY;UNTIL=20260427", c.Event.RRule)
	assert.Equal(t, []time.Time{jst(2026, 4, 13, 0).UTC()}, c.Event.Exdates)
	got := occurrenceStarts(t, c.Event)
	require.Len(t, got, 3)
	assert.True(t, got[0].Equal(jst(2026, 4, 6, 0)))
	assert.True(t, got[1].Equal(jst(2026, 4, 20, 0)), "the cancelled week is gone")
	assert.True(t, got[2].Equal(jst(2026, 4, 27, 0)), "the UNTIL day is kept")
}

func TestConvertUntilRewrite(t *testing.T) {
	tokyoDay := time.Date(2026, 4, 5, 15, 0, 0, 0, time.UTC) // 04-06 in Tokyo
	cases := map[string]string{
		"FREQ=DAILY;UNTIL=20260410T150000Z": "FREQ=DAILY;UNTIL=20260411", // Z instant read in start zone
		"FREQ=DAILY;UNTIL=20260410T090000":  "FREQ=DAILY;UNTIL=20260410", // local time keeps its date
		"FREQ=DAILY;UNTIL=20260410":         "FREQ=DAILY;UNTIL=20260410", // date form untouched
		"FREQ=DAILY;COUNT=3":                "FREQ=DAILY;COUNT=3",
	}
	for in, want := range cases {
		ev := allDay(tokyoDay, tokyoDay, "Asia/Tokyo")
		ev.Recurrences = []string{"RRULE:" + in}
		c := testConverter(nil).convert(ev)
		require.True(t, c.ok(), in)
		assert.Equal(t, want, c.Event.RRule, in)
	}
	// A timed series keeps its UNTIL as given.
	ev := timed(jst(2026, 4, 6, 10), jst(2026, 4, 6, 11))
	ev.Recurrences = []string{"RRULE:FREQ=DAILY;UNTIL=20260410T150000Z"}
	assert.Equal(t, "FREQ=DAILY;UNTIL=20260410T150000Z", testConverter(nil).convert(ev).Event.RRule)
}

func TestConvertRRulePrefixIsStripped(t *testing.T) {
	ev := timed(jst(2026, 4, 6, 10), jst(2026, 4, 6, 11))
	ev.Recurrences = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=4"}
	c := testConverter(nil).convert(ev)
	assert.Equal(t, "FREQ=WEEKLY;BYDAY=MO;COUNT=4", c.Event.RRule)
	require.Len(t, occurrenceStarts(t, c.Event), 4)
}

func TestConvertExdateForms(t *testing.T) {
	// All-day Tokyo series: every form below names the 04-13 occurrence.
	s := time.Date(2026, 4, 5, 15, 0, 0, 0, time.UTC)
	want := []time.Time{jst(2026, 4, 13, 0).UTC()}
	rows := map[string]string{
		"date":            "EXDATE:20260413",
		"utc datetime":    "EXDATE:20260412T150000Z",
		"tzid value":      "EXDATE:TZID=Asia/Tokyo:20260413T090000",
		"tzid param":      "EXDATE;TZID=Asia/Tokyo:20260413T090000",
		"value=date":      "EXDATE;VALUE=DATE:20260413",
		"duplicate forms": "EXDATE:20260413,20260412T150000Z",
	}
	for name, row := range rows {
		ev := allDay(s, s, "Asia/Tokyo")
		ev.Recurrences = []string{"RRULE:FREQ=WEEKLY", row}
		c := testConverter(nil).convert(ev)
		require.True(t, c.ok(), name)
		assert.Equal(t, want, c.Event.Exdates, name)
	}

	multi := allDay(s, s, "Asia/Tokyo")
	multi.Recurrences = []string{"RRULE:FREQ=WEEKLY", "EXDATE:20260420,20260413", "EXDATE:20260413"}
	assert.Equal(t, []time.Time{jst(2026, 4, 13, 0).UTC(), jst(2026, 4, 20, 0).UTC()},
		testConverter(nil).convert(multi).Event.Exdates, "comma lists merge, deduplicate and sort")
}

func TestConvertTimedExdates(t *testing.T) {
	start := jst(2026, 4, 6, 10)
	want := []time.Time{jst(2026, 4, 13, 10).UTC()}
	for name, row := range map[string]string{
		"utc":   "EXDATE:20260413T010000Z",
		"tzid":  "EXDATE;TZID=Asia/Tokyo:20260413T100000",
		"date":  "EXDATE:20260413",
		"mixed": "EXDATE:20260413T010000Z,20260413",
	} {
		ev := timed(start, start.Add(time.Hour))
		ev.Recurrences = []string{"RRULE:FREQ=WEEKLY", row}
		c := testConverter(nil).convert(ev)
		require.True(t, c.ok(), name)
		assert.Equal(t, want, c.Event.Exdates, name)
	}
}

func TestConvertUnreadableExdateRejectsTheEvent(t *testing.T) {
	s := time.Date(2026, 4, 5, 15, 0, 0, 0, time.UTC)
	for _, row := range []string{
		"EXDATE:garbage",
		"EXDATE:20260413,garbage",
		"EXDATE:",
		"EXDATE:20260413T100000", // floating time, no zone
		"EXDATE;TZID=Not/AZone:20260413T100000",
		"EXDATE:TZID=Asia/Tokyo",          // zone without a value
		"EXDATE;TZID=Asia/Tokyo:20260413", // zone with a date
	} {
		ev := allDay(s, s, "Asia/Tokyo")
		ev.Recurrences = []string{"RRULE:FREQ=WEEKLY", row}
		c := testConverter(nil).convert(ev)
		assert.Equal(t, rejectBadExdate, c.Reject, row)
		assert.Empty(t, c.Skip, row)
	}
}

func TestConvertTooManyExdatesIsRejected(t *testing.T) {
	var vals []string
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= calimport.MaxExdates; i++ {
		vals = append(vals, day.AddDate(0, 0, i).Format("20060102"))
	}
	ev := allDay(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ev.Recurrences = []string{"RRULE:FREQ=DAILY", "EXDATE:" + strings.Join(vals, ",")}
	c := testConverter(nil).convert(ev)
	assert.Equal(t, rejectManyExdates, c.Reject)
	assert.True(t, c.Event.TooManyExdates)
	assert.Empty(t, c.Event.Exdates)

	ev.Recurrences = []string{"RRULE:FREQ=DAILY", "EXDATE:" + strings.Join(vals[:calimport.MaxExdates], ",")}
	c = testConverter(nil).convert(ev)
	assert.True(t, c.ok())
	assert.Len(t, c.Event.Exdates, calimport.MaxExdates)
}

func TestConvertChangedOccurrenceIsStandalone(t *testing.T) {
	ev := timed(jst(2026, 4, 13, 14), jst(2026, 4, 13, 15))
	ev.RecurringUUID = "parent"
	c := testConverter(nil).convert(ev)
	require.True(t, c.ok())
	assert.False(t, c.Event.HasRecurrenceID)
	assert.Empty(t, c.Event.RRule)
}

func TestDoubleDisplayCandidates(t *testing.T) {
	parent := timed(jst(2026, 4, 6, 10), jst(2026, 4, 6, 11))
	parent.ID = "parent"
	parent.Recurrences = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=6", "EXDATE:20260413T010000Z"}

	variant := func(id string, start time.Time, parentID string) ttEvent {
		e := timed(start, start.Add(time.Hour))
		e.ID, e.RecurringUUID = id, parentID
		return e
	}
	events := []ttEvent{
		parent,
		variant("moved-from-cancelled", jst(2026, 4, 13, 14), "parent"), // 04-13 is cancelled: no double
		variant("same-day", jst(2026, 4, 20, 14), "parent"),             // 04-20 still occurs: candidate
		variant("elsewhere", jst(2026, 4, 22, 9), "parent"),             // no occurrence that day
		variant("orphan", jst(2026, 4, 27, 9), "missing"),               // parent not in the data
		variant("beyond-count", jst(2026, 6, 1, 9), "parent"),           // series ended by COUNT
	}
	cv := testConverter(nil)
	conv := make([]converted, len(events))
	for i, e := range events {
		conv[i] = cv.convert(e)
		require.True(t, conv[i].ok(), e.ID)
	}
	assert.Equal(t, 1, doubleDisplayCandidates(events, conv))

	// A skipped parent (unsupported rule) leaves nothing to double.
	events[0].Recurrences = []string{"RRULE:FREQ=HOURLY"}
	conv[0] = cv.convert(events[0])
	assert.Equal(t, 0, doubleDisplayCandidates(events, conv))
}

func TestAnalyzeTalliesTheReportInputs(t *testing.T) {
	cv := testConverter(&MappingCalendar{Labels: map[string]string{"花子": "h@example.com"}})
	d := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	mk := func(id string, mut func(*ttEvent)) ttEvent {
		e := timed(jst(2026, 5, 1, 10), jst(2026, 5, 1, 11))
		e.ID = id
		mut(&e)
		return e
	}
	events := []ttEvent{
		mk("a", func(e *ttEvent) { e.LabelID = "1"; e.Attendees = []json.RawMessage{json.RawMessage(`1`)} }),
		mk("b", func(e *ttEvent) { e.StartTimezone = "UTC"; e.StartAt = ms(jst(2026, 3, 1, 10)) }),
		mk("c", func(e *ttEvent) { e.Type = 1 }),
		mk("d", func(e *ttEvent) { e.Category = 2 }),
		mk("e", func(e *ttEvent) { e.DeletedAt = json.RawMessage(`1`) }),
		mk("f", func(e *ttEvent) { e.Recurrences = []string{"RDATE:20260501T000000Z"} }),
		mk("g", func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=HOURLY"} }),
		mk("h", func(e *ttEvent) { e.Recurrences = []string{"RRULE:FREQ=DAILY", "EXDATE:bad"} }),
	}
	for i := 0; i < 4; i++ {
		events = append(events, mk("ad"+string(rune('0'+i)), func(e *ttEvent) {
			*e = allDay(d, d, "UTC")
			e.ID = "ad" + string(rune('0'+i))
		}))
	}
	a := analyze(calData{cal: ttCalendar{ID: "9"}, events: events}, cv)

	assert.Equal(t, 1, a.attendees)
	assert.Equal(t, 1, a.utcTimed)
	assert.Equal(t, "2026-03-01", a.oldest.Format("2006-01-02"))
	assert.Equal(t, 1, a.byLabel["花子"])
	assert.Equal(t, 11, a.byLabel[noLabel])
	assert.Equal(t, map[string]int{skipBirthday: 1, skipMemo: 1, skipDeleted: 1, skipOtherRepeat: 1, skipUnsupported: 1}, a.skips)
	assert.Equal(t, map[string]int{rejectBadExdate: 1}, a.rejects)
	assert.Len(t, a.allDay, maxAllDaySamples)
	assert.Equal(t, 1, a.owners["h@example.com"])
	assert.Equal(t, 5, a.owners["ops@example.com"], "b, and the four all-day events")

	var sb strings.Builder
	writeReport(&sb, []*analysis{a}, resolution{Plans: []calendarPlan{{TT: ttCalendar{ID: "9"}, Name: "n", Mapping: MappingCalendar{TimeTree: "x"}}}, NewUsers: []string{"u@example.com"}, Errors: []string{"bad thing"}})
	rep := sb.String()
	for _, want := range []string{"alias_code", "mapped (timetree: x)", "oldest start: 2026-03-01", "花子=1", "possible double display: 0",
		"all-day sample: ad0 start_at=", "calendar \"n\": create", "users to create: [u@example.com]", "validation errors: 1", "bad thing"} {
		assert.Contains(t, rep, want)
	}
}
