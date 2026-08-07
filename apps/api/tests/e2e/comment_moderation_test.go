package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestWhoeverRunsTheCalendarCanTakeACommentDown covers moderation on a shared
// calendar. Comments are open to every member by design, which is what makes
// the removal path necessary: without it the only remedy for something posted
// on a family or team calendar is to delete the event it hangs off.
//
// Editing stays the author's alone. Taking words off the wall and putting
// different words in someone's mouth are not the same power.
func TestWhoeverRunsTheCalendarCanTakeACommentDown(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	author := helpers.NewTenant(t, testServerURL)
	bystander := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, author, "editor")
	joinAs(t, calURL, owner.AccessToken, bystander, "editor")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Planning",
			"allDay":  false,
			"startAt": "2026-07-02T10:00:00+09:00",
			"endAt":   "2026-07-02T11:00:00+09:00",
		}, &evt)
	eventURL := calURL + "/events/" + evt.ID

	post := func(token, body string) string {
		t.Helper()
		var c struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, eventURL+"/activities", token,
			map[string]any{"content": body}, &c)
		return c.ID
	}

	// Another member cannot remove what someone else wrote just by being an
	// editor: writing to the calendar is not moderating it.
	first := post(author.AccessToken, "見学の集合は 9 時です")
	byPeer, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		eventURL+"/activities/"+first, bystander.AccessToken, nil)
	require.Equal(t, 403, byPeer)

	// Whoever runs the calendar can.
	byOwner, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		eventURL+"/activities/"+first, owner.AccessToken, nil)
	require.True(t, byOwner >= 200 && byOwner < 300, "owner should be able to moderate, got %d", byOwner)

	var remaining []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, eventURL+"/activities", owner.AccessToken, nil, &remaining)
	for _, c := range remaining {
		require.NotEqual(t, first, c.ID, "the removed comment should be gone from the thread")
	}

	// A manager holds the same power -- moderation belongs to running the
	// calendar, not to owning it.
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, bystander, "manager"))
	second := post(author.AccessToken, "やっぱり 10 時にします")
	byManager, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		eventURL+"/activities/"+second, bystander.AccessToken, nil)
	require.True(t, byManager >= 200 && byManager < 300, "manager should be able to moderate, got %d", byManager)

	// Rewriting somebody else's comment stays refused for both of them.
	third := post(author.AccessToken, "資料は当日配ります")
	ownerEdit, _ := helpers.DoJSONStatus(t, http.MethodPut,
		eventURL+"/activities/"+third, owner.AccessToken, map[string]any{"content": "資料は不要です"})
	require.Equal(t, 403, ownerEdit, "moderation is removal, not rewriting")

	// And the author still governs their own comment.
	authorEdit, _ := helpers.DoJSONStatus(t, http.MethodPut,
		eventURL+"/activities/"+third, author.AccessToken, map[string]any{"content": "資料は前日に配ります"})
	require.Equal(t, 200, authorEdit)
}
