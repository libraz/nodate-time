package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// searchTooBroadCap mirrors the number of candidates one search may weigh.
const searchTooBroadCap = 2000

type searchItem struct {
	ID            string `json:"id"`
	CalendarID    string `json:"calendarId"`
	Title         string `json:"title"`
	Location      string `json:"location"`
	Memo          string `json:"memo"`
	StartAt       string `json:"startAt"`
	Etag          string `json:"etag"`
	DetailsHidden bool   `json:"detailsHidden"`
}

type searchPage struct {
	Items   []searchItem `json:"items"`
	HasMore bool         `json:"hasMore"`
}

// searchRaw runs a search and returns the status and body untouched.
func searchRaw(t *testing.T, token, query string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/search?"+query, token, nil)
}

// searchFor expects the search to succeed.
func searchFor(t *testing.T, token, q, extra string) searchPage {
	t.Helper()
	query := "q=" + url.QueryEscape(q)
	if extra != "" {
		query += "&" + extra
	}
	status, raw := searchRaw(t, token, query)
	require.Equal(t, http.StatusOK, status, string(raw))
	var page searchPage
	require.NoError(t, json.Unmarshal(raw, &page))
	return page
}

func searchErrorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(raw, &e), string(raw))
	return e.Code
}

// searchTag is text no other test's events contain, so a search for it sees
// only this test's rows in the shared database.
func searchTag() string {
	return "zq" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
}

func searchTitles(p searchPage) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Title)
	}
	return out
}

func searchCreate(t *testing.T, calURL, token string, body map[string]any) searchItem {
	t.Helper()
	var ev searchItem
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, body, &ev)
	return ev
}

func searchTimed(title, start, end string, extra map[string]any) map[string]any {
	body := map[string]any{"title": title, "allDay": false, "startAt": start, "endAt": end}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func searchAddMember(t *testing.T, calURL, ownerToken, userID, role string) {
	t.Helper()
	helpers.DoJSON(t, http.MethodPost, calURL+"/members", ownerToken,
		map[string]any{"userId": userID, "role": role}, nil)
}

func TestSearchQueryLengthBounds(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)

	for name, query := range map[string]string{
		"missing":        "",
		"empty":          "q=",
		"101 characters": "q=" + strings.Repeat("a", 101),
		"101 emoji":      "q=" + url.QueryEscape(strings.Repeat("😀", 101)),
	} {
		status, raw := searchRaw(t, owner.AccessToken, query)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		require.Equal(t, "REQUEST.VALIDATION", searchErrorCode(t, raw), name)
	}

	for name, q := range map[string]string{
		"one character":  "a",
		"100 characters": strings.Repeat("a", 100),
		"one emoji":      "😀",
		"100 emoji":      strings.Repeat("😀", 100),
	} {
		status, raw := searchRaw(t, owner.AccessToken, "q="+url.QueryEscape(q))
		require.Equal(t, http.StatusOK, status, "%s: %s", name, raw)
	}

	for _, extra := range []string{"limit=0", "limit=101", "from=yesterday"} {
		status, raw := searchRaw(t, owner.AccessToken, "q=a&"+extra)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", extra, raw)
	}
	status, raw := searchRaw(t, owner.AccessToken, "q=a&limit=100")
	require.Equal(t, http.StatusOK, status, string(raw))
}

func TestSearchMatchesLikeCharactersLiterally(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag := searchTag()

	for i, title := range []string{
		"50%off " + tag, "50xoff " + tag,
		"a_b " + tag, "aXb " + tag,
		`c\d ` + tag, "cd " + tag,
	} {
		day := "2031-05-0" + string(rune('1'+i))
		searchCreate(t, calURL, owner.AccessToken, searchTimed(title, day+"T10:00:00Z", day+"T11:00:00Z", nil))
	}

	from := "from=2031-05-01"
	require.Equal(t, []string{"50%off " + tag}, searchTitles(searchFor(t, owner.AccessToken, "%off "+tag, from)))
	require.Equal(t, []string{"a_b " + tag}, searchTitles(searchFor(t, owner.AccessToken, "a_b "+tag, from)))
	require.Equal(t, []string{`c\d ` + tag}, searchTitles(searchFor(t, owner.AccessToken, `c\d `+tag, from)))
	require.Len(t, searchFor(t, owner.AccessToken, "%", from+"&limit=100").Items, 1, "a lone percent matches only the title holding one")
}

