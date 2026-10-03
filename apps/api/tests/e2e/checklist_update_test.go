package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type checklistItemResp struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Done      bool   `json:"done"`
	SortOrder int    `json:"sortOrder"`
}

// Ticking an item sends only done. A title or position the caller read earlier
// is not resent, so another member's rename or reorder made since survives.
func TestTickingAChecklistItemKeepsItsTitleAndPosition(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Packing", "allDay": false,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)
	checklistURL := calURL + "/events/" + evt.ID + "/checklist"

	var item checklistItemResp
	helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
		map[string]any{"title": "Tent", "sortOrder": 3}, &item)

	// Somebody else renames it.
	var renamed checklistItemResp
	helpers.DoJSON(t, http.MethodPut, checklistURL+"/"+item.ID, tt.AccessToken,
		map[string]any{"title": "Two tents"}, &renamed)
	require.Equal(t, "Two tents", renamed.Title)
	require.False(t, renamed.Done, "an omitted done keeps the stored value")

	var ticked checklistItemResp
	helpers.DoJSON(t, http.MethodPut, checklistURL+"/"+item.ID, tt.AccessToken,
		map[string]any{"done": true}, &ticked)
	require.True(t, ticked.Done)
	require.Equal(t, "Two tents", ticked.Title)
	require.Equal(t, 3, ticked.SortOrder)
}
