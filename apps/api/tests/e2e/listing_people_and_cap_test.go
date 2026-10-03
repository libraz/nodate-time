package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestListingCostDoesNotFollowTheNumberOfPeople pins that owners, creators and
// their colours are read together. A shared calendar's listing used to pay a
// round trip or two per distinct person on it.
func TestListingCostDoesNotFollowTheNumberOfPeople(t *testing.T) {
	bootstrap(t)

	srv, counter := helpers.NewCountingTestServer(t, testDB)
	owner := helpers.NewTenant(t, srv.BaseURL)
	calURL := srv.BaseURL + "/calendars/" + owner.CalendarID

	day := 1
	addAuthor := func() {
		t.Helper()
		member := helpers.NewTenant(t, srv.BaseURL)
		joinAs2(t, srv.BaseURL, calURL, owner.AccessToken, member, "editor")
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", member.AccessToken,
			eventBody(fmt.Sprintf("by member %d", day), fmt.Sprintf("2026-10-%02dT10:00:00+09:00", day),
				fmt.Sprintf("2026-10-%02dT11:00:00+09:00", day)), nil)
		day++
	}
	measure := func() (int, int) {
		t.Helper()
		var listed []struct {
			OwnerID   string `json:"ownerId"`
			CreatedBy string `json:"createdBy"`
			Color     string `json:"color"`
		}
		counter.Reset()
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-10-01&end=2026-10-31&tz=Asia/Tokyo",
			owner.AccessToken, nil, &listed)
		people := map[string]bool{}
		for _, e := range listed {
			require.NotEmpty(t, e.OwnerID)
			require.Equal(t, e.OwnerID, e.CreatedBy)
			require.NotEmpty(t, e.Color)
			people[e.OwnerID] = true
		}
		return counter.Count(), len(people)
	}

	addAuthor()
	addAuthor()
	before, fewer := measure()
	for range 4 {
		addAuthor()
	}
	after, more := measure()
	require.Equal(t, 2, fewer)
	require.Equal(t, 6, more)
	require.Equal(t, before, after,
		"three times the people should not cost more queries: %d then %d", before, after)
}

// TestListingCapCutsTheTailOfTheWindow fills a window with more one-off events
// than one listing carries, beside a daily series. Both the calendar listing
// and the public feed stop at the cap, keep the earliest instances whatever
// kind they are, and say they stopped.
func TestListingCapCutsTheTailOfTheWindow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var calID, workspaceID, userID uint32
	calPub := uuid.MustParse(owner.CalendarID)
	userPub := uuid.MustParse(owner.UserID)
	require.NoError(t, testDB.QueryRow(`SELECT id, workspace_id FROM calendars WHERE public_id = ?`, calPub[:]).
		Scan(&calID, &workspaceID))
	require.NoError(t, testDB.QueryRow(`SELECT id FROM users WHERE public_id = ?`, userPub[:]).Scan(&userID))

	// One-off events a minute apart from 1 July 00:00 UTC: the last starts
	// on 4 July at 11:20.
	oneOffs := daterange.MaxInstances + 1
	_, err := testDB.Exec(`
INSERT INTO calendar_events (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
                             owner_user_id, created_by_user_id)
SELECT UNHEX(REPLACE(UUID(), '-', '')), ?, ?, CONCAT('bulk ', n),
       TIMESTAMPADD(MINUTE, n, '2026-07-01 00:00:00'), TIMESTAMPADD(MINUTE, n + 1, '2026-07-01 00:00:00'), 'UTC',
       ?, ?
FROM (SELECT a.d + 10 * b.d + 100 * c.d + 1000 * e.d AS n
      FROM (SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
            UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) a,
           (SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
            UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) b,
           (SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
            UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) c,
           (SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
            UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) e) seq
WHERE n < ?`, workspaceID, calID, userID, userID, oneOffs)
	require.NoError(t, err)

	// A daily series at 12:00 UTC: 1-3 July fall before the last one-off.
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Daily",
			"allDay":         false,
			"startAt":        "2026-07-01T12:00:00Z",
			"endAt":          "2026-07-01T12:30:00Z",
			"timezone":       "UTC",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1},
		}, nil)

	status, raw, headers := helpers.DoJSONFull(t, http.MethodGet,
		calURL+"/events?start=2026-07-01&end=2026-07-31&tz=UTC", owner.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "true", headers.Get("X-Result-Truncated"))

	var listed []recInstance
	require.NoError(t, json.Unmarshal(raw, &listed))
	require.Len(t, listed, daterange.MaxInstances)
	series := 0
	for _, e := range listed {
		if e.Title == "Daily" {
			series++
		}
	}
	require.Equal(t, 3, series, "the series' occurrences inside the kept head of the window stay")
	// 5001 one-offs and 3 occurrences precede 4 July 11:20; the 4 latest
	// one-offs are what the cap drops.
	require.Equal(t, fmt.Sprintf("bulk %d", oneOffs-5), listed[len(listed)-1].Title)

	// The public feed cuts the same way.
	token := publicShareToken(t, calURL, owner.AccessToken)
	status, raw, headers = helpers.DoJSONFull(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?start=2026-07-01&end=2026-07-31&tz=UTC", "", nil, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "true", headers.Get("X-Result-Truncated"))
	var shared []struct {
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal(raw, &shared))
	require.Len(t, shared, daterange.MaxInstances)
	series = 0
	for _, e := range shared {
		if e.Title == "Daily" {
			series++
		}
	}
	require.Equal(t, 3, series)
	require.Equal(t, fmt.Sprintf("bulk %d", oneOffs-5), shared[len(shared)-1].Title)
}
