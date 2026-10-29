package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// concurrentPuts sends the same If-Match with every body at once and returns
// the statuses.
func concurrentPuts(t *testing.T, url, token, etag string, bodies []map[string]any) []int {
	t.Helper()
	var start, done sync.WaitGroup
	start.Add(1)
	statuses := make([]int, len(bodies))
	for i, body := range bodies {
		done.Add(1)
		go func(i int, body map[string]any) {
			defer done.Done()
			start.Wait()
			statuses[i], _ = helpers.DoJSONStatusWithHeaders(t, http.MethodPut, url, token, body,
				map[string]string{"If-Match": etag})
		}(i, body)
	}
	start.Done()
	done.Wait()
	return statuses
}

func countStatus(statuses []int, want int) int {
	n := 0
	for _, s := range statuses {
		if s == want {
			n++
		}
	}
	return n
}

// Writers holding the same revision race to save it. Exactly one may win;
// every other one is told its copy is stale, on a plain event and on an
// occurrence of a series alike.
func TestConcurrentSavesOfOneRevisionLetExactlyOneThrough(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		eventBody("Plain", "2026-09-10T10:00:00+09:00", "2026-09-10T11:00:00+09:00"), &evt)
	occurrences := createWeeklyFriday(t, calURL, owner.AccessToken)

	for name, url := range map[string]string{
		"plain event":           calURL + "/events/" + evt.ID,
		"occurrence scope=this": calURL + "/events/" + occurrences[1].ID + "?scope=this",
	} {
		_, _, headers := helpers.DoJSONFull(t, http.MethodGet, strings.Split(url, "?")[0], owner.AccessToken, nil, nil)
		etag := headers.Get("ETag")
		require.NotEmpty(t, etag)

		bodies := make([]map[string]any, 6)
		for i := range bodies {
			bodies[i] = eventBody(fmt.Sprintf("writer %d", i), "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00")
		}
		statuses := concurrentPuts(t, url, owner.AccessToken, etag, bodies)
		require.Equal(t, 1, countStatus(statuses, http.StatusOK), "%s: statuses %v", name, statuses)
		require.Equal(t, len(bodies)-1, countStatus(statuses, http.StatusConflict), "%s: statuses %v", name, statuses)
	}
}

