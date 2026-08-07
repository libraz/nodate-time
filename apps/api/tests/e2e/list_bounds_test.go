package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// memoPage is the shape the memo list answers with.
type memoPage struct {
	Items []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		SortOrder int32  `json:"sortOrder"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// commentPage is the shape the comment thread answers with.
type commentPage struct {
	Items []struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// TestMemoListIsBoundedAndPagesToTheRest pins the memo contract.
//
// The list used to answer with every memo a calendar had, and the SPA asks it
// once per calendar at startup, so the cost of opening the app followed how
// much anyone had ever written down. A page cap alone would answer with a
// truncated list and no way to tell -- and no way to reach the rest -- so the
// answer carries a cursor.
func TestMemoListIsBoundedAndPagesToTheRest(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tenant.CalendarID

	// More memos than one default page holds.
	const total = 150
	seedMemos(t, tenant, total)

	var first memoPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tenant.AccessToken, nil, &first)
	require.Len(t, first.Items, 100, "the default page is bounded")
	require.NotEmpty(t, first.NextCursor, "a bounded page must say there is more")

	seen := make([]int32, 0, total)
	ids := make(map[string]bool, total)
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 10, "paging is not terminating")
		url := calURL + "/memos"
		if cursor != "" {
			url += "?cursor=" + cursor
		}
		var p memoPage
		helpers.DoJSON(t, http.MethodGet, url, tenant.AccessToken, nil, &p)
		for _, m := range p.Items {
			require.False(t, ids[m.ID], "memo %s came back on two pages", m.ID)
			ids[m.ID] = true
			seen = append(seen, m.SortOrder)
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}

	require.Len(t, seen, total, "paging must reach every memo")
	for i := 1; i < len(seen); i++ {
		require.LessOrEqual(t, seen[i-1], seen[i],
			"a page boundary broke the order the owner arranged")
	}

	// The declared maximum is enforced rather than described.
	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/memos?limit=201", tenant.AccessToken, nil)
	require.Equal(t, http.StatusUnprocessableEntity, status,
		"a page size above the documented maximum must be refused, not quietly clamped")
}

// TestCommentThreadIsBoundedAndPagesIntoTheHistory pins the comment contract.
//
// A thread answered with every comment ever posted on the event. Bounding it
// from the oldest end would have been worse than useless -- the page nobody is
// looking at -- so the first page is the newest comments, returned in reading
// order, and the cursor goes backwards from there.
func TestCommentThreadIsBoundedAndPagesIntoTheHistory(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tenant.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tenant.AccessToken,
		map[string]any{
			"title":   "長い相談",
			"allDay":  false,
			"startAt": "2026-12-01T10:00:00+09:00",
			"endAt":   "2026-12-01T11:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	const total = 62
	seedComments(t, tenant, evt.ID, total)

	activityURL := calURL + "/events/" + evt.ID + "/activities"

	var first commentPage
	helpers.DoJSON(t, http.MethodGet, activityURL, tenant.AccessToken, nil, &first)
	require.Len(t, first.Items, 50, "the default page is bounded")
	require.NotEmpty(t, first.NextCursor, "a bounded thread must say there is more behind it")

	// The page is the newest end of the thread, oldest first within itself.
	require.Equal(t, fmt.Sprintf("発言 %02d", total-1), first.Items[len(first.Items)-1].Body,
		"the first page must end at the newest comment")
	require.Equal(t, fmt.Sprintf("発言 %02d", total-50), first.Items[0].Body,
		"and begin one page back from it, in reading order")

	var older commentPage
	helpers.DoJSON(t, http.MethodGet, activityURL+"?cursor="+first.NextCursor,
		tenant.AccessToken, nil, &older)
	require.Len(t, older.Items, total-50, "the cursor must reach the rest of the thread")
	require.Empty(t, older.NextCursor, "and then say there is no more")
	require.Equal(t, "発言 00", older.Items[0].Body, "the last page reaches the first comment")

	bodies := make(map[string]bool, total)
	for _, c := range append(older.Items, first.Items...) {
		require.False(t, bodies[c.ID], "comment %s came back on two pages", c.ID)
		bodies[c.ID] = true
	}
	require.Len(t, bodies, total, "paging must reach every comment exactly once")
}

// TestCappedListsHoldTheirCeiling covers the three lists that take a cap
// rather than a cursor. Each is read as a whole by the client -- a participant
// picker that cannot offer a member, or a checklist split across pages, would
// be a worse answer than a ceiling -- so the bound only has to keep one
// response from being unbounded.
func TestCappedListsHoldTheirCeiling(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tenant.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tenant.AccessToken,
		map[string]any{
			"title":   "上限",
			"allDay":  false,
			"startAt": "2026-12-02T10:00:00+09:00",
			"endAt":   "2026-12-02T11:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	const over = 520
	seedChecklistItems(t, tenant, evt.ID, over)
	seedInvites(t, tenant, over)
	seedMembers(t, tenant, over)

	var checklist []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/checklist",
		tenant.AccessToken, nil, &checklist)
	require.Len(t, checklist, 500, "a checklist answer must stop at its ceiling")

	var invites []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/invites", tenant.AccessToken, nil, &invites)
	require.Len(t, invites, 500, "an invite answer must stop at its ceiling")

	var members []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", tenant.AccessToken, nil, &members)
	require.Len(t, members, 500, "a membership answer must stop at its ceiling")
}

// TestActivityFeedDefaultIsDeclared covers the other half of the paging
// contract: the feed applied a default page size that its schema never
// mentioned, so a client reading the document could not tell what it would
// get by leaving the parameter out.
func TestActivityFeedDefaultIsDeclared(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, body := getRaw(t, testServerURL+"/openapi.json")
	require.Equal(t, http.StatusOK, status)

	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name   string `json:"name"`
					In     string `json:"in"`
					Schema struct {
						// Defaults are typed per parameter -- a timezone's is a
						// string -- so these stay untyped until the one under
						// test is picked out.
						Default any `json:"default"`
						Maximum any `json:"maximum"`
					} `json:"schema"`
				} `json:"parameters"`
			} `json:"get"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &doc))

	feed, ok := doc.Paths["/calendars/{calendarId}/activity"]
	require.True(t, ok, "the activity feed must be described")
	found := false
	for _, p := range feed.Get.Parameters {
		if p.Name != "limit" || p.In != "query" {
			continue
		}
		found = true
		require.NotNil(t, p.Schema.Default,
			"the feed applies a default page size, so the document has to name it")
		require.Equal(t, float64(50), p.Schema.Default,
			"the declared default must be the one the handler applies")
		require.Equal(t, float64(200), p.Schema.Maximum)
	}
	require.True(t, found, "the activity feed declares a limit parameter")

	tenant := helpers.NewTenant(t, testServerURL)
	feedURL := testServerURL + "/calendars/" + tenant.CalendarID + "/activity"

	// Omitting the parameter and asking for the declared default must be the
	// same request.
	var omitted, explicit struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, feedURL, tenant.AccessToken, nil, &omitted)
	helpers.DoJSON(t, http.MethodGet, feedURL+"?limit=50", tenant.AccessToken, nil, &explicit)
	require.Equal(t, len(explicit.Items), len(omitted.Items),
		"the documented default must be the one the handler applies")
}