func TestSearchRefusesTooManyCandidates(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag := searchTag()

	_, err := testDB.ExecContext(t.Context(), `INSERT INTO calendar_events
		(public_id, workspace_id, calendar_id, kind, visibility, title, all_day, start_at, end_at, timezone,
		 owner_user_id, created_by_user_id)
		SELECT UNHEX(REPLACE(UUID(), '-', '')), c.workspace_id, c.id, 'event', 'default', ?, FALSE,
		       '2031-01-05 10:00:00', '2031-01-05 11:00:00', 'UTC', cm.user_id, cm.user_id
		FROM calendars c
		JOIN calendar_members cm ON cm.calendar_id = c.id AND cm.role = 'owner'
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) a
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) b
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d
		JOIN (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
		      UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) e
		WHERE c.public_id = UUID_TO_BIN(?) AND a.n + 10*b.n + 100*d.n + 1000*e.n < ?`,
		"filler "+tag, owner.CalendarID, searchTooBroadCap)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DELETE e FROM calendar_events e JOIN calendars c ON c.id = e.calendar_id
			WHERE c.public_id = UUID_TO_BIN(?) AND e.title LIKE ?`, owner.CalendarID, "%"+tag)
	})

	page := searchFor(t, owner.AccessToken, tag, "from=2031-01-01&limit=100")
	require.Len(t, page.Items, 100, "exactly the cap is searched")
	require.True(t, page.HasMore)

	searchCreate(t, calURL, owner.AccessToken, searchTimed("one more "+tag, "2031-01-06T10:00:00Z", "2031-01-06T11:00:00Z", nil))
	status, raw := searchRaw(t, owner.AccessToken, "q="+url.QueryEscape(tag)+"&from=2031-01-01")
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "SEARCH.TOO_BROAD", searchErrorCode(t, raw))

	// Narrowing the text, or moving from past the pile, brings it back under the cap.
	require.Len(t, searchFor(t, owner.AccessToken, "one more "+tag, "from=2031-01-01").Items, 1)
	require.Empty(t, searchFor(t, owner.AccessToken, tag, "from=2031-02-01").Items)
}

func TestSearchFromKeepsTodaysAllDayAndRunningEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag := searchTag()

	allDay := func(title, from, to string) {
		searchCreate(t, calURL, owner.AccessToken, map[string]any{
			"title": title, "allDay": true, "startDate": from, "endDate": to})
	}
	allDay("today allday "+tag, "2031-06-10", "2031-06-11")
	allDay("yesterday allday "+tag, "2031-06-09", "2031-06-10")
	allDay("tomorrow allday "+tag, "2031-06-11", "2031-06-12")
	searchCreate(t, calURL, owner.AccessToken, searchTimed("running "+tag, "2031-06-10T00:30:00Z", "2031-06-10T02:00:00Z", nil))
	searchCreate(t, calURL, owner.AccessToken, searchTimed("finished "+tag, "2031-06-10T00:10:00Z", "2031-06-10T00:50:00Z", nil))
	searchCreate(t, calURL, owner.AccessToken, searchTimed("later "+tag, "2031-06-10T05:00:00Z", "2031-06-10T06:00:00Z", nil))

	// 10:00 in Tokyo is 01:00 UTC, the same calendar day for a caller in either zone.
	got := searchFor(t, owner.AccessToken, tag, "from="+url.QueryEscape("2031-06-10T10:00:00+09:00"))
	require.Equal(t, []string{
		"today allday " + tag, "running " + tag, "later " + tag, "tomorrow allday " + tag,
	}, searchTitles(got), "ordered by start; ended and previous-day events are out")

	page := searchFor(t, owner.AccessToken, tag, "from="+url.QueryEscape("2031-06-10T10:00:00+09:00")+"&limit=2")
	require.Len(t, page.Items, 2)
	require.True(t, page.HasMore)

	// A bare date reads as that day in the caller's zone.
	require.Len(t, searchFor(t, owner.AccessToken, tag, "from=2031-06-11").Items, 1)
}

func TestSearchOrdersTiesByEventID(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag := searchTag()

	for i := 0; i < 4; i++ {
		searchCreate(t, calURL, owner.AccessToken, searchTimed("same time "+tag, "2031-07-01T10:00:00Z", "2031-07-01T11:00:00Z", nil))
	}
	page := searchFor(t, owner.AccessToken, tag, "from=2031-07-01")
	require.Len(t, page.Items, 4)
	for i := 1; i < len(page.Items); i++ {
		require.Less(t, page.Items[i-1].ID, page.Items[i].ID)
	}
}

func TestSearchRespectsVisibilityAndSpansMemberCalendars(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	attendee := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag := searchTag()
	searchAddMember(t, calURL, owner.AccessToken, member.UserID, "viewer")
	searchAddMember(t, calURL, owner.AccessToken, attendee.UserID, "viewer")

	at := func(day string) (string, string) { return day + "T10:00:00Z", day + "T11:00:00Z" }
	mk := func(day string, body map[string]any) searchItem {
		s, e := at(day)
		return searchCreate(t, calURL, owner.AccessToken, searchTimed(body["title"].(string), s, e, body))
	}
	mk("2031-08-01", map[string]any{"title": "public " + tag, "visibility": "public"})
	mk("2031-08-02", map[string]any{"title": "private " + tag, "visibility": "private", "location": "room " + tag})
	mk("2031-08-03", map[string]any{"title": "plain", "visibility": "private", "location": "only location " + tag})
	mk("2031-08-04", map[string]any{"title": "plain public", "visibility": "public", "memo": "memo " + tag})
	mk("2031-08-05", map[string]any{"title": "secret " + tag, "visibility": "confidential", "participantIds": []string{attendee.UserID}})
	mk("2031-08-06", map[string]any{"title": "plain confidential", "visibility": "confidential", "memo": "memo " + tag})

	from := "from=2031-08-01"
	ownerSees := searchTitles(searchFor(t, owner.AccessToken, tag, from))
	require.Equal(t, []string{
		"public " + tag, "private " + tag, "plain", "plain public", "secret " + tag, "plain confidential",
	}, ownerSees)

	memberPage := searchFor(t, member.AccessToken, tag, from)
	require.Equal(t, []string{"public " + tag, "private " + tag, "plain public"}, searchTitles(memberPage),
		"confidential rows and matches in fields the member may not read never appear")
	private := memberPage.Items[1]
	require.True(t, private.DetailsHidden)
	require.Empty(t, private.Location)
	require.Equal(t, "memo "+tag, memberPage.Items[2].Memo)
	require.Equal(t, owner.CalendarID, private.CalendarID)

	attendeePage := searchFor(t, attendee.AccessToken, tag, from)
	require.Contains(t, searchTitles(attendeePage), "secret "+tag)
	require.NotContains(t, searchTitles(attendeePage), "plain confidential")

	// Calendars the caller belongs to are all searched, others never.
	other := helpers.NewTenant(t, testServerURL)
	otherURL := testServerURL + "/calendars/" + other.CalendarID
	searchCreate(t, otherURL, other.AccessToken, searchTimed("elsewhere "+tag, "2031-08-07T10:00:00Z", "2031-08-07T11:00:00Z", nil))
	require.NotContains(t, searchTitles(searchFor(t, member.AccessToken, tag, from)), "elsewhere "+tag)
	searchAddMember(t, otherURL, other.AccessToken, member.UserID, "viewer")
	spanning := searchFor(t, member.AccessToken, tag, from)
	require.Contains(t, searchTitles(spanning), "elsewhere "+tag)
	require.Contains(t, searchTitles(spanning), "public "+tag)
}

func TestSearchFindsTheNextOccurrenceOfASeries(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tag, renamed := searchTag(), searchTag()

	series := searchCreate(t, calURL, owner.AccessToken, searchTimed("daily "+tag, "2031-09-01T10:00:00Z", "2031-09-01T11:00:00Z",
		map[string]any{"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 30}}))

	first := searchFor(t, owner.AccessToken, tag, "from=2031-09-05T10:30:00Z")
	require.Len(t, first.Items, 1, "a series answers once, with its first upcoming occurrence")
	require.Equal(t, series.ID+"_20310905", first.Items[0].ID, "the running occurrence counts")
	require.Equal(t, "2031-09-05T10:00:00Z", first.Items[0].StartAt)

	next := searchFor(t, owner.AccessToken, tag, "from=2031-09-05T11:00:00Z")
	require.Equal(t, series.ID+"_20310906", next.Items[0].ID)
	require.Empty(t, searchFor(t, owner.AccessToken, tag, "from=2031-10-15").Items, "a finished series has no upcoming occurrence")

	// Renaming one occurrence moves the series' hit to the next one and makes
	// the renamed occurrence a hit of its own under the new text.
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch,
		calURL+"/events/"+next.Items[0].ID+"?scope=this", owner.AccessToken,
		map[string]any{"title": "renamed " + renamed}, etagHeader(next.Items[0].Etag))
	require.Equal(t, http.StatusOK, status, string(raw))

	after := searchFor(t, owner.AccessToken, tag, "from=2031-09-05T11:00:00Z")
	require.Len(t, after.Items, 1)
	require.Equal(t, series.ID+"_20310907", after.Items[0].ID)
	byOverride := searchFor(t, owner.AccessToken, renamed, "from=2031-09-05T11:00:00Z")
	require.Len(t, byOverride.Items, 1)
	require.Equal(t, series.ID+"_20310906", byOverride.Items[0].ID)
	require.Equal(t, "renamed "+renamed, byOverride.Items[0].Title)

	// A cancelled occurrence is never a hit.
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+after.Items[0].ID+"?scope=this", owner.AccessToken, nil, etagHeader(after.Items[0].Etag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
	skipped := searchFor(t, owner.AccessToken, tag, "from=2031-09-05T11:00:00Z")
	require.Equal(t, series.ID+"_20310908", skipped.Items[0].ID)
}