// A second edit of an already-edited occurrence that omits the optional
// fields keeps what the occurrence has, not what the series has.
func TestSecondOccurrenceEditKeepsTheOccurrencesOwnFields(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	target := createWeeklyFriday(t, calURL, owner.AccessToken)[1].ID
	url := calURL + "/events/" + target

	first := eventBody("Private this week", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00")
	first["visibility"] = "private"
	first["showAs"] = "oof"
	first["flexibility"] = "negotiable"
	first["timezone"] = "America/New_York"
	helpers.DoJSON(t, http.MethodPut, url+"?scope=this", owner.AccessToken, first, nil)

	// A drag: only the times and the full-replace fields, none of the
	// omittable ones.
	helpers.DoJSON(t, http.MethodPut, url+"?scope=this", owner.AccessToken,
		eventBody("Private this week", "2026-04-10T17:00:00+09:00", "2026-04-10T18:00:00+09:00"), nil)

	var got struct {
		Visibility  string `json:"visibility"`
		ShowAs      string `json:"showAs"`
		Flexibility string `json:"flexibility"`
		Timezone    string `json:"timezone"`
		StartAt     string `json:"startAt"`
	}
	helpers.DoJSON(t, http.MethodGet, url, owner.AccessToken, nil, &got)
	require.Equal(t, "private", got.Visibility, "omitting visibility must not republish the occurrence")
	require.Equal(t, "oof", got.ShowAs)
	require.Equal(t, "negotiable", got.Flexibility)
	require.Equal(t, "America/New_York", got.Timezone)
	require.Equal(t, "2026-04-10T08:00:00Z", got.StartAt, "the drag itself applied")
}

// An occurrence on one side of a DST transition, moved and applied to the
// whole series, moves the series by the calendar days and time of day the
// user moved it -- not by the hours between the two instants.
func TestSeriesEditFromAnOccurrenceKeepsWallClockAcrossDST(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var series struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Morning call",
			"allDay":         false,
			"startAt":        "2026-01-05T10:00:00-05:00",
			"endAt":          "2026-01-05T10:30:00-05:00",
			"timezone":       "America/New_York",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1},
		}, &series)

	// 7 March is still EST; dragged to 9 March, which is EDT, same 10:00.
	body := eventBody("Morning call", "2026-03-09T10:00:00-04:00", "2026-03-09T10:30:00-04:00")
	body["recurrenceRule"] = map[string]any{"freq": "daily", "interval": 1}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series.ID+"_20260307?scope=all", owner.AccessToken, body, nil)

	var master struct {
		StartAt string `json:"startAt"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, &master)
	require.Equal(t, "2026-01-07T15:00:00Z", master.StartAt, "the anchor moves two days and keeps 10:00 EST")

	var later []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-03-20&end=2026-03-21&tz=America/New_York",
		owner.AccessToken, nil, &later)
	require.Len(t, later, 2)
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, occ := range later {
		start, err := time.Parse(time.RFC3339, occ.StartAt)
		require.NoError(t, err)
		require.Equal(t, "10:00", start.In(ny).Format("15:04"), "occurrence %s", occ.ID)
	}
}

// Changing the rule drops the edited and cancelled occurrences the new rule
// no longer produces, instead of leaving them beside the new schedule.
func TestRuleChangeDropsOccurrencesTheNewRuleDoesNotProduce(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	listApril := func(calURL string) []recInstance {
		t.Helper()
		var evts []recInstance
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30&tz=Asia/Tokyo",
			owner.AccessToken, nil, &evts)
		return evts
	}
	seriesID := func(composite string) string { return strings.Split(composite, "_")[0] }

	t.Run("count shortened", func(t *testing.T) {
		calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "count")
		occ := createWeeklyFriday(t, calURL, owner.AccessToken)
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[2].ID+"?scope=this", owner.AccessToken,
			eventBody("edited", "2026-04-17T15:00:00+09:00", "2026-04-17T16:00:00+09:00"), nil)

		body := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00")
		body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "count": 2}
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+seriesID(occ[0].ID), owner.AccessToken, body, nil)

		after := listApril(calURL)
		require.Len(t, after, 2, "only the two occurrences the rule still produces: %+v", after)
		for _, e := range after {
			require.NotEqual(t, "edited", e.Title)
		}
	})

	t.Run("weekday changed", func(t *testing.T) {
		calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "weekday")
		occ := createWeeklyFriday(t, calURL, owner.AccessToken)
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[1].ID+"?scope=this", owner.AccessToken,
			eventBody("edited", "2026-04-11T15:00:00+09:00", "2026-04-11T16:00:00+09:00"), nil)
		helpers.DoJSON(t, http.MethodDelete, calURL+"/events/"+occ[2].ID+"?scope=this", owner.AccessToken, nil, nil)

		body := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00")
		body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"TH"}}
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+seriesID(occ[0].ID), owner.AccessToken, body, nil)

		after := listApril(calURL)
		require.Len(t, after, 5, "the start (Friday 3), then Thursdays 9, 16, 23 and 30: %+v", after)
		for i, e := range after {
			require.NotEqual(t, "edited", e.Title, "an edit of a Friday no longer stands for any occurrence")
			start, err := time.Parse(time.RFC3339, e.StartAt)
			require.NoError(t, err)
			want := time.Thursday
			if i == 0 {
				want = time.Friday // the event's start is always its first occurrence
			}
			require.Equal(t, want, start.Add(9*time.Hour).Weekday(), "%s", e.StartAt)
		}

		// Back to Fridays: the dropped edit and cancellation stay dropped
		// rather than resurfacing once their dates are on the grid again.
		body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+seriesID(occ[0].ID), owner.AccessToken, body, nil)
		back := listApril(calURL)
		require.Len(t, back, 4, "Fridays 3, 10, 17 and 24: %+v", back)
		for _, e := range back {
			require.Equal(t, "Weekly meeting", e.Title)
		}
	})
}

// seriesOverrideCount is how many override rows hang off a series.
func seriesOverrideCount(t *testing.T, seriesPublicID string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(`
		SELECT COUNT(*) FROM calendar_events
		 WHERE recurrence_parent_id = (
		   SELECT id FROM calendar_events WHERE public_id = UNHEX(REPLACE(?, '-', '')))`,
		seriesPublicID).Scan(&n))
	return n
}

// An occurrence moved on its own is shown at its override's dates. Saving it
// to the whole series with those same dates changes nothing about when
// anything happens: the move is measured from what the caller was shown, not
// from the slot the rule gives the occurrence.
func TestSavingAMovedOccurrenceUnchangedToTheWholeSeriesMovesNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	occ := createWeeklyFriday(t, calURL, owner.AccessToken)
	moved := calURL + "/events/" + occ[1].ID

	helpers.DoJSON(t, http.MethodPut, moved+"?scope=this", owner.AccessToken,
		eventBody("Weekly meeting", "2026-04-10T19:00:00+09:00", "2026-04-10T20:30:00+09:00"), nil)

	var shown struct {
		StartAt string `json:"startAt"`
		EndAt   string `json:"endAt"`
	}
	helpers.DoJSON(t, http.MethodGet, moved, owner.AccessToken, nil, &shown)
	require.Equal(t, "2026-04-10T10:00:00Z", shown.StartAt)

	body := eventBody("Renamed", shown.StartAt, shown.EndAt)
	body["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
	helpers.DoJSON(t, http.MethodPut, moved+"?scope=all", owner.AccessToken, body, nil)

	var after []struct {
		StartAt string `json:"startAt"`
		EndAt   string `json:"endAt"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30&tz=Asia/Tokyo",
		owner.AccessToken, nil, &after)
	got := make([]string, 0, len(after))
	for _, e := range after {
		got = append(got, e.StartAt+"/"+e.EndAt)
	}
	require.ElementsMatch(t, []string{
		"2026-04-03T06:00:00Z/2026-04-03T07:00:00Z",
		"2026-04-10T10:00:00Z/2026-04-10T11:30:00Z",
		"2026-04-17T06:00:00Z/2026-04-17T07:00:00Z",
		"2026-04-24T06:00:00Z/2026-04-24T07:00:00Z",
	}, got, "neither the series nor the moved occurrence moves")
}

