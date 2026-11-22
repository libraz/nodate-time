package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/timetree"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

type ttSyncTotals struct{ imported, updated, duplicates, deleted, kept, moved, skipped, failed, rejected int }

var ttSyncTotalLine = regexp.MustCompile(`total: events imported=(\d+) updated=(\d+) duplicates=(\d+) deleted=(\d+) kept=(\d+) moved=(\d+) skipped=(\d+) failed=(\d+) rejected=(\d+)`)

func parseSyncTotals(t *testing.T, out string) ttSyncTotals {
	t.Helper()
	m := ttSyncTotalLine.FindStringSubmatch(out)
	require.NotNil(t, m, "output: %s", out)
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return ttSyncTotals{n(1), n(2), n(3), n(4), n(5), n(6), n(7), n(8), n(9)}
}

func (s *ttScenario) runSync(t *testing.T, dryRun bool) (int, string, error) {
	t.Helper()
	var out strings.Builder
	code, err := timetree.Run(context.Background(), timetree.Options{
		BaseURL:  s.server.URL,
		Email:    "tt@example.com",
		Password: "pw",
		Mapping:  strings.NewReader(s.mapping),
		DryRun:   dryRun,
		Sync:     true,
		DB:       testDB,
		Config:   ttConfig(),
		Out:      &out,
	})
	return code, out.String(), err
}

// syncOK runs a sync that must succeed and returns its totals.
func (s *ttScenario) syncOK(t *testing.T) ttSyncTotals {
	t.Helper()
	code, out, err := s.runSync(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)
	return parseSyncTotals(t, out)
}

// ttRow is the latest row of the calendar carrying a TimeTree ID.
type ttRow struct {
	id                uint32
	publicID, title   string
	start             time.Time
	owner, visibility string
	enabled           bool
	rule, exceptions  sql.NullString
}

func (s *ttScenario) row(t *testing.T, uid string) ttRow {
	t.Helper()
	var r ttRow
	require.NoError(t, testDB.QueryRow(
		`SELECT e.id, BIN_TO_UUID(e.public_id), e.title, e.start_at, u.email, e.visibility, e.enabled,
		        CAST(e.recurrence_rule AS CHAR), CAST(e.recurrence_exceptions AS CHAR)
		 FROM calendar_events e
		 JOIN calendars c ON c.id = e.calendar_id
		 JOIN users u ON u.id = e.owner_user_id
		 WHERE c.name = ? AND e.source_uid = ? AND e.recurrence_parent_id IS NULL
		 ORDER BY e.id DESC LIMIT 1`, s.calName, uid).
		Scan(&r.id, &r.publicID, &r.title, &r.start, &r.owner, &r.visibility, &r.enabled, &r.rule, &r.exceptions))
	return r
}

