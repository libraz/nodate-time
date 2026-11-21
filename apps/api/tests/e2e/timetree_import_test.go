package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/timetree"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

// ttScenario is one test's fake TimeTree, mapping file and unique names. The
// importer is an account registered through the API so the test can read the
// imported calendar back over HTTP.
type ttScenario struct {
	calName  string
	importer string
	hanako   string
	mgr      string
	token    string
	server   *httptest.Server
	mapping  string

	mu sync.Mutex
	// feeds is each TimeTree calendar's events by id; extraCals are live
	// calendars served after the first.
	feeds     map[int][]map[string]any
	extraCals []map[string]any
}

// setFeed replaces what the fake TimeTree serves for one calendar.
func (s *ttScenario) setFeed(calendarID int, events []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeds[calendarID] = events
}

func (s *ttScenario) feed(calendarID int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feeds[calendarID]
}

var tokyoLoc = func() *time.Location {
	l, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return l
}()

func ttTimed(id, title string, start time.Time, label int) map[string]any {
	e := map[string]any{
		"id": id, "title": title, "all_day": false, "type": 0, "category": 1,
		"start_at": start.UnixMilli(), "end_at": start.Add(time.Hour).UnixMilli(),
		"start_timezone": "Asia/Tokyo", "end_timezone": "Asia/Tokyo",
	}
	if label != 0 {
		e["label_id"] = label
	}
	return e
}

func ttAllDay(id, title string, start time.Time, tz string) map[string]any {
	return map[string]any{
		"id": id, "title": title, "all_day": true, "type": 0, "category": 1,
		"start_at": start.UnixMilli(), "end_at": start.UnixMilli(), "start_timezone": tz,
	}
}

func withRecurrences(e map[string]any, rows ...string) map[string]any {
	e["recurrences"] = rows
	return e
}

// ttEvents is the event set most tests share: three importable events, one of
// them an all-day weekly series with a cancelled week and an UNTIL day, and
// four that are skipped (memo, birthday, RDATE, unsupported repeat).
func ttEvents() []map[string]any {
	d := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	auckland := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC) // 04-06 00:00 in Auckland
	memo := ttTimed("e-memo", "memo", time.Date(2026, 4, 1, 1, 0, 0, 0, time.UTC), 0)
	memo["category"] = 2
	birthday := ttTimed("e-birthday", "birthday", time.Date(2026, 4, 2, 1, 0, 0, 0, time.UTC), 0)
	birthday["type"] = 1
	return []map[string]any{
		ttTimed("e-timed", "timed", time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC), 11),
		ttAllDay("e-allday", "allday", d, "UTC"),
		withRecurrences(ttAllDay("e-rec", "rec", auckland, "Pacific/Auckland"),
			"RRULE:FREQ=WEEKLY;UNTIL=20260426T120000Z", "EXDATE:20260413"),
		memo,
		birthday,
		withRecurrences(ttTimed("e-rdate", "rdate", time.Date(2026, 4, 3, 1, 0, 0, 0, time.UTC), 0),
			"RRULE:FREQ=DAILY;COUNT=2", "RDATE:20260501T000000Z"),
		withRecurrences(ttTimed("e-hourly", "hourly", time.Date(2026, 4, 4, 1, 0, 0, 0, time.UTC), 0),
			"RRULE:FREQ=HOURLY;COUNT=3"),
	}
}