// An occurrence somebody else cancelled is gone: GET answers 404 for it, and
// so does an edit confined to it, which leaves no override behind and does not
// give the series a new revision.
func TestEditingACancelledOccurrenceIsNotFound(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	occ := createWeeklyFriday(t, calURL, owner.AccessToken)
	seriesID := strings.Split(occ[0].ID, "_")[0]

	helpers.DoJSON(t, http.MethodDelete, calURL+"/events/"+occ[2].ID+"?scope=this", owner.AccessToken, nil, nil)
	_, _, headers := helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+seriesID, owner.AccessToken, nil, nil)
	etag := headers.Get("ETag")
	require.NotEmpty(t, etag)

	status, raw := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occ[2].ID+"?scope=this", owner.AccessToken,
		eventBody("Moved after all", "2026-04-17T18:00:00+09:00", "2026-04-17T19:00:00+09:00"))
	require.Equal(t, http.StatusNotFound, status, "%s", raw)

	_, _, headers = helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+seriesID, owner.AccessToken, nil, nil)
	require.Equal(t, etag, headers.Get("ETag"), "a refused edit gives the series no new revision")
	require.Zero(t, seriesOverrideCount(t, seriesID), "and leaves no override for the cancelled date")
}

// scope=this reaches one occurrence of a live series or nothing. An occurrence
// id whose series has stopped recurring names no occurrence, and a plain id
// names none to begin with; neither falls through to the event itself.
func TestScopeThisNeverRewritesOrDeletesTheWholeEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)

	t.Run("occurrence of a series no longer recurring", func(t *testing.T) {
		calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "stale occurrence")
		occ := createWeeklyFriday(t, calURL, owner.AccessToken)
		seriesURL := calURL + "/events/" + strings.Split(occ[0].ID, "_")[0]

		helpers.DoJSON(t, http.MethodPut, seriesURL, owner.AccessToken,
			eventBody("Just once", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00"), nil)

		status, raw := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+occ[1].ID+"?scope=this",
			owner.AccessToken, nil)
		require.Equal(t, http.StatusNotFound, status, "%s", raw)
		status, raw = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occ[1].ID+"?scope=this",
			owner.AccessToken, eventBody("Stale", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00"))
		require.Equal(t, http.StatusNotFound, status, "%s", raw)

		var evt struct {
			Title   string `json:"title"`
			StartAt string `json:"startAt"`
		}
		helpers.DoJSON(t, http.MethodGet, seriesURL, owner.AccessToken, nil, &evt)
		require.Equal(t, "Just once", evt.Title)
		require.Equal(t, "2026-04-03T06:00:00Z", evt.StartAt)
	})

	t.Run("plain id", func(t *testing.T) {
		calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "plain id")
		occ := createWeeklyFriday(t, calURL, owner.AccessToken)
		seriesURL := calURL + "/events/" + strings.Split(occ[0].ID, "_")[0]

		status, raw := helpers.DoJSONStatus(t, http.MethodPut, seriesURL+"?scope=this", owner.AccessToken,
			eventBody("Rewritten", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00"))
		require.Equal(t, http.StatusBadRequest, status, "%s", raw)
		status, raw = helpers.DoJSONStatus(t, http.MethodDelete, seriesURL+"?scope=this", owner.AccessToken, nil)
		require.Equal(t, http.StatusBadRequest, status, "%s", raw)

		var after []recInstance
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", owner.AccessToken, nil, &after)
		require.Len(t, after, 4)
		for _, e := range after {
			require.Equal(t, "Weekly meeting", e.Title)
		}
	})
}

// An edited occurrence can be handed to another member, and GET then reports
// them as its owner. Being its owner lets them change that occurrence -- and
// nothing else of the series.
func TestTheOwnerOfAnEditedOccurrenceMayEditItAndNoSibling(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")
	occ := createWeeklyFriday(t, calURL, owner.AccessToken)

	handed := eventBody("Your turn", "2026-04-10T15:00:00+09:00", "2026-04-10T16:00:00+09:00")
	handed["ownerId"] = member.UserID
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+occ[1].ID+"?scope=this", owner.AccessToken, handed, nil)

	var got struct {
		OwnerID string `json:"ownerId"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+occ[1].ID, owner.AccessToken, nil, &got)
	require.Equal(t, member.UserID, got.OwnerID)

	status, raw := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occ[1].ID+"?scope=this", member.AccessToken,
		eventBody("Moved by its owner", "2026-04-10T17:00:00+09:00", "2026-04-10T18:00:00+09:00"))
	require.Equal(t, http.StatusOK, status, "the owner GET reports may edit the occurrence: %s", raw)

	status, _ = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occ[2].ID+"?scope=this", member.AccessToken,
		eventBody("Not mine", "2026-04-17T17:00:00+09:00", "2026-04-17T18:00:00+09:00"))
	require.Equal(t, http.StatusForbidden, status, "a sibling occurrence is not theirs")

	all := eventBody("Not mine either", "2026-04-10T17:00:00+09:00", "2026-04-10T18:00:00+09:00")
	all["recurrenceRule"] = map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}}
	status, _ = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+occ[1].ID+"?scope=all", member.AccessToken, all)
	require.Equal(t, http.StatusForbidden, status, "nor is the series")
}
