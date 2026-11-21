package e2e

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// ctEvent is the slice of an event response the shared-contract tests read.
type ctEvent struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	AllDay        bool   `json:"allDay"`
	Kind          string `json:"kind"`
	StartAt       string `json:"startAt"`
	EndAt         string `json:"endAt"`
	StartDate     string `json:"startDate"`
	EndDate       string `json:"endDate"`
	Timezone      string `json:"timezone"`
	Etag          string `json:"etag"`
	SeriesEtag    string `json:"seriesEtag"`
	IsRecurrence  bool   `json:"isRecurrence"`
	DetailsHidden bool   `json:"detailsHidden"`
	Attendees     []struct {
		UserID string `json:"userId"`
	} `json:"attendees"`
}

func (e ctEvent) attendeeIDs() []string {
	ids := make([]string, 0, len(e.Attendees))
	for _, a := range e.Attendees {
		ids = append(ids, a.UserID)
	}
	sort.Strings(ids)
	return ids
}

type ctEventList struct {
	Items   []ctEvent `json:"items"`
	HasMore bool      `json:"hasMore"`
}

// ctListEvents reads a calendar's events for a half-open date window.
func ctListEvents(t *testing.T, calURL, token, start, end string) ctEventList {
	t.Helper()
	var out ctEventList
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start="+start+"&end="+end, token, nil, &out)
	return out
}

// ctEventRow is a calendar_events row written the way another product on the
// shared schema would write it, bypassing this product's API.
type ctEventRow struct {
	Kind          string
	Visibility    string
	Title         string
	AllDay        bool
	Start, End    string
	Timezone      string
	Location      string
	Memo          string
	URL           string
	Rule          string
	Exceptions    string
	ParentID      int64
	OriginalStart string
	SourceUID     string
	OwnerUserID   string
}

// ctInsertEvent inserts a row and returns its public id and internal id.
func ctInsertEvent(t *testing.T, calPublicID, ownerPublicID string, r ctEventRow) (string, int64) {
	t.Helper()
	if r.Kind == "" {
		r.Kind = "event"
	}
	if r.Visibility == "" {
		r.Visibility = "default"
	}
	if r.Timezone == "" {
		r.Timezone = "UTC"
	}
	id, err := uuid.NewV7()
	require.NoError(t, err)
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	var parent any
	if r.ParentID != 0 {
		parent = r.ParentID
	}
	res, err := testDB.ExecContext(t.Context(), `INSERT INTO calendar_events
		(public_id, workspace_id, calendar_id, kind, visibility, title, all_day, start_at, end_at, timezone,
		 location, memo, url, source_uid, owner_user_id, created_by_user_id,
		 recurrence_rule, recurrence_exceptions, recurrence_parent_id, recurrence_original_start)
		SELECT ?, c.workspace_id, c.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, u.id, u.id, ?, ?, ?, ?
		FROM calendars c JOIN users u ON u.public_id = UUID_TO_BIN(?)
		WHERE c.public_id = UUID_TO_BIN(?)`,
		id[:], r.Kind, r.Visibility, r.Title, r.AllDay, r.Start, r.End, r.Timezone,
		nullable(r.Location), nullable(r.Memo), nullable(r.URL), nullable(r.SourceUID),
		nullable(r.Rule), nullable(r.Exceptions), parent, nullable(r.OriginalStart),
		ownerPublicID, calPublicID)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "calendar or owner not found")
	internal, err := res.LastInsertId()
	require.NoError(t, err)
	return id.String(), internal
}

