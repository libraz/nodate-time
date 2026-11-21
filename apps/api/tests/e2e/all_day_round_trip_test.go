package e2e

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// adErr is the machine-readable part of an error response.
type adErr struct {
	Code string `json:"code"`
}

func adCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e adErr
	require.NoError(t, json.Unmarshal(raw, &e), string(raw))
	return e.Code
}

// An all-day event written with dates reads back as the same dates, in every
// read path and whichever zone it was created in, and is stored as UTC
// midnights with an exclusive end.
func TestAllDayDatesRoundTrip(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	cases := []struct {
		name, tz, start, end string
	}{
		{"one day, east", "Asia/Tokyo", "2026-08-05", "2026-08-06"},
		{"one day, west", "America/Los_Angeles", "2026-08-05", "2026-08-06"},
		{"far east zone", "Pacific/Kiritimati", "2026-08-05", "2026-08-06"},
		{"several days over a month end", "Europe/London", "2026-08-30", "2026-09-03"},
		{"year end", "UTC", "2026-12-31", "2027-01-01"},
		{"leap day", "Asia/Tokyo", "2028-02-29", "2028-03-01"},
		{"date with no local midnight", "America/Sao_Paulo", "2018-11-04", "2018-11-05"},
		{"dst change day", "America/Los_Angeles", "2026-03-08", "2026-03-09"},
		{"end date at the upper bound is accepted", "UTC", "9999-12-30", "9999-12-31"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var created ctEvent
			helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
				"title": c.name, "allDay": true, "timezone": c.tz,
				"startDate": c.start, "endDate": c.end}, &created)
			require.True(t, created.AllDay)
			require.Equal(t, c.start, created.StartDate)
			require.Equal(t, c.end, created.EndDate)
			require.Equal(t, c.start+"T00:00:00Z", created.StartAt)
			require.Equal(t, c.end+"T00:00:00Z", created.EndAt)

			var got ctEvent
			helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+created.ID, owner.AccessToken, nil, &got)
			require.Equal(t, c.start, got.StartDate)
			require.Equal(t, c.end, got.EndDate)

			stored := ctLoadEvent(t, created.ID)
			require.True(t, stored.AllDay)
			require.Equal(t, ctMidnight(c.start), stored.Start)
			require.Equal(t, ctMidnight(c.end), stored.End)

			window := ctListEvents(t, calURL, owner.AccessToken, c.start, c.end)
			var found bool
			for _, e := range window.Items {
				if e.ID == created.ID {
					found = true
					require.Equal(t, c.start, e.StartDate)
					require.Equal(t, c.end, e.EndDate)
				}
			}
			require.True(t, found, "listed in its own window")
		})
	}
}

// The reader's own zone never shifts an all-day date.
func TestAllDayDatesDoNotShiftWithTheReadersZone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	var created ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "holiday", "allDay": true, "timezone": "Asia/Tokyo",
		"startDate": "2026-08-05", "endDate": "2026-08-06"}, &created)

	for _, tz := range []string{"Pacific/Honolulu", "Asia/Tokyo", "Pacific/Kiritimati"} {
		var out ctEventList
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-08-05&end=2026-08-05&tz="+tz,
			owner.AccessToken, nil, &out)
		require.Len(t, out.Items, 1, tz)
		require.Equal(t, "2026-08-05", out.Items[0].StartDate, tz)
		require.Equal(t, "2026-08-06", out.Items[0].EndDate, tz)
	}
}