// newTTScenario starts a fake TimeTree holding one calendar with the given
// events and two labels (11 and 12).
func newTTScenario(t *testing.T, events []map[string]any) *ttScenario {
	t.Helper()
	sfx := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	tenant := helpers.NewTenant(t, testServerURL)
	s := &ttScenario{
		calName:  "TT-" + sfx,
		importer: tenant.Email,
		hanako:   "hanako-" + sfx + "@test.local",
		mgr:      "mgr-" + sfx + "@test.local",
		token:    tenant.AccessToken,
		feeds:    map[int][]map[string]any{9001: events},
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		switch {
		case r.URL.Path == "/api/v1/auth/email/signin":
			http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "s", Path: "/"})
			reply(map[string]any{})
		case r.URL.Path == "/api/v1/calendars":
			s.mu.Lock()
			cals := append([]map[string]any{
				{"id": 9001, "name": s.calName, "alias_code": "al-" + sfx, "deactivated_at": nil},
			}, s.extraCals...)
			s.mu.Unlock()
			reply(map[string]any{"calendars": append(cals,
				map[string]any{"id": 9002, "name": "stopped-" + sfx, "alias_code": "st-" + sfx, "deactivated_at": 1700000000000})})
		case strings.HasSuffix(r.URL.Path, "/labels"):
			reply(map[string]any{"calendar_labels": []map[string]any{
				{"id": 11, "name": "hana"}, {"id": 12, "name": "mgr"},
			}})
		case strings.HasSuffix(r.URL.Path, "/events/sync"):
			id, _ := strconv.Atoi(strings.Split(r.URL.Path, "/")[4])
			events := s.feed(id)
			// Two chunks, so the cursor is exercised.
			half := len(events) / 2
			if r.URL.Query().Get("since") == "" {
				reply(map[string]any{"events": events[:half], "since": 1, "chunk": true})
				return
			}
			reply(map[string]any{"events": events[half:], "since": 2, "chunk": false})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	s.mapping = fmt.Sprintf(`
importer: %s
users:
  - email: %s
    name: 山田花子
  - email: %s
calendars:
  - timetree: %s
    default_owner: %s
    default_attendees: [%s, %s]
    members:
      - { email: %s, role: viewer }
      - { email: %s, role: manager }
    labels:
      hana: %s
      mgr: %s
`, s.importer, s.hanako, s.mgr, s.calName, s.mgr, s.hanako, s.mgr, s.hanako, s.mgr, s.hanako, s.mgr)
	return s
}

func ttConfig() *config.Config {
	return &config.Config{
		WorkspaceSlug:     helpers.TestWorkspaceSlug,
		WorkspaceName:     "Nodate Time (test)",
		WorkspaceTimezone: "Asia/Tokyo",
		WorkspaceCountry:  "JP",
		DefaultLocale:     "ja",
	}
}

func (s *ttScenario) run(t *testing.T, dryRun bool) (int, string, error) {
	t.Helper()
	var out strings.Builder
	code, err := timetree.Run(context.Background(), timetree.Options{
		BaseURL:  s.server.URL,
		Email:    "tt@example.com",
		Password: "pw",
		Mapping:  strings.NewReader(s.mapping),
		DryRun:   dryRun,
		DB:       testDB,
		Config:   ttConfig(),
		Out:      &out,
	})
	return code, out.String(), err
}

type ttTotals struct{ imported, duplicates, skipped, failed, rejected, calendars, users int }

var ttTotalLine = regexp.MustCompile(`total: imported=(\d+) updated=\d+ duplicates=(\d+) deleted=\d+ kept=\d+ moved=\d+ skipped=(\d+) failed=(\d+) rejected=(\d+) calendars_created=(\d+) users_created=(\d+)`)

func parseTotals(t *testing.T, out string) ttTotals {
	t.Helper()
	m := ttTotalLine.FindStringSubmatch(out)
	require.NotNil(t, m, "output: %s", out)
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return ttTotals{n(1), n(2), n(3), n(4), n(5), n(6), n(7)}
}

func (s *ttScenario) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(query, args...).Scan(&n))
	return n
}

func (s *ttScenario) users(t *testing.T) int {
	return s.count(t, `SELECT COUNT(*) FROM users WHERE email IN (?, ?)`, s.hanako, s.mgr)
}

func (s *ttScenario) calendars(t *testing.T) int {
	return s.count(t, `SELECT COUNT(*) FROM calendars WHERE name = ?`, s.calName)
}

func (s *ttScenario) events(t *testing.T) int {
	return s.count(t, `SELECT COUNT(*) FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id WHERE c.name = ?`, s.calName)
}

func (s *ttScenario) eventOwner(t *testing.T, sourceUID string) string {
	t.Helper()
	var email string
	require.NoError(t, testDB.QueryRow(
		`SELECT u.email FROM calendar_events e
		 JOIN calendars c ON c.id = e.calendar_id
		 JOIN users u ON u.id = e.owner_user_id
		 WHERE c.name = ? AND e.source_uid = ?`, s.calName, sourceUID).Scan(&email))
	return email
}