// ctInsertAttendee writes an attendee row for an event, enabled or not.
func ctInsertAttendee(t *testing.T, eventInternalID int64, userPublicID string, enabled bool) {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	res, err := testDB.ExecContext(t.Context(), `INSERT INTO calendar_event_attendees
		(public_id, workspace_id, event_id, user_id, enabled)
		SELECT ?, e.workspace_id, e.id, u.id, ?
		FROM calendar_events e JOIN users u ON u.public_id = UUID_TO_BIN(?)
		WHERE e.id = ?`, id[:], enabled, userPublicID, eventInternalID)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// ctStoredEvent is a calendar_events row as it sits in the database.
type ctStoredEvent struct {
	AllDay     bool
	Kind       string
	Start, End time.Time
	Rule       sql.NullString
	Exceptions sql.NullString
	Parent     sql.NullInt64
	OrigStart  sql.NullTime
}

func ctLoadEvent(t *testing.T, publicID string) ctStoredEvent {
	t.Helper()
	var s ctStoredEvent
	err := testDB.QueryRowContext(t.Context(), `SELECT all_day, kind, start_at, end_at,
		CAST(recurrence_rule AS CHAR), CAST(recurrence_exceptions AS CHAR),
		recurrence_parent_id, recurrence_original_start
		FROM calendar_events WHERE public_id = UUID_TO_BIN(?)`, publicID).
		Scan(&s.AllDay, &s.Kind, &s.Start, &s.End, &s.Rule, &s.Exceptions, &s.Parent, &s.OrigStart)
	require.NoError(t, err)
	s.Start, s.End = s.Start.UTC(), s.End.UTC()
	return s
}

func ctMidnight(date string) time.Time {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return d
}

// Rows another product writes in the canonical all-day form are read back as
// the dates they name, whatever zone the row records and whichever zone the
// reader is in.
func TestForeignAllDayRowsReadBackAsTheirDates(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	cases := []struct {
		name       string
		row        ctEventRow
		start, end string // exclusive end
		kind       string
	}{
		{"single day, east zone", ctEventRow{Title: "east", AllDay: true, Timezone: "Asia/Tokyo",
			Start: "2026-08-05 00:00:00", End: "2026-08-06 00:00:00"}, "2026-08-05", "2026-08-06", "event"},
		{"single day, west zone", ctEventRow{Title: "west", AllDay: true, Timezone: "America/Los_Angeles",
			Start: "2026-08-07 00:00:00", End: "2026-08-08 00:00:00"}, "2026-08-07", "2026-08-08", "event"},
		{"three days across a month end", ctEventRow{Title: "span", AllDay: true, Timezone: "Pacific/Kiritimati",
			Start: "2026-08-30 00:00:00", End: "2026-09-02 00:00:00"}, "2026-08-30", "2026-09-02", "event"},
		{"milestone is a point", ctEventRow{Title: "point", AllDay: true, Kind: "milestone", Timezone: "UTC",
			Start: "2026-08-12 00:00:00", End: "2026-08-12 00:00:00"}, "2026-08-12", "2026-08-12", "milestone"},
	}

	ids := map[string]string{}
	for _, c := range cases {
		id, _ := ctInsertEvent(t, owner.CalendarID, owner.UserID, c.row)
		ids[c.name] = id
	}

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-08-01", "2026-09-30")
	byID := map[string]ctEvent{}
	for _, e := range listed.Items {
		byID[e.ID] = e
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got ctEvent
			helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+ids[c.name], owner.AccessToken, nil, &got)
			for label, e := range map[string]ctEvent{"get": got, "list": byID[ids[c.name]]} {
				require.Equal(t, ids[c.name], e.ID, label)
				require.True(t, e.AllDay, label)
				require.Equal(t, c.start, e.StartDate, "%s startDate", label)
				require.Equal(t, c.end, e.EndDate, "%s endDate (exclusive; same day for a milestone)", label)
				require.Equal(t, c.kind, e.Kind, label)
				require.Equal(t, c.start+"T00:00:00Z", e.StartAt, "%s startAt is the UTC midnight", label)
				require.Equal(t, c.end+"T00:00:00Z", e.EndAt, "%s endAt is the UTC midnight", label)
			}
		})
	}
}

// A foreign all-day series is expanded on UTC dates: every occurrence starts at
// UTC midnight, a daylight-saving change in the recorded zone does not move
// one, and the date forms of until and of an exception are honoured.
func TestForeignAllDaySeriesExpandsOnUTCDates(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// 2026-03-08 is the US spring-forward day; the zone must not leak into the grid.
	ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "counted", AllDay: true, Timezone: "America/Los_Angeles",
		Start: "2026-03-06 00:00:00", End: "2026-03-07 00:00:00",
		Rule:       `{"freq":"daily","interval":1,"count":5}`,
		Exceptions: `["2026-03-08"]`,
	})
	ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "bounded", AllDay: true, Timezone: "America/Los_Angeles",
		Start: "2026-03-20 00:00:00", End: "2026-03-21 00:00:00",
		Rule: `{"freq":"daily","interval":1,"until":"2026-03-22"}`,
	})

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-03-01", "2026-03-31")
	dates := map[string][]string{}
	for _, e := range listed.Items {
		require.True(t, e.AllDay)
		require.Equal(t, e.StartDate+"T00:00:00Z", e.StartAt)
		dates[e.Title] = append(dates[e.Title], e.StartDate)
	}
	for _, d := range dates {
		sort.Strings(d)
	}
	require.Equal(t, []string{"2026-03-06", "2026-03-07", "2026-03-09", "2026-03-10"}, dates["counted"],
		"COUNT 5 generates 06..10; the 08th is cancelled by its date")
	require.Equal(t, []string{"2026-03-20", "2026-03-21", "2026-03-22"}, dates["bounded"],
		"a date until includes that day")
}