// --- seeds -----------------------------------------------------------------

// bulkInsert runs one multi-row INSERT. A few hundred separate statements are
// a few hundred transactions taking locks one at a time, which is enough
// contention to deadlock whatever else the parallel suite is doing; a single
// statement is over before anything else notices.
func bulkInsert(t *testing.T, into, row string, rows [][]any) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	values := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*len(rows[0]))
	for _, r := range rows {
		values = append(values, row)
		args = append(args, r...)
	}
	_, err := testDB.Exec(into+" VALUES "+strings.Join(values, ","), args...)
	require.NoError(t, err)
}

// seedMemos writes memos straight to the table. Going through the API would
// append a log row for each, which is not what this is measuring.
func seedMemos(t *testing.T, tenant *helpers.TestTenant, total int) {
	t.Helper()
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)
	userID := internalUserID(t, tenant.UserID)

	rows := make([][]any, 0, total)
	for i := range total {
		pub := uuid.New()
		rows = append(rows, []any{pub[:], workspaceID, calendarID, userID, fmt.Sprintf("覚書 %03d", i), i})
	}
	bulkInsert(t,
		`INSERT INTO calendar_memos (public_id, workspace_id, calendar_id, created_by_user_id, title, sort_weight)`,
		"(?, ?, ?, ?, ?, ?)", rows)
}