// eventAttendees lists the participants of an imported event, sorted.
func (s *ttScenario) eventAttendees(t *testing.T, sourceUID string) []string {
	t.Helper()
	rows, err := testDB.Query(
		`SELECT u.email FROM calendar_event_attendees a
		 JOIN calendar_events e ON e.id = a.event_id
		 JOIN calendars c ON c.id = e.calendar_id
		 JOIN users u ON u.id = a.user_id
		 WHERE c.name = ? AND e.source_uid = ? AND a.enabled = TRUE
		 ORDER BY u.email`, s.calName, sourceUID)
	require.NoError(t, err)
	defer rows.Close()
	emails := []string{}
	for rows.Next() {
		var email string
		require.NoError(t, rows.Scan(&email))
		emails = append(emails, email)
	}
	require.NoError(t, rows.Err())
	return emails
}

func (s *ttScenario) calendarID(t *testing.T) string {
	t.Helper()
	var id string
	require.NoError(t, testDB.QueryRow(`SELECT BIN_TO_UUID(public_id) FROM calendars WHERE name = ?`, s.calName).Scan(&id))
	return id
}

func (s *ttScenario) roles(t *testing.T) map[string]string {
	t.Helper()
	rows, err := testDB.Query(
		`SELECT u.email, m.role FROM calendar_members m
		 JOIN calendars c ON c.id = m.calendar_id
		 JOIN users u ON u.id = m.user_id
		 WHERE c.name = ? AND m.enabled = TRUE`, s.calName)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var email, role string
		require.NoError(t, rows.Scan(&email, &role))
		out[email] = role
	}
	require.NoError(t, rows.Err())
	return out
}

func TestTimeTreeImportDryRunWritesNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	code, out, err := s.run(t, true)
	require.NoError(t, err)
	require.Equal(t, 0, code, out)

	require.Zero(t, s.users(t), "dry run created users")
	require.Zero(t, s.calendars(t), "dry run created the calendar")
	require.Zero(t, s.events(t), "dry run wrote events")

	assert.Contains(t, out, s.calName)
	assert.Contains(t, out, "mapped")
	assert.Contains(t, out, "create")
	assert.Contains(t, out, "users to create: ["+s.hanako)
	assert.Contains(t, out, "validation errors: 0")
	assert.NotContains(t, out, "stopped-", "a deactivated calendar is not listed")
}

func TestTimeTreeImportCreatesUsersMembersAndOwners(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	code, out, err := s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)
	require.Equal(t, ttTotals{imported: 3, skipped: 4, calendars: 1, users: 2}, parseTotals(t, out))

	require.Equal(t, 2, s.users(t))
	require.Equal(t, 1, s.calendars(t))
	var name string
	require.NoError(t, testDB.QueryRow(`SELECT display_name FROM users WHERE email = ?`, s.hanako).Scan(&name))
	require.Equal(t, "山田花子", name)
	require.NoError(t, testDB.QueryRow(`SELECT display_name FROM users WHERE email = ?`, s.mgr).Scan(&name))
	require.Equal(t, strings.SplitN(s.mgr, "@", 2)[0], name, "a missing name is the local part")

	require.Equal(t, map[string]string{s.importer: "owner", s.hanako: "viewer", s.mgr: "manager"}, s.roles(t))

	require.Equal(t, s.hanako, s.eventOwner(t, "e-timed"), "label hana maps to hanako")
	require.Equal(t, s.mgr, s.eventOwner(t, "e-allday"), "no label falls back to default_owner")
	everyone := []string{s.hanako, s.mgr}
	sort.Strings(everyone)
	require.Equal(t, everyone, s.eventAttendees(t, "e-allday"), "default_attendees join an unlabelled event")
	require.Equal(t, everyone, s.eventAttendees(t, "e-rec"), "and an unlabelled series")
	require.Empty(t, s.eventAttendees(t, "e-timed"), "a person's label is theirs alone")
	require.Equal(t, 3, s.events(t))
	for _, skipped := range []string{"e-memo", "e-birthday", "e-rdate", "e-hourly"} {
		require.Zero(t, s.count(t, `SELECT COUNT(*) FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
			WHERE c.name = ? AND e.source_uid = ?`, s.calName, skipped), skipped)
	}

	// The series lands as a rule plus a cancellation, not as a single event.
	var rule, exceptions string
	require.NoError(t, testDB.QueryRow(
		`SELECT CAST(e.recurrence_rule AS CHAR), CAST(e.recurrence_exceptions AS CHAR)
		 FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
		 WHERE c.name = ? AND e.source_uid = 'e-rec'`, s.calName).Scan(&rule, &exceptions))
	require.Contains(t, rule, `"freq": "weekly"`)
	require.Contains(t, rule, `"until": "2026-04-27"`)
	require.Contains(t, exceptions, "2026-04-13")
}

func TestTimeTreeImportSecondRunAddsNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	code, out, err := s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)
	first := parseTotals(t, out)

	code, out, err = s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)
	second := parseTotals(t, out)

	require.Equal(t, 0, second.imported)
	require.Equal(t, first.imported, second.duplicates)
	require.Equal(t, first.skipped, second.skipped)
	require.Equal(t, 0, second.calendars)
	require.Equal(t, 0, second.users)
	require.Contains(t, out, "members added=0 existing=2")
	require.Contains(t, out, "(reused)")

	require.Equal(t, 1, s.calendars(t))
	require.Equal(t, 2, s.users(t))
	require.Equal(t, 3, s.events(t))
	require.Equal(t, map[string]string{s.importer: "owner", s.hanako: "viewer", s.mgr: "manager"}, s.roles(t))
}

func TestTimeTreeImportAllDayRecurrenceHonoursTheExdateAndTheUntilDay(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	code, out, err := s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)

	var evts []struct {
		Title   string `json:"title"`
		StartAt string `json:"startAt"`
		AllDay  bool   `json:"allDay"`
	}
	listEventItems(t,
		testServerURL+"/calendars/"+s.calendarID(t)+"/events?start=2026-04-01&end=2026-05-31&tz=Asia/Tokyo",
		s.token, &evts)

	starts := map[string][]time.Time{}
	for _, e := range evts {
		at, err := time.Parse(time.RFC3339, e.StartAt)
		require.NoError(t, err)
		starts[e.Title] = append(starts[e.Title], at)
	}
	midnight := func(d int) time.Time { return time.Date(2026, 4, d, 0, 0, 0, 0, time.UTC) }
	require.Len(t, starts["rec"], 3, "%v", starts["rec"])
	for i, day := range []int{6, 20, 27} {
		require.True(t, starts["rec"][i].Equal(midnight(day)), "occurrence %d: %s", i, starts["rec"][i])
	}
	require.Len(t, starts["allday"], 1)
	require.True(t, starts["allday"][0].Equal(midnight(10)), "all-day event starts at UTC midnight: %s", starts["allday"][0])
}

func TestTimeTreeImportUnreadableExdateFailsTheRunButKeepsTheRest(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	bad := withRecurrences(ttAllDay("e-bad", "bad", time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC), "UTC"),
		"RRULE:FREQ=WEEKLY", "EXDATE:not-a-date")
	s := newTTScenario(t, []map[string]any{bad, ttTimed("e-ok", "ok", time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC), 0)})
	code, out, err := s.run(t, false)
	require.Error(t, err)
	require.Equal(t, 1, code, out)
	totals := parseTotals(t, out)
	require.Equal(t, 1, totals.imported)
	require.Equal(t, 1, totals.failed)
	require.Equal(t, 1, totals.rejected)
	require.Equal(t, 1, s.events(t))
}

func TestTimeTreeImportCalendarWithoutEventsIsStillSetUp(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, nil)
	code, out, err := s.run(t, false)
	require.NoError(t, err, out)
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, s.calendars(t))
	require.Equal(t, map[string]string{s.importer: "owner", s.hanako: "viewer", s.mgr: "manager"}, s.roles(t))
	require.Zero(t, s.events(t))
}

func TestTimeTreeImportMappingErrorWritesNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	s.mapping = strings.Replace(s.mapping, "hana:", "hanna:", 1)
	code, out, err := s.run(t, false)
	require.Error(t, err)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "hanna")
	require.Zero(t, s.users(t))
	require.Zero(t, s.calendars(t))
	require.Zero(t, s.events(t))
}

func TestTimeTreeImportMissingCredentialsExitsTwoWithoutWriting(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	s := newTTScenario(t, ttEvents())
	for _, dryRun := range []bool{false, true} {
		for _, creds := range [][2]string{{"", "pw"}, {"tt@example.com", ""}, {"", ""}} {
			var out strings.Builder
			code, err := timetree.Run(context.Background(), timetree.Options{
				BaseURL: s.server.URL, Email: creds[0], Password: creds[1],
				Mapping: strings.NewReader(s.mapping), DryRun: dryRun,
				DB: testDB, Config: ttConfig(), Out: &out,
			})
			require.Error(t, err)
			require.Equal(t, 2, code)
		}
	}
	require.Zero(t, s.users(t))
	require.Zero(t, s.calendars(t))
}