// Wire-shape and value errors on create, with the codes a client can branch on.
func TestAllDayCreateRejectsBadShapesAndDates(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"all-day with instants", map[string]any{"title": "x", "allDay": true,
			"startAt": "2026-08-05T00:00:00Z", "endAt": "2026-08-06T00:00:00Z"}, 400, "EVENT.ALL_DAY_SHAPE"},
		{"all-day with both forms", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-08-05", "endDate": "2026-08-06",
			"startAt": "2026-08-05T00:00:00Z", "endAt": "2026-08-06T00:00:00Z"}, 400, "EVENT.ALL_DAY_SHAPE"},
		{"timed with dates", map[string]any{"title": "x", "allDay": false,
			"startDate": "2026-08-05", "endDate": "2026-08-06"}, 400, "EVENT.ALL_DAY_SHAPE"},
		{"end equals start", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-08-05", "endDate": "2026-08-05"}, 400, "EVENT.DATE_RANGE_INVALID"},
		{"end before start", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-08-05", "endDate": "2026-08-04"}, 400, "EVENT.DATE_RANGE_INVALID"},
		{"no such day", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-02-30", "endDate": "2026-03-02"}, 400, "EVENT.DATE_INVALID"},
		{"year zero", map[string]any{"title": "x", "allDay": true,
			"startDate": "0000-01-01", "endDate": "0000-01-02"}, 400, "EVENT.DATE_INVALID"},
		{"before year 1000", map[string]any{"title": "x", "allDay": true,
			"startDate": "0999-12-31", "endDate": "1000-01-01"}, 400, "EVENT.DATE_INVALID"},
		{"unpadded date", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-8-5", "endDate": "2026-08-06"}, 422, "REQUEST.VALIDATION"},
		{"instant where a date belongs", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-08-05T00:00:00Z", "endDate": "2026-08-06"}, 422, "REQUEST.VALIDATION"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken, c.body)
			require.Equal(t, c.status, status, string(raw))
			require.Equal(t, c.code, adCode(t, raw), string(raw))
		})
	}
}

// PATCH keeps the end it was not sent and checks the order afterwards; a
// switch of allDay needs both ends of the new form.
func TestAllDayPatchEdgesAndSwitching(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	create := func() ctEvent {
		var e ctEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
			"title": "trip", "allDay": true, "startDate": "2026-09-10", "endDate": "2026-09-12"}, &e)
		return e
	}
	patch := func(e ctEvent, body map[string]any) (int, []byte) {
		return helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+e.ID, owner.AccessToken,
			body, map[string]string{"If-Match": e.Etag})
	}

	t.Run("end only, later than the stored start", func(t *testing.T) {
		e := create()
		status, raw := patch(e, map[string]any{"endDate": "2026-09-15"})
		require.Equal(t, http.StatusOK, status, string(raw))
		var got ctEvent
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, "2026-09-10", got.StartDate)
		require.Equal(t, "2026-09-15", got.EndDate)
		stored := ctLoadEvent(t, e.ID)
		require.Equal(t, ctMidnight("2026-09-10"), stored.Start)
		require.Equal(t, ctMidnight("2026-09-15"), stored.End)
	})

	t.Run("end only, not after the stored start", func(t *testing.T) {
		e := create()
		for _, end := range []string{"2026-09-10", "2026-09-09"} {
			status, raw := patch(e, map[string]any{"endDate": end})
			require.Equal(t, http.StatusBadRequest, status, string(raw))
			require.Equal(t, "EVENT.DATE_RANGE_INVALID", adCode(t, raw))
		}
		stored := ctLoadEvent(t, e.ID)
		require.Equal(t, ctMidnight("2026-09-12"), stored.End, "a refused edit changes nothing")
	})

	t.Run("start only moves past the stored end", func(t *testing.T) {
		e := create()
		status, raw := patch(e, map[string]any{"startDate": "2026-09-20"})
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "EVENT.DATE_RANGE_INVALID", adCode(t, raw))
	})

	t.Run("all-day to timed needs both instants", func(t *testing.T) {
		e := create()
		status, raw := patch(e, map[string]any{"allDay": false})
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "EVENT.ALL_DAY_SHAPE", adCode(t, raw))

		status, raw = patch(e, map[string]any{"allDay": false,
			"startAt": "2026-09-10T10:00:00+09:00", "endAt": "2026-09-10T11:00:00+09:00"})
		require.Equal(t, http.StatusOK, status, string(raw))
		stored := ctLoadEvent(t, e.ID)
		require.False(t, stored.AllDay)
		require.Equal(t, "2026-09-10T01:00:00Z", stored.Start.Format("2006-01-02T15:04:05Z"))
	})

	t.Run("timed to all-day needs both dates", func(t *testing.T) {
		var e ctEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
			"title": "call", "allDay": false,
			"startAt": "2026-09-10T10:00:00+09:00", "endAt": "2026-09-10T11:00:00+09:00"}, &e)
		status, raw := patch(e, map[string]any{"allDay": true, "startDate": "2026-09-10"})
		require.Equal(t, http.StatusBadRequest, status, string(raw))

		status, raw = patch(e, map[string]any{"allDay": true, "startDate": "2026-09-10", "endDate": "2026-09-11"})
		require.Equal(t, http.StatusOK, status, string(raw))
		stored := ctLoadEvent(t, e.ID)
		require.True(t, stored.AllDay)
		require.Equal(t, ctMidnight("2026-09-10"), stored.Start)
		require.Equal(t, ctMidnight("2026-09-11"), stored.End)
	})

	t.Run("a stored milestone accepts end equal to start", func(t *testing.T) {
		id, _ := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
			Title: "milestone", Kind: "milestone", AllDay: true,
			Start: "2026-09-20 00:00:00", End: "2026-09-20 00:00:00"})
		var e ctEvent
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, owner.AccessToken, nil, &e)
		status, raw := patch(e, map[string]any{"title": "renamed", "startDate": "2026-09-21", "endDate": "2026-09-21"})
		require.Equal(t, http.StatusOK, status, string(raw))
		stored := ctLoadEvent(t, id)
		require.Equal(t, ctMidnight("2026-09-21"), stored.Start)
		require.Equal(t, stored.Start, stored.End)
	})
}

