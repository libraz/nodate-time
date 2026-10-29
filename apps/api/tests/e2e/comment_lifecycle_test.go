package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// commentThread creates an event on the tenant's calendar and returns its URL
// and a function posting a comment on it.
func commentThread(t *testing.T, tt *helpers.TestTenant) (eventURL string, post func(body string) string) {
	t.Helper()
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		eventBody("Discussed", "2026-07-02T10:00:00+09:00", "2026-07-02T11:00:00+09:00"), &evt)
	eventURL = calURL + "/events/" + evt.ID
	return eventURL, func(body string) string {
		t.Helper()
		var c struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, eventURL+"/activities", tt.AccessToken,
			map[string]any{"content": body}, &c)
		return c.ID
	}
}

// commentLogRows counts the log rows of one type recorded for one comment.
func commentLogRows(t *testing.T, eventType, commentID string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(`
		SELECT COUNT(*) FROM events
		 WHERE type = ? AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.commentId')) = ?`,
		eventType, commentID).Scan(&n))
	return n
}

// The history and the activity feed are readable by every member, viewers
// included, and they are append-only. Text written into them would outlive
// the edit that replaced it and the removal that took it down.
func TestEditedAndRemovedCommentTextIsNotInHistoryOrActivity(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, viewer, "viewer")

	eventURL, post := commentThread(t, owner)
	nonce := time.Now().UnixNano()
	original := fmt.Sprintf("door code %d", nonce)
	edited := fmt.Sprintf("door code withdrawn %d", nonce)

	id := post(original)
	helpers.DoJSON(t, http.MethodPut, eventURL+"/activities/"+id, owner.AccessToken,
		map[string]any{"content": edited}, nil)
	helpers.DoJSON(t, http.MethodDelete, eventURL+"/activities/"+id, owner.AccessToken, nil, nil)

	for name, url := range map[string]string{
		"event history": eventURL + "/history",
		"activity feed": calURL + "/activity?limit=200",
	} {
		status, raw := helpers.DoJSONStatus(t, http.MethodGet, url, viewer.AccessToken, nil)
		require.Equal(t, http.StatusOK, status, "%s: %s", name, raw)
		require.NotContains(t, string(raw), "door code", "%s still carries comment text", name)
	}
	require.Equal(t, 1, commentLogRows(t, "calendar.event.comment.deleted", id), "the removal itself is still recorded")
}

// The cursor names a position in the thread. The comment it was taken from
// being removed since does not move that position, so the older comments
// behind it stay reachable.
func TestOlderCommentsStillLoadAfterTheCursorCommentIsRemoved(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	eventURL, post := commentThread(t, owner)
	oldest := post("first")
	post("second")
	post("third")

	type page struct {
		Items []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	var newest page
	helpers.DoJSON(t, http.MethodGet, eventURL+"/activities?limit=2", owner.AccessToken, nil, &newest)
	require.Len(t, newest.Items, 2)
	require.NotEmpty(t, newest.NextCursor)

	helpers.DoJSON(t, http.MethodDelete, eventURL+"/activities/"+newest.NextCursor, owner.AccessToken, nil, nil)

	status, raw := helpers.DoJSONStatus(t, http.MethodGet,
		eventURL+"/activities?limit=2&cursor="+newest.NextCursor, owner.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "%s", raw)
	require.Contains(t, string(raw), oldest, "the comment behind the removed cursor is still reachable")
}

// Two removals of one comment -- the author and a moderator, or a double
// click -- record one removal between them, and the second is told there was
// nothing left to remove.
func TestConcurrentRemovalsOfOneCommentRecordOne(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	eventURL, post := commentThread(t, owner)
	id := post("said twice")

	statuses := concurrently(t, 6, http.MethodDelete, eventURL+"/activities/"+id, owner.AccessToken)
	require.Equal(t, 1, countStatus(statuses, http.StatusNoContent), "statuses %v", statuses)
	require.Equal(t, len(statuses)-1, countStatus(statuses, http.StatusNotFound), "statuses %v", statuses)
	require.Equal(t, 1, commentLogRows(t, "calendar.event.comment.deleted", id))
}

// An edit racing the removal of the same comment either lands before it or is
// told the comment is gone; it is never applied to, or recorded against, a
// comment already removed.
func TestAnEditRacingARemovalNeverLandsAfterIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	eventURL, post := commentThread(t, owner)

	for round := range 8 {
		id := post(fmt.Sprintf("round %d", round))
		statuses := runTogether(2, func(i int) int {
			if i == 0 {
				status, _ := helpers.DoJSONStatus(t, http.MethodDelete, eventURL+"/activities/"+id, owner.AccessToken, nil)
				return status
			}
			status, _ := helpers.DoJSONStatus(t, http.MethodPut, eventURL+"/activities/"+id, owner.AccessToken,
				map[string]any{"content": fmt.Sprintf("round %d, edited", round)})
			return status
		})
		require.Equal(t, http.StatusNoContent, statuses[0])
		require.Contains(t, []int{http.StatusOK, http.StatusNotFound}, statuses[1])

		var editedAfterRemoval int
		require.NoError(t, testDB.QueryRow(`
			SELECT COUNT(*) FROM events edited
			  JOIN events removed
			    ON JSON_UNQUOTE(JSON_EXTRACT(removed.payload_json, '$.commentId')) = ?
			   AND removed.type = 'calendar.event.comment.deleted'
			 WHERE edited.type = 'calendar.event.comment.updated'
			   AND JSON_UNQUOTE(JSON_EXTRACT(edited.payload_json, '$.commentId')) = ?
			   AND edited.id > removed.id`, id, id).Scan(&editedAfterRemoval))
		require.Zero(t, editedAfterRemoval, "round %d: an edit was recorded after the removal", round)
		if statuses[1] == http.StatusNotFound {
			require.Zero(t, commentLogRows(t, "calendar.event.comment.updated", id), "round %d", round)
		}
	}
}

// runTogether runs n calls of fn at once and returns their results in order.
func runTogether(n int, fn func(i int) int) []int {
	var start, done sync.WaitGroup
	start.Add(1)
	out := make([]int, n)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			out[i] = fn(i)
		}()
	}
	start.Done()
	done.Wait()
	return out
}
