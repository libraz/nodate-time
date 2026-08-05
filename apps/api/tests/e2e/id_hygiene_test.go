package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestActivityFeedExposesNoInternalIDs verifies that the ids and cursors the
// activity API hands out name rows publicly. The internal ids are one
// AUTO_INCREMENT sequence for the whole deployment, so two of them tell any
// member how much every calendar on the instance wrote in between.
func TestActivityFeedExposesNoInternalIDs(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	for i := range 4 {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			map[string]any{
				"title":   "Entry",
				"allDay":  false,
				"startAt": "2026-08-1" + string(rune('0'+i)) + "T10:00:00+09:00",
				"endAt":   "2026-08-1" + string(rune('0'+i)) + "T11:00:00+09:00",
			}, nil)
	}

	var page struct {
		Items []struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2", owner.AccessToken, nil, &page)
	require.Len(t, page.Items, 2)
	require.NotEmpty(t, page.NextCursor)

	for _, item := range page.Items {
		_, err := uuid.Parse(item.ID)
		require.NoError(t, err, "an activity id must be a public id, got %q", item.ID)
	}
	_, err := uuid.Parse(page.NextCursor)
	require.NoError(t, err, "a cursor must name a row publicly, got %q", page.NextCursor)

	// The cursor still pages: naming rows publicly is not a licence to break
	// the ordering it was doing.
	var second struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2&cursor="+page.NextCursor,
		owner.AccessToken, nil, &second)
	require.NotEmpty(t, second.Items)
	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.ID] = true
	}
	for _, item := range second.Items {
		require.False(t, seen[item.ID], "a cursor must not repeat items across pages")
	}

	// A cursor from another calendar is refused rather than silently paging
	// through this one from an unrelated position.
	stranger := helpers.NewTenant(t, testServerURL)
	badStatus, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+stranger.CalendarID+"/activity?cursor="+page.NextCursor,
		stranger.AccessToken, nil)
	require.Equal(t, 400, badStatus)
}

// TestEventSideChangesReachTheHistory verifies that the things people do to an
// event after creating it are recorded, and under the names the client reads.
func TestEventSideChangesReachTheHistory(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Planning",
			"allDay":  false,
			"startAt": "2026-08-20T10:00:00+09:00",
			"endAt":   "2026-08-20T11:00:00+09:00",
		}, &evt)

	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", owner.AccessToken,
		map[string]any{"content": "who is bringing what"}, nil)
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/checklist", owner.AccessToken,
		map[string]any{"title": "book the room"}, nil)

	var history []struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/history", owner.AccessToken, nil, &history)

	actions := map[string]bool{}
	for _, h := range history {
		actions[h.Action] = true
		_, err := uuid.Parse(h.ID)
		require.NoError(t, err, "a history id must be a public id, got %q", h.ID)
	}
	require.True(t, actions["calendar.event.created"])
	require.True(t, actions["calendar.comment.added"], "a comment must reach the event's history")
	require.True(t, actions["calendar.checklist.added"], "a checklist item must reach the event's history")
}