// An all-day series bounded by a date, with one occurrence cancelled by date:
// the occurrences read back as the same dates, the stored array holds the date,
// and undoing the cancellation restores the day.
func TestAllDaySeriesWithADateException(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "retreat", "allDay": true, "timezone": "America/Los_Angeles",
		"startDate": "2026-03-06", "endDate": "2026-03-07",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-03-10"}}, &series)

	datesOf := func() []string {
		var out []string
		for _, e := range ctListEvents(t, calURL, owner.AccessToken, "2026-03-01", "2026-03-31").Items {
			require.Equal(t, e.StartDate+"T00:00:00Z", e.StartAt, "each occurrence starts at UTC midnight")
			out = append(out, e.StartDate)
		}
		sort.Strings(out)
		return out
	}
	require.Equal(t, []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10"}, datesOf(),
		"a date until includes its own day, across the US spring-forward")

	var target ctEvent
	for _, e := range ctListEvents(t, calURL, owner.AccessToken, "2026-03-08", "2026-03-08").Items {
		target = e
	}
	require.Equal(t, "2026-03-08", target.StartDate)
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+target.ID+"?scope=this", owner.AccessToken, nil,
		map[string]string{"If-Match": target.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))

	require.Equal(t, []string{"2026-03-06", "2026-03-07", "2026-03-09", "2026-03-10"}, datesOf())
	stored := ctLoadEvent(t, series.ID)
	var exceptions []string
	require.NoError(t, json.Unmarshal([]byte(stored.Exceptions.String), &exceptions))
	require.Equal(t, []string{"2026-03-08"}, exceptions)

	var master ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, &master)
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+series.ID+"/exceptions/2026-03-08", owner.AccessToken, nil,
		map[string]string{"If-Match": master.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))
	require.Equal(t, []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10"}, datesOf())
	stored = ctLoadEvent(t, series.ID)
	require.JSONEq(t, `[]`, stored.Exceptions.String)
}

// Undoing a cancellation removes every spelling of the day an older writer may
// have left, and answers 404 for a day that was never cancelled.
func TestExceptionUndoRemovesEverySpellingOfTheDay(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	id, _ := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "legacy", AllDay: true, Timezone: "UTC",
		Start: "2026-04-01 00:00:00", End: "2026-04-02 00:00:00",
		Rule:       `{"freq":"daily","interval":1,"count":5}`,
		Exceptions: `["2026-04-03","2026-04-03T00:00:00Z","2026-04-03T00:00:00","2026-04-04","??"]`,
	})
	t.Cleanup(func() {
		_, err := testDB.Exec(`DELETE FROM calendar_events WHERE public_id = UUID_TO_BIN(?)`, id)
		require.NoError(t, err)
	})
	var master ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, owner.AccessToken, nil, &master)

	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+id+"/exceptions/2026-04-03", owner.AccessToken, nil,
		map[string]string{"If-Match": master.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))
	var exceptions []string
	require.NoError(t, json.Unmarshal([]byte(ctLoadEvent(t, id).Exceptions.String), &exceptions))
	require.Equal(t, []string{"2026-04-04", "??"}, exceptions,
		"all three spellings go; other days and unreadable entries stay")

	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, owner.AccessToken, nil, &master)
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+id+"/exceptions/2026-04-03", owner.AccessToken, nil,
		map[string]string{"If-Match": master.Etag})
	require.Equal(t, http.StatusNotFound, status, string(raw))
	require.Equal(t, "EVENT.EXCEPTION_NOT_FOUND", adCode(t, raw))
}