// seedComments writes a thread whose bodies carry their position, so a page
// can be checked against where in the thread it came from.
func seedComments(t *testing.T, tenant *helpers.TestTenant, eventPublicID string, total int) {
	t.Helper()
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	eventID := internalEventID(t, eventPublicID)
	userID := internalUserID(t, tenant.UserID)

	rows := make([][]any, 0, total)
	for i := range total {
		pub := uuid.New()
		rows = append(rows, []any{pub[:], workspaceID, eventID, userID, fmt.Sprintf("発言 %02d", i), i})
	}
	bulkInsert(t,
		`INSERT INTO calendar_event_comments (public_id, workspace_id, event_id, author_id, body, created_at)`,
		"(?, ?, ?, ?, ?, DATE_ADD('2026-01-01 00:00:00.000', INTERVAL ? SECOND))", rows)
}

func seedChecklistItems(t *testing.T, tenant *helpers.TestTenant, eventPublicID string, total int) {
	t.Helper()
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	eventID := internalEventID(t, eventPublicID)
	userID := internalUserID(t, tenant.UserID)

	rows := make([][]any, 0, total)
	for i := range total {
		pub := uuid.New()
		rows = append(rows, []any{pub[:], workspaceID, eventID, userID, fmt.Sprintf("持ち物 %03d", i), i})
	}
	bulkInsert(t,
		`INSERT INTO calendar_event_checklist_items (public_id, workspace_id, event_id, created_by_user_id, title, sort_weight)`,
		"(?, ?, ?, ?, ?, ?)", rows)
}

func seedInvites(t *testing.T, tenant *helpers.TestTenant, total int) {
	t.Helper()
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)
	userID := internalUserID(t, tenant.UserID)

	rows := make([][]any, 0, total)
	for range total {
		pub := uuid.New()
		hash := uuid.NewString() + uuid.NewString()
		rows = append(rows, []any{pub[:], workspaceID, calendarID, userID, hash[:64]})
	}
	bulkInsert(t,
		`INSERT INTO calendar_invites (public_id, workspace_id, calendar_id, created_by_user_id, token_hash, role)`,
		"(?, ?, ?, ?, ?, 'editor')", rows)
}

// seedMembers adds members without signing anybody up. Each needs a user row
// of its own, because membership joins users for the name a response carries.
//
// The membership rows are selected back by the address prefix rather than
// counted forward from an insert id: a multi-row INSERT is not promised
// consecutive auto-increment values.
func seedMembers(t *testing.T, tenant *helpers.TestTenant, total int) {
	t.Helper()
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)

	batch := uuid.NewString()
	rows := make([][]any, 0, total)
	for i := range total {
		pub := uuid.New()
		rows = append(rows, []any{
			pub[:],
			fmt.Sprintf("%s-%03d@example.test", batch, i),
			fmt.Sprintf("同席者 %03d", i),
		})
	}
	bulkInsert(t, `INSERT INTO users (public_id, email, display_name)`, "(?, ?, ?)", rows)

	_, err := testDB.Exec(
		`INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role, member_color)
		 SELECT UUID_TO_BIN(UUID()), ?, ?, id, 'viewer', '#42A5F5' FROM users WHERE email LIKE ?`,
		workspaceID, calendarID, batch+"-%")
	require.NoError(t, err)
}