// snapshot reads every column of one row as text.
func snapshot(t *testing.T, id uint32) []string {
	t.Helper()
	rows, err := testDB.Query(`SELECT * FROM calendar_events WHERE id = ?`, id)
	require.NoError(t, err)
	defer rows.Close()
	cols, err := rows.Columns()
	require.NoError(t, err)
	require.True(t, rows.Next())
	vals := make([]sql.RawBytes, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	require.NoError(t, rows.Scan(ptrs...))
	out := make([]string, len(cols))
	for i, v := range vals {
		out[i] = cols[i] + "=" + string(v)
	}
	return out
}

func (s *ttScenario) logCount(t *testing.T) int {
	return s.count(t, `SELECT COUNT(*) FROM events ev JOIN calendars c ON c.id = ev.calendar_id WHERE c.name = ?`, s.calName)
}

// sourcedLogs counts one event's log entries of a type that name the importer.
func (s *ttScenario) sourcedLogs(t *testing.T, eventID uint32, typ string) int {
	return s.count(t, `SELECT COUNT(*) FROM events WHERE type = ?
		AND subject_public_id = (SELECT public_id FROM calendar_events WHERE id = ?)
		AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.source')) = 'timetree-import'`, typ, eventID)
}

func (s *ttScenario) liveEvents(t *testing.T, calName string) int {
	return s.count(t, `SELECT COUNT(*) FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
		WHERE c.name = ? AND e.enabled = TRUE`, calName)
}

func (s *ttScenario) overrides(t *testing.T, parentID uint32) int {
	return s.count(t, `SELECT COUNT(*) FROM calendar_events WHERE recurrence_parent_id = ?`, parentID)
}

// editOccurrence changes one occurrence of a series through the API, which
// stores it as an override row.
func (s *ttScenario) editOccurrence(t *testing.T, seriesTitle string) {
	t.Helper()
	calURL := testServerURL + "/calendars/" + s.calendarID(t)
	var occ []recInstance
	getItems(t, http.MethodGet, calURL+"/events?start=2026-05-01&end=2026-06-30&tz=Asia/Tokyo", s.token, nil, &occ)
	for _, o := range occ {
		if o.Title != seriesTitle || !strings.Contains(o.ID, "_") {
			continue
		}
		start, err := time.Parse(time.RFC3339, o.StartAt)
		require.NoError(t, err)
		status, raw := sendAnyVersion(t, http.MethodPatch, calURL+"/events/"+o.ID+"?scope=this", s.token,
			eventBody("changed here", o.StartAt, start.Add(30*time.Minute).Format(time.RFC3339)))
		require.Equal(t, http.StatusOK, status, "%s", raw)
		return
	}
	t.Fatalf("no occurrence of %q in %+v", seriesTitle, occ)
}

var (
	syncTimedAt = time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC)
	syncRecAt   = time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC) // a Monday, 10:00 in Tokyo
)

// syncEvents is a labelled event, an unlabelled weekly series with a
// cancelled week, and an unlabelled single event.
func syncEvents() []map[string]any {
	return []map[string]any{
		ttTimed("s-timed", "timed", syncTimedAt, 11),
		withRecurrences(ttTimed("s-rec", "weekly", syncRecAt, 0),
			"RRULE:FREQ=WEEKLY;COUNT=6", "EXDATE:20260511T010000Z"),
		ttTimed("s-plain", "plain", time.Date(2026, 5, 2, 1, 0, 0, 0, time.UTC), 0),
	}
}

// edited copies events, applying change to the one with the given ID.
func edited(events []map[string]any, id string, change func(map[string]any)) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		c := make(map[string]any, len(e))
		for k, v := range e {
			c[k] = v
		}
		if c["id"] == id {
			change(c)
		}
		out = append(out, c)
	}
	return out
}

func without(events []map[string]any, id string) []map[string]any {
	var out []map[string]any
	for _, e := range events {
		if e["id"] != id {
			out = append(out, e)
		}
	}
	return out
}