// The series bound must be in the series' own form.
func TestSeriesBoundFormFollowsTheSeriesKind(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"all-day with an instant", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-09-01", "endDate": "2026-09-02",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-05T00:00:00Z"}},
			400, "EVENT.OCCURRENCE_FORM"},
		{"timed with a date", map[string]any{"title": "x", "allDay": false,
			"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-05"}},
			400, "EVENT.OCCURRENCE_FORM"},
		{"all-day with a date", map[string]any{"title": "x", "allDay": true,
			"startDate": "2026-09-01", "endDate": "2026-09-02",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-05"}}, 201, ""},
		{"timed with an instant", map[string]any{"title": "x", "allDay": false,
			"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-09-05T10:00:00Z"}}, 201, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken, c.body)
			require.Equal(t, c.status, status, string(raw))
			if c.code != "" {
				require.Equal(t, c.code, adCode(t, raw))
			}
		})
	}
}

// Switching a whole series between timed and all-day rewrites the keys the
// series carries, in one step: the bound, the exceptions, the override keys.
func TestSwitchingASeriesBetweenTimedAndAllDayRewritesItsKeys(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "switch", "allDay": false, "timezone": "Asia/Tokyo",
		"startAt": "2026-07-01T10:00:00+09:00", "endAt": "2026-07-01T11:00:00+09:00",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-07-06T10:00:00+09:00"}}, &series)

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-06-30", "2026-07-31").Items
	require.Len(t, listed, 6)
	find := func(prefixUTC string) ctEvent {
		for _, e := range listed {
			if strings.HasPrefix(e.StartAt, prefixUTC) {
				return e
			}
		}
		t.Fatalf("no occurrence at %s", prefixUTC)
		return ctEvent{}
	}
	// 10:00 JST is 01:00Z on the same calendar day.
	cancel := find("2026-07-02")
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+cancel.ID+"?scope=this", owner.AccessToken, nil, map[string]string{"If-Match": cancel.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))
	moved := find("2026-07-04")
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch,
		calURL+"/events/"+moved.ID+"?scope=this", owner.AccessToken,
		map[string]any{"title": "moved", "startAt": "2026-07-04T15:00:00+09:00", "endAt": "2026-07-04T16:00:00+09:00"},
		map[string]string{"If-Match": moved.Etag})
	require.Equal(t, http.StatusOK, status, string(raw))

	var master ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, &master)
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+series.ID, owner.AccessToken,
		map[string]any{"allDay": true, "startDate": "2026-07-01", "endDate": "2026-07-02",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-07-06"}},
		map[string]string{"If-Match": master.Etag})
	require.Equal(t, http.StatusOK, status, string(raw))

	stored := ctLoadEvent(t, series.ID)
	require.True(t, stored.AllDay)
	var exceptions []string
	require.NoError(t, json.Unmarshal([]byte(stored.Exceptions.String), &exceptions))
	require.Equal(t, []string{"2026-07-02"}, exceptions, "the timed cancellation becomes the date in the series zone")

	var origStart string
	require.NoError(t, testDB.QueryRowContext(t.Context(), `SELECT DATE_FORMAT(o.recurrence_original_start, '%Y-%m-%d %H:%i:%s')
		FROM calendar_events o JOIN calendar_events m ON m.id = o.recurrence_parent_id
		WHERE m.public_id = UUID_TO_BIN(?) AND o.enabled`, series.ID).Scan(&origStart))
	require.Equal(t, "2026-07-04 00:00:00", origStart, "the override is re-keyed to the occurrence's date")
}