// A foreign series cancelled by a date-only entry and by older spellings of the
// same day drops the occurrence for each spelling, and unreadable entries are
// kept when this product rewrites the array.
func TestForeignExceptionSpellingsAreHonouredAndKept(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	id, _ := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "timed", Timezone: "UTC",
		Start: "2026-04-01 09:00:00", End: "2026-04-01 10:00:00",
		Rule:       `{"freq":"daily","interval":1,"count":6}`,
		Exceptions: `["2026-04-02T09:00:00Z","2026-04-03","2026-04-04T09:00:00","not a date"]`,
	})

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-04-01", "2026-04-30")
	var days []string
	for _, e := range listed.Items {
		days = append(days, e.StartAt[:10])
	}
	sort.Strings(days)
	require.Equal(t, []string{"2026-04-01", "2026-04-05", "2026-04-06"}, days,
		"RFC 3339, date-only and floating spellings each cancel their occurrence")

	// Cancelling another occurrence keeps the entries it did not touch, spelled as they were.
	var occ ctEvent
	for _, e := range listed.Items {
		if strings.HasPrefix(e.StartAt, "2026-04-05") {
			occ = e
		}
	}
	require.NotEmpty(t, occ.ID)
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+occ.ID+"?scope=this", owner.AccessToken, nil,
		map[string]string{"If-Match": occ.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))

	stored := ctLoadEvent(t, id)
	var exceptions []string
	require.NoError(t, json.Unmarshal([]byte(stored.Exceptions.String), &exceptions))
	require.Subset(t, exceptions,
		[]string{"2026-04-02T09:00:00Z", "2026-04-03", "2026-04-04T09:00:00", "not a date"},
		"entries this product did not write keep their spelling, readable or not")
	require.Contains(t, exceptions, "2026-04-05T09:00:00Z", "a timed series cancels by RFC 3339 UTC")
}

// An override's attendee rows replace the series' attendees for that occurrence
// once it has any row at all (enabled or not); an override with none inherits.
func TestForeignOverrideAttendeesReplaceTheSeries(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, a, "editor")
	joinAs(t, calURL, owner.AccessToken, b, "editor")

	_, masterInternal := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "standup", Timezone: "UTC",
		Start: "2026-05-04 10:00:00", End: "2026-05-04 11:00:00",
		Rule: `{"freq":"daily","interval":1,"count":4}`,
	})
	ctInsertAttendee(t, masterInternal, a.UserID, true)
	ctInsertAttendee(t, masterInternal, b.UserID, true)

	// Day 2: the other product wrote an override that keeps only A (B disabled).
	_, dayTwo := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "standup", Timezone: "UTC", ParentID: masterInternal,
		Start: "2026-05-05 10:00:00", End: "2026-05-05 11:00:00", OriginalStart: "2026-05-05 10:00:00",
	})
	ctInsertAttendee(t, dayTwo, a.UserID, true)
	ctInsertAttendee(t, dayTwo, b.UserID, false)
	// Day 3: an override that has no attendee rows at all inherits the series.
	ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "standup (moved)", Timezone: "UTC", ParentID: masterInternal,
		Start: "2026-05-06 12:00:00", End: "2026-05-06 13:00:00", OriginalStart: "2026-05-06 10:00:00",
	})
	// Day 4: an override whose only rows are disabled leaves the occurrence empty.
	_, dayFour := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "standup", Timezone: "UTC", ParentID: masterInternal,
		Start: "2026-05-07 10:00:00", End: "2026-05-07 11:00:00", OriginalStart: "2026-05-07 10:00:00",
	})
	ctInsertAttendee(t, dayFour, a.UserID, false)
	ctInsertAttendee(t, dayFour, b.UserID, false)

	both := []string{a.UserID, b.UserID}
	sort.Strings(both)
	want := map[string][]string{
		"2026-05-04": both,
		"2026-05-05": {a.UserID},
		"2026-05-06": both,
		"2026-05-07": {},
	}

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-05-01", "2026-05-31")
	require.Len(t, listed.Items, 4)
	for _, e := range listed.Items {
		day := e.StartAt[:10]
		require.Equal(t, want[day], e.attendeeIDs(), "attendees on %s", day)
		// The single-occurrence read must agree with the listing.
		var one ctEvent
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+e.ID, owner.AccessToken, nil, &one)
		require.Equal(t, want[day], one.attendeeIDs(), "attendees on %s via get", day)
	}
}