func TestTimeTreeSync(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	t.Run("changes", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		require.Equal(t, ttSyncTotals{imported: 3}, s.syncOK(t))

		moved := syncTimedAt.Add(2 * time.Hour)
		feed := edited(syncEvents(), "s-timed", func(e map[string]any) {
			e["title"], e["label_id"] = "timed v2", 12
			e["start_at"], e["end_at"] = moved.UnixMilli(), moved.Add(time.Hour).UnixMilli()
		})
		feed = edited(feed, "s-rec", func(e map[string]any) {
			e["label_id"] = 11
			e["recurrences"] = []string{"RRULE:FREQ=WEEKLY;COUNT=8", "EXDATE:20260518T010000Z"}
		})
		s.setFeed(9001, feed)
		require.Equal(t, ttSyncTotals{updated: 2, duplicates: 1}, s.syncOK(t))

		timed := s.row(t, "s-timed")
		assert.Equal(t, "timed v2", timed.title)
		assert.True(t, moved.Equal(timed.start), "start %s", timed.start)
		assert.Equal(t, s.mgr, timed.owner)
		assert.Empty(t, s.eventAttendees(t, "s-timed"))
		assert.Equal(t, 1, s.sourcedLogs(t, timed.id, "calendar.event.updated"))

		rec := s.row(t, "s-rec")
		assert.Equal(t, s.hanako, rec.owner)
		assert.Contains(t, rec.rule.String, `"count": 8`)
		assert.Contains(t, rec.exceptions.String, "2026-05-18")
		assert.NotContains(t, rec.exceptions.String, "2026-05-11")
		assert.Empty(t, s.eventAttendees(t, "s-rec"), "a person's label drops the default attendees")
		assert.Equal(t, 1, s.sourcedLogs(t, rec.id, "calendar.event.updated"))
		assert.Zero(t, s.sourcedLogs(t, s.row(t, "s-plain").id, "calendar.event.updated"))
	})

	t.Run("creator", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		require.Equal(t, s.importer, s.eventCreator(t, "s-plain"))

		s.setFeed(9001, edited(syncEvents(), "s-plain", func(e map[string]any) { e["author_id"] = 501 }))
		require.Equal(t, ttSyncTotals{updated: 1, duplicates: 2}, s.syncOK(t))
		assert.Equal(t, s.hanako, s.eventCreator(t, "s-plain"), "a sync records the mapped author as creator")
		assert.Equal(t, s.importer, s.eventCreator(t, "s-timed"))
	})

	t.Run("overwrite", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)

		timed := s.row(t, "s-timed")
		body := eventBody("renamed here", "2026-05-01T10:00:00+09:00", "2026-05-01T11:00:00+09:00")
		body["visibility"] = "private"
		status, raw := sendAnyVersion(t, http.MethodPatch,
			testServerURL+"/calendars/"+s.calendarID(t)+"/events/"+timed.publicID, s.token, body)
		require.Equal(t, http.StatusOK, status, "%s", raw)
		s.editOccurrence(t, "weekly")
		rec := s.row(t, "s-rec")
		require.Positive(t, s.overrides(t, rec.id))

		got := s.syncOK(t)
		assert.Equal(t, 2, got.updated, "%+v", got)
		timed = s.row(t, "s-timed")
		assert.Equal(t, "timed", timed.title, "TimeTree's title wins")
		assert.Equal(t, "private", timed.visibility, "a column TimeTree has no value for is kept")
		assert.Zero(t, s.overrides(t, rec.id), "the override is dropped")
	})

	t.Run("rule-off", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		s.editOccurrence(t, "weekly")
		rec := s.row(t, "s-rec")
		require.Positive(t, s.overrides(t, rec.id))

		s.setFeed(9001, edited(syncEvents(), "s-rec", func(e map[string]any) { delete(e, "recurrences") }))
		assert.Equal(t, 1, s.syncOK(t).updated)
		rec = s.row(t, "s-rec")
		assert.False(t, rec.rule.Valid, "rule %q", rec.rule.String)
		assert.False(t, rec.exceptions.Valid, "exceptions %q", rec.exceptions.String)
		assert.Zero(t, s.overrides(t, rec.id))
	})

	t.Run("deletes", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		plain, timed := s.row(t, "s-plain"), s.row(t, "s-timed")

		feed := edited(without(syncEvents(), "s-plain"), "s-timed", func(e map[string]any) { e["deleted_at"] = 1700000000000 })
		s.setFeed(9001, feed)
		got := s.syncOK(t)
		assert.Equal(t, 2, got.deleted, "%+v", got)
		for _, r := range []ttRow{plain, timed} {
			var enabled bool
			require.NoError(t, testDB.QueryRow(`SELECT enabled FROM calendar_events WHERE id = ?`, r.id).Scan(&enabled))
			assert.False(t, enabled, r.title)
			assert.Equal(t, 1, s.sourcedLogs(t, r.id, "calendar.event.deleted"), r.title)
		}
		assert.Equal(t, 1, s.liveEvents(t, s.calName))
	})

	t.Run("untouched", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		calURL := testServerURL + "/calendars/" + s.calendarID(t)

		var mine struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", s.token,
			eventBody("made here", "2026-05-03T10:00:00+09:00", "2026-05-03T11:00:00+09:00"), &mine)
		ics := wrap(vevent("UID:ics-only@example.com", "SUMMARY:from ics",
			"DTSTART:20260505T010000Z", "DTEND:20260505T020000Z") +
			vevent("UID:s-shared", "SUMMARY:shared from ics",
				"DTSTART:20260506T010000Z", "DTEND:20260506T020000Z"))
		require.Equal(t, 2, importOK(t, calURL, s.token, ics).Imported)

		var ids []uint32
		for _, q := range []struct{ where, arg string }{
			{"BIN_TO_UUID(e.public_id) = ?", mine.ID},
			{"e.source_uid = ?", "ics-only@example.com"},
			{"e.source_uid = ?", "s-shared"},
		} {
			var id uint32
			require.NoError(t, testDB.QueryRow(`SELECT e.id FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
				WHERE c.name = ? AND `+q.where, s.calName, q.arg).Scan(&id))
			ids = append(ids, id)
		}
		before := make([][]string, len(ids))
		for i, id := range ids {
			before[i] = snapshot(t, id)
		}

		s.setFeed(9001, append(syncEvents(), ttTimed("s-shared", "shared from TimeTree", syncTimedAt, 0)))
		got := s.syncOK(t)
		assert.Equal(t, ttSyncTotals{duplicates: 3, kept: 1}, got)
		for i, id := range ids {
			assert.Equal(t, before[i], snapshot(t, id))
		}
	})

	t.Run("following", func(t *testing.T) {
		t.Parallel()
		occurrences := func(s *ttScenario) []flEvent {
			var out []flEvent
			for _, it := range flList(t, testServerURL+"/calendars/"+s.calendarID(t), s.token) {
				if it.RecurrenceDate != nil {
					out = append(out, it)
				}
			}
			return out
		}
		split := func(s *ttScenario) {
			occ := occurrences(s)[2]
			status, raw, _ := flPatch(t, testServerURL+"/calendars/"+s.calendarID(t), s.token, occ, "following",
				occ.SeriesEtag, map[string]any{"title": "cut here"})
			require.Equal(t, http.StatusOK, status, "%s", raw)
		}

		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		want := flTitles(occurrences(s))
		split(s)
		got := s.syncOK(t)
		assert.Equal(t, 1, got.updated, "%+v", got)
		assert.Equal(t, want, flTitles(occurrences(s)), "the source's series replaces the cut")

		split(s)
		s.setFeed(9001, without(syncEvents(), "s-rec"))
		assert.Equal(t, 1, s.syncOK(t).deleted)
		assert.Empty(t, occurrences(s), "the series cut from a deleted one goes with it")
	})

	t.Run("kept", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		rec := s.row(t, "s-rec")
		before := snapshot(t, rec.id)

		s.setFeed(9001, edited(syncEvents(), "s-rec", func(e map[string]any) {
			e["title"] = "now unreadable"
			e["recurrences"] = []string{"RRULE:FREQ=WEEKLY;COUNT=6", "EXDATE:garbage"}
		}))
		assert.Equal(t, ttSyncTotals{duplicates: 2, kept: 1}, s.syncOK(t))
		assert.Equal(t, before, snapshot(t, rec.id))
	})

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, append(ttEvents(), syncEvents()...))
		first := s.syncOK(t)
		require.Equal(t, 6, first.imported, "%+v", first)
		logs := s.logCount(t)

		second := s.syncOK(t)
		assert.Equal(t, ttSyncTotals{duplicates: 6, skipped: first.skipped}, second)
		assert.Equal(t, logs, s.logCount(t))
	})

	t.Run("guard", func(t *testing.T) {
		t.Parallel()
		var twelve []map[string]any
		for i := range 12 {
			twelve = append(twelve, ttTimed(fmt.Sprintf("g-%d", i), "g", syncTimedAt.Add(time.Duration(i)*time.Hour), 0))
		}
		s := newTTScenario(t, twelve)
		require.Equal(t, 12, s.syncOK(t).imported)
		logs := s.logCount(t)

		for name, feed := range map[string][]map[string]any{"11 of 12": twelve[:1], "all": nil} {
			s.setFeed(9001, feed)
			for _, dryRun := range []bool{true, false} {
				code, out, err := s.runSync(t, dryRun)
				require.Error(t, err, name)
				require.Equal(t, 2, code, "%s: %s", name, out)
				if feed == nil {
					assert.Contains(t, out, fmt.Sprintf(`refusing: TimeTree returned no events for calendar %q but 12 are synced`, s.calName))
				} else {
					assert.Contains(t, out, fmt.Sprintf(`refusing: calendar %q would delete 11 of 12 synced events`, s.calName))
				}
				assert.Equal(t, 12, s.liveEvents(t, s.calName), name)
				assert.Equal(t, logs, s.logCount(t), name)
			}
		}
	})

	t.Run("moved", func(t *testing.T) {
		t.Parallel()
		var eleven []map[string]any
		for i := range 11 {
			eleven = append(eleven, ttTimed(fmt.Sprintf("m-%d", i), "m", syncTimedAt.Add(time.Duration(i)*time.Hour), 0))
		}
		s := newTTScenario(t, eleven)
		other := s.calName + "-B"
		s.mu.Lock()
		s.extraCals = []map[string]any{{"id": 9003, "name": other, "alias_code": "b-" + s.calName, "deactivated_at": nil}}
		s.mu.Unlock()
		s.mapping += fmt.Sprintf("  - source: %s\n", other)
		require.Equal(t, 11, s.syncOK(t).imported)

		s.setFeed(9001, nil)
		s.setFeed(9003, eleven)
		code, out, err := s.runSync(t, false)
		require.NoError(t, err, out)
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, fmt.Sprintf(`calendar %q (reused): members added=0 existing=2; events imported=0 updated=0 duplicates=0 deleted=11 kept=0 moved=11`, s.calName))
		assert.Contains(t, out, fmt.Sprintf(`calendar %q (reused): members added=0 existing=0; events imported=11 `, other))
		assert.Equal(t, ttSyncTotals{imported: 11, deleted: 11, moved: 11}, parseSyncTotals(t, out))
		assert.Zero(t, s.liveEvents(t, s.calName))
		assert.Equal(t, 11, s.liveEvents(t, other))
	})

	t.Run("repeated", func(t *testing.T) {
		t.Parallel()
		// Two chunks of two: r-1 appears in both, newer in the second.
		s := newTTScenario(t, []map[string]any{
			ttTimed("r-1", "old", syncTimedAt, 0),
			ttTimed("r-2", "two", syncTimedAt, 0),
			ttTimed("r-1", "new", syncTimedAt, 0),
			ttTimed("r-3", "three", syncTimedAt, 0),
		})
		code, out, err := s.runSync(t, true)
		require.NoError(t, err, out)
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, fmt.Sprintf(`calendar %q: sync plan events create=3 update=0 unchanged=0 delete=0 moved=0 kept=0 synced=0 orphaned=0 repeated=1`, s.calName))

		assert.Equal(t, 3, s.syncOK(t).imported)
		assert.Equal(t, "new", s.row(t, "r-1").title)
		assert.Equal(t, 1, s.count(t, `SELECT COUNT(*) FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
			WHERE c.name = ? AND e.source_uid = 'r-1'`, s.calName))
	})

	t.Run("dry-run", func(t *testing.T) {
		t.Parallel()
		s := newTTScenario(t, syncEvents())
		s.syncOK(t)
		timed := s.row(t, "s-timed")
		before, logs := snapshot(t, timed.id), s.logCount(t)

		feed := edited(without(syncEvents(), "s-plain"), "s-timed", func(e map[string]any) { e["title"] = "renamed in TimeTree" })
		s.setFeed(9001, append(feed, ttTimed("s-new", "new", syncTimedAt, 0)))
		code, out, err := s.runSync(t, true)
		require.NoError(t, err, out)
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, fmt.Sprintf(`calendar %q: sync plan events create=1 update=1 unchanged=1 delete=1 moved=0 kept=0 synced=3 orphaned=0 repeated=0`, s.calName))
		assert.NotContains(t, out, "total:")

		assert.Equal(t, before, snapshot(t, timed.id))
		assert.Equal(t, logs, s.logCount(t))
		assert.Equal(t, 3, s.liveEvents(t, s.calName))
	})
}
