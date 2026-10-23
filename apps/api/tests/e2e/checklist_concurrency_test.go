package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// checklistFor creates an event and returns its checklist URL.
func checklistFor(t *testing.T, tt *helpers.TestTenant) string {
	t.Helper()
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		eventBody("Packing", "2026-05-12T09:00:00+09:00", "2026-05-12T10:00:00+09:00"), &evt)
	return calURL + "/events/" + evt.ID + "/checklist"
}

// checklistLogRows counts the log rows of one type recorded for one item.
func checklistLogRows(t *testing.T, eventType, itemID string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(`
		SELECT COUNT(*) FROM events
		 WHERE type = ? AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.subject')) = ?`,
		eventType, itemID).Scan(&n))
	return n
}

// Racing removals of one item record one removal between them; the rest are
// told there was nothing left to remove.
func TestConcurrentRemovalsOfOneChecklistItemRecordOne(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	checklistURL := checklistFor(t, tt)
	var item checklistItemResp
	helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken, map[string]any{"title": "Tent"}, &item)

	statuses := concurrently(t, 6, http.MethodDelete, checklistURL+"/"+item.ID, tt.AccessToken)
	require.Equal(t, 1, countStatus(statuses, http.StatusNoContent), "statuses %v", statuses)
	require.Equal(t, len(statuses)-1, countStatus(statuses, http.StatusNotFound), "statuses %v", statuses)
	require.Equal(t, 1, checklistLogRows(t, "calendar.checklist.removed", item.ID))
}

// Two members changing different fields of one item at once both keep their
// change: each merges over the item as it stands when its write runs, not as
// it was when the request arrived.
func TestConcurrentPartialUpdatesOfAChecklistItemKeepBothFields(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	checklistURL := checklistFor(t, tt)

	for round := range 8 {
		var item checklistItemResp
		helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
			map[string]any{"title": fmt.Sprintf("Tent %d", round)}, &item)
		itemURL := checklistURL + "/" + item.ID
		renamed := fmt.Sprintf("Two tents %d", round)

		statuses := runTogether(2, func(i int) int {
			body := map[string]any{"done": true}
			if i == 1 {
				body = map[string]any{"title": renamed}
			}
			status, _ := helpers.DoJSONStatus(t, http.MethodPut, itemURL, tt.AccessToken, body)
			return status
		})
		require.Equal(t, []int{http.StatusOK, http.StatusOK}, statuses)

		var items []checklistItemResp
		helpers.DoJSON(t, http.MethodGet, checklistURL, tt.AccessToken, nil, &items)
		var got checklistItemResp
		for _, it := range items {
			if it.ID == item.ID {
				got = it
			}
		}
		require.True(t, got.Done, "round %d: the tick was lost", round)
		require.Equal(t, renamed, got.Title, "round %d: the rename was lost", round)
	}
}

// An update racing the item's removal either lands before it or is told the
// item is gone -- never a 500, and never an update recorded after the removal.
func TestAChecklistUpdateRacingARemovalNeverLandsAfterIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	checklistURL := checklistFor(t, tt)

	for round := range 8 {
		var item checklistItemResp
		helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
			map[string]any{"title": fmt.Sprintf("Stove %d", round)}, &item)
		itemURL := checklistURL + "/" + item.ID

		statuses := runTogether(2, func(i int) int {
			if i == 0 {
				status, _ := helpers.DoJSONStatus(t, http.MethodDelete, itemURL, tt.AccessToken, nil)
				return status
			}
			status, _ := helpers.DoJSONStatus(t, http.MethodPut, itemURL, tt.AccessToken, map[string]any{"done": true})
			return status
		})
		require.Equal(t, http.StatusNoContent, statuses[0])
		require.Contains(t, []int{http.StatusOK, http.StatusNotFound}, statuses[1], "round %d", round)

		var updatedAfterRemoval int
		require.NoError(t, testDB.QueryRow(`
			SELECT COUNT(*) FROM events updated
			  JOIN events removed
			    ON JSON_UNQUOTE(JSON_EXTRACT(removed.payload_json, '$.subject')) = ?
			   AND removed.type = 'calendar.checklist.removed'
			 WHERE updated.type = 'calendar.checklist.updated'
			   AND JSON_UNQUOTE(JSON_EXTRACT(updated.payload_json, '$.subject')) = ?
			   AND updated.id > removed.id`, item.ID, item.ID).Scan(&updatedAfterRemoval))
		require.Zero(t, updatedAfterRemoval, "round %d: an update was recorded after the removal", round)
	}
}