// Rows this product writes satisfy the shared all-day contract: UTC midnights,
// an exclusive end, date-form series bounds and exceptions, and midnight keys
// on every override.
func TestRowsWrittenHereSatisfyTheAllDayContract(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	dateOnly := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	create := func(body map[string]any) ctEvent {
		var e ctEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, body, &e)
		return e
	}

	t.Run("single and multi-day rows", func(t *testing.T) {
		for _, tz := range []string{"Asia/Tokyo", "America/Los_Angeles", "Pacific/Auckland"} {
			one := create(map[string]any{"title": "one", "allDay": true, "timezone": tz,
				"startDate": "2026-10-04", "endDate": "2026-10-05"})
			many := create(map[string]any{"title": "many", "allDay": true, "timezone": tz,
				"startDate": "2026-10-10", "endDate": "2026-10-14"})
			s1, s2 := ctLoadEvent(t, one.ID), ctLoadEvent(t, many.ID)
			require.True(t, s1.AllDay)
			require.Equal(t, ctMidnight("2026-10-04"), s1.Start, tz)
			require.Equal(t, ctMidnight("2026-10-05"), s1.End, tz)
			require.Equal(t, ctMidnight("2026-10-10"), s2.Start, tz)
			require.Equal(t, ctMidnight("2026-10-14"), s2.End, tz)
		}
	})

	t.Run("series bound, exception and override keys", func(t *testing.T) {
		series := create(map[string]any{"title": "series", "allDay": true, "timezone": "America/Los_Angeles",
			"startDate": "2026-11-01", "endDate": "2026-11-02",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-11-07"}})

		listed := ctListEvents(t, calURL, owner.AccessToken, "2026-11-01", "2026-11-30")
		byDate := map[string]ctEvent{}
		for _, e := range listed.Items {
			byDate[e.StartDate] = e
		}
		require.Len(t, byDate, 7)

		// Cancel one occurrence, and move another.
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
			calURL+"/events/"+byDate["2026-11-03"].ID+"?scope=this", owner.AccessToken, nil,
			map[string]string{"If-Match": byDate["2026-11-03"].Etag})
		require.Equal(t, http.StatusNoContent, status, string(raw))
		status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch,
			calURL+"/events/"+byDate["2026-11-05"].ID+"?scope=this", owner.AccessToken,
			map[string]any{"title": "moved", "startDate": "2026-11-12", "endDate": "2026-11-13"},
			map[string]string{"If-Match": byDate["2026-11-05"].Etag})
		require.Equal(t, http.StatusOK, status, string(raw))

		master := ctLoadEvent(t, series.ID)
		var rule struct {
			Until string `json:"until"`
		}
		require.NoError(t, json.Unmarshal([]byte(master.Rule.String), &rule))
		require.Equal(t, "2026-11-07", rule.Until, "an all-day series is bounded by a date")
		var exceptions []string
		require.NoError(t, json.Unmarshal([]byte(master.Exceptions.String), &exceptions))
		require.Equal(t, []string{"2026-11-03"}, exceptions, "an all-day series cancels by date")

		rows, err := testDB.QueryContext(t.Context(), `SELECT o.start_at, o.end_at, o.recurrence_original_start
			FROM calendar_events o JOIN calendar_events m ON m.id = o.recurrence_parent_id
			WHERE m.public_id = UUID_TO_BIN(?)`, series.ID)
		require.NoError(t, err)
		defer rows.Close()
		n := 0
		for rows.Next() {
			var start, end, orig time.Time
			require.NoError(t, rows.Scan(&start, &end, &orig))
			n++
			require.Equal(t, ctMidnight("2026-11-12"), start.UTC())
			require.Equal(t, ctMidnight("2026-11-13"), end.UTC())
			require.Equal(t, ctMidnight("2026-11-05"), orig.UTC(), "the override is keyed by the occurrence's UTC midnight")
		}
		require.NoError(t, rows.Err())
		require.Equal(t, 1, n)
	})

	t.Run("timed series keep instants", func(t *testing.T) {
		series := create(map[string]any{"title": "timed", "allDay": false, "timezone": "Asia/Tokyo",
			"startAt": "2026-11-02T10:00:00+09:00", "endAt": "2026-11-02T11:00:00+09:00",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1,
				"until": "2026-11-06T10:00:00+09:00"}})
		listed := ctListEvents(t, calURL, owner.AccessToken, "2026-11-02", "2026-11-10")
		var first ctEvent
		for _, e := range listed.Items {
			if e.Title == "timed" && strings.HasPrefix(e.StartAt, "2026-11-03") {
				first = e
			}
		}
		require.NotEmpty(t, first.ID)
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
			calURL+"/events/"+first.ID+"?scope=this", owner.AccessToken, nil,
			map[string]string{"If-Match": first.Etag})
		require.Equal(t, http.StatusNoContent, status, string(raw))

		master := ctLoadEvent(t, series.ID)
		var exceptions []string
		require.NoError(t, json.Unmarshal([]byte(master.Exceptions.String), &exceptions))
		require.Len(t, exceptions, 1)
		require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$`, exceptions[0],
			"a timed series cancels by RFC 3339 UTC")
		require.False(t, dateOnly.MatchString(exceptions[0]))
		var rule struct {
			Until string `json:"until"`
		}
		require.NoError(t, json.Unmarshal([]byte(master.Rule.String), &rule))
		require.False(t, dateOnly.MatchString(rule.Until), "a timed series is bounded by an instant, got %q", rule.Until)
	})
}

// An override created for an occurrence attendee-wise carries the series' people
// who are not on that occurrence as disabled rows, so the other product reads
// the same occurrence membership.
func TestOverrideWrittenHereDisablesSeriesAttendeesWhoAreNotOnIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, a, "editor")
	joinAs(t, calURL, owner.AccessToken, b, "editor")

	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "sync", "allDay": false,
		"startAt": "2026-06-01T10:00:00Z", "endAt": "2026-06-01T11:00:00Z",
		"participantIds": []string{a.UserID, b.UserID},
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 3},
	}, &series)

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-06-01", "2026-06-30")
	var second ctEvent
	for _, e := range listed.Items {
		if strings.HasPrefix(e.StartAt, "2026-06-02") {
			second = e
		}
	}
	require.NotEmpty(t, second.ID)
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch,
		calURL+"/events/"+second.ID+"?scope=this", owner.AccessToken,
		map[string]any{"participantIds": []string{a.UserID}},
		map[string]string{"If-Match": second.Etag})
	require.Equal(t, http.StatusOK, status, string(raw))

	rows, err := testDB.QueryContext(t.Context(), `SELECT BIN_TO_UUID(u.public_id), at.enabled
		FROM calendar_event_attendees at
		JOIN users u ON u.id = at.user_id
		JOIN calendar_events o ON o.id = at.event_id
		JOIN calendar_events m ON m.id = o.recurrence_parent_id
		WHERE m.public_id = UUID_TO_BIN(?)`, series.ID)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var uid string
		var enabled bool
		require.NoError(t, rows.Scan(&uid, &enabled))
		got[uid] = enabled
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]bool{a.UserID: true, b.UserID: false}, got,
		"the override lists A as present and B as an explicitly disabled row")
}

// Every calendar-scoped log row about an event this product writes names the
// calendar, so another product reading the shared log can scope it.
func TestEventLogRowsCarryTheirCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var e ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "logged", "allDay": true, "startDate": "2026-12-01", "endDate": "2026-12-02"}, &e)
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+e.ID, owner.AccessToken,
		map[string]any{"title": "logged again"}, map[string]string{"If-Match": e.Etag})
	require.Equal(t, http.StatusOK, status, string(raw))

	var total, withCalendar int
	require.NoError(t, testDB.QueryRowContext(t.Context(), `SELECT COUNT(*), COUNT(calendar_id)
		FROM events WHERE subject_public_id = UUID_TO_BIN(?) AND type LIKE 'calendar.%'`, e.ID).
		Scan(&total, &withCalendar))
	require.GreaterOrEqual(t, total, 2, "creation and edit are both logged")
	require.Equal(t, total, withCalendar, "no calendar.* row about an event may have a NULL calendar_id")
}
