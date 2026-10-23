package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// race starts every call at once and returns their statuses in order.
func race(calls ...func() int) []int {
	statuses := make([]int, len(calls))
	var start, done sync.WaitGroup
	start.Add(1)
	for i, call := range calls {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			statuses[i] = call()
		}()
	}
	start.Done()
	done.Wait()
	return statuses
}

func eventCount(t *testing.T, calendarID, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM events e JOIN calendars c ON c.id = e.calendar_id
		  WHERE c.public_id = UUID_TO_BIN(?) AND e.type = ?`, calendarID, eventType).Scan(&n))
	return n
}

// lastEventID is the id of the newest event of a type in a calendar, 0 if none.
func lastEventID(t *testing.T, calendarID, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COALESCE(MAX(e.id), 0) FROM events e JOIN calendars c ON c.id = e.calendar_id
		  WHERE c.public_id = UUID_TO_BIN(?) AND e.type = ?`, calendarID, eventType).Scan(&n))
	return n
}

func roleOf(t *testing.T, calURL, token, userID string) string {
	t.Helper()
	var members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", token, nil, &members)
	for _, m := range members {
		if m.ID == userID {
			return m.Role
		}
	}
	return ""
}

// A manager's demotion of a member races the owner's promotion of the same
// member. The owner always wins the promotion, so the member ends as an owner
// whichever order they commit in: a demotion that decided on the role read
// before the promotion would commit over it.
func TestPromotionRacingDemotionNeverLosesTheOwnerGrant(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for round := range 15 {
		owner := helpers.NewTenant(t, testServerURL)
		manager := helpers.NewTenant(t, testServerURL)
		target := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, manager, "editor")
		joinAs(t, calURL, owner.AccessToken, target, "editor")
		require.Equal(t, 200, promote(t, calURL, owner.AccessToken, manager, "manager"))
		require.Equal(t, 200, promote(t, calURL, owner.AccessToken, target, "manager"))

		statuses := race(
			func() int { return promote(t, calURL, owner.AccessToken, target, "owner") },
			func() int { return promote(t, calURL, manager.AccessToken, target, "viewer") },
		)

		require.Equal(t, 200, statuses[0], "round %d: the owner's promotion always lands", round)
		require.Contains(t, []int{200, 403}, statuses[1], "round %d", round)
		require.Equal(t, "owner", roleOf(t, calURL, owner.AccessToken, target.UserID),
			"round %d: a manager's demotion must not commit over an owner grant (statuses %v)", round, statuses)
	}
}

// Two owners demoting each other at the same moment leave one of them.
func TestOwnersDemotingEachOtherLeaveOneOwner(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for round := range 15 {
		first := helpers.NewTenant(t, testServerURL)
		second := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + first.CalendarID
		joinAs(t, calURL, first.AccessToken, second, "editor")
		require.Equal(t, 200, promote(t, calURL, first.AccessToken, second, "owner"))

		race(
			func() int { return promote(t, calURL, first.AccessToken, second, "editor") },
			func() int { return promote(t, calURL, second.AccessToken, first, "editor") },
		)

		var owners int
		require.NoError(t, testDB.QueryRow(
			`SELECT COUNT(*) FROM calendar_members cm JOIN calendars c ON c.id = cm.calendar_id
			  WHERE c.public_id = UUID_TO_BIN(?) AND cm.role = 'owner' AND cm.enabled = TRUE`,
			first.CalendarID).Scan(&owners))
		require.Equal(t, 1, owners, "round %d: the calendar keeps exactly one owner", round)
	}
}

// A colour change racing the removal of its target is either applied before the
// removal or refused as a missing member; it never answers for a member who is
// no longer there.
func TestColourChangeRacingRemovalOfTheTarget(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for round := range 15 {
		owner := helpers.NewTenant(t, testServerURL)
		target := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, target, "editor")

		statuses := race(
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/members/"+target.UserID+"/color",
					owner.AccessToken, map[string]any{"color": "#E73B3B"})
				return status
			},
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+target.UserID, owner.AccessToken, nil)
				return status
			},
		)

		require.Contains(t, []int{200, 404}, statuses[0], "round %d", round)
		require.Equal(t, 204, statuses[1], "round %d", round)
		require.Empty(t, roleOf(t, calURL, owner.AccessToken, target.UserID), "round %d", round)
	}
}

// Two managers editing different fields of one calendar keep both edits.
func TestConcurrentCalendarEditsOfDifferentFieldsKeepBoth(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for round := range 15 {
		owner := helpers.NewTenant(t, testServerURL)
		manager := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, manager, "editor")
		require.Equal(t, 200, promote(t, calURL, owner.AccessToken, manager, "manager"))

		name := fmt.Sprintf("Renamed %d", round)
		statuses := race(
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, owner.AccessToken, map[string]any{"name": name})
				return status
			},
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, manager.AccessToken, map[string]any{"color": "#8F8F8F"})
				return status
			},
		)
		require.Equal(t, []int{200, 200}, statuses, "round %d", round)

		var got struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL, owner.AccessToken, nil, &got)
		require.Equal(t, name, got.Name, "round %d: the colour edit wrote the old name back", round)
		require.Equal(t, "#8F8F8F", got.Color, "round %d: the rename wrote the old colour back", round)
	}
}

// An edit that loses the race with the calendar's deletion is refused, and the
// log never shows the calendar edited after it was deleted.
func TestCalendarEditRacingDeletionLogsNothingAfterIt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	for round := range 15 {
		calendarID := newCalendar(t, owner, fmt.Sprintf("Doomed %d", round))
		calURL := testServerURL + "/calendars/" + calendarID

		statuses := race(
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, owner.AccessToken, map[string]any{"name": "late"})
				return status
			},
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, owner.AccessToken, nil)
				return status
			},
		)

		require.Equal(t, 204, statuses[1], "round %d", round)
		require.Contains(t, []int{200, 404}, statuses[0], "round %d", round)
		edited := lastEventID(t, calendarID, "calendar.updated")
		deleted := lastEventID(t, calendarID, "calendar.deleted")
		require.NotZero(t, deleted, "round %d", round)
		require.Less(t, edited, deleted, "round %d: an edit was logged after the deletion", round)
	}
}

func createMemo(t *testing.T, calURL, token, title string) string {
	t.Helper()
	var memo struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/memos", token, map[string]any{"title": title, "sortOrder": 0}, &memo)
	require.NotEmpty(t, memo.ID)
	return memo.ID
}

// Two deletes of one memo are one deletion: one succeeds, the other finds
// nothing, and the log holds a single delete event.
func TestConcurrentDeletesOfOneMemoLogOnce(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	for round := range 15 {
		memoID := createMemo(t, calURL, tt.AccessToken, fmt.Sprintf("memo %d", round))
		before := eventCount(t, tt.CalendarID, "calendar.memo.deleted")

		statuses := concurrently(t, 4, http.MethodDelete, calURL+"/memos/"+memoID, tt.AccessToken)

		deleted, missing := 0, 0
		for _, s := range statuses {
			switch s {
			case http.StatusNoContent:
				deleted++
			case http.StatusNotFound:
				missing++
			}
		}
		require.Equal(t, 1, deleted, "round %d: %v", round, statuses)
		require.Equal(t, 3, missing, "round %d: %v", round, statuses)
		require.Equal(t, before+1, eventCount(t, tt.CalendarID, "calendar.memo.deleted"), "round %d", round)
	}
}

// Partial updates of different fields of one memo both survive, and an update
// that loses to a delete finds nothing.
func TestConcurrentMemoUpdatesKeepEachOthersFields(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	for round := range 15 {
		memoID := createMemo(t, calURL, tt.AccessToken, "before")
		title := fmt.Sprintf("after %d", round)

		statuses := race(
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/memos/"+memoID, tt.AccessToken, map[string]any{"done": true})
				return status
			},
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/memos/"+memoID, tt.AccessToken, map[string]any{"title": title})
				return status
			},
		)
		require.Equal(t, []int{200, 200}, statuses, "round %d", round)

		var memos struct {
			Items []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Done  bool   `json:"done"`
			} `json:"items"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL+"/memos?limit=200", tt.AccessToken, nil, &memos)
		found := false
		for _, m := range memos.Items {
			if m.ID == memoID {
				found = true
				require.Equal(t, title, m.Title, "round %d: the tick wrote the old title back", round)
				require.True(t, m.Done, "round %d: the rename reopened the memo", round)
			}
		}
		require.True(t, found, "round %d", round)
	}

	memoID := createMemo(t, calURL, tt.AccessToken, "doomed")
	before := eventCount(t, tt.CalendarID, "calendar.memo.updated")
	statuses := race(
		func() int {
			status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/memos/"+memoID, tt.AccessToken, map[string]any{"done": true})
			return status
		},
		func() int {
			status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/memos/"+memoID, tt.AccessToken, nil)
			return status
		},
	)
	require.Equal(t, 204, statuses[1])
	require.Contains(t, []int{200, 404}, statuses[0], "an update that loses to the delete is not a server error")
	if statuses[0] == http.StatusNotFound {
		require.Equal(t, before, eventCount(t, tt.CalendarID, "calendar.memo.updated"), "a refused update logs nothing")
	}
}

// A cursor naming a memo deleted since the page was served still resumes the
// list; one from another calendar is refused.
func TestMemoCursorSurvivesTheDeletionOfItsMemo(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	first := createMemo(t, calURL, tt.AccessToken, "first")
	second := createMemo(t, calURL, tt.AccessToken, "second")

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos?limit=1", tt.AccessToken, nil, &page)
	require.Len(t, page.Items, 1)
	require.Equal(t, first, page.Items[0].ID)
	require.NotEmpty(t, page.NextCursor)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/memos/"+first, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	var next struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos?limit=1&cursor="+page.NextCursor, tt.AccessToken, nil, &next)
	require.Len(t, next.Items, 1)
	require.Equal(t, second, next.Items[0].ID)

	other := helpers.NewTenant(t, testServerURL)
	foreign := createMemo(t, testServerURL+"/calendars/"+other.CalendarID, other.AccessToken, "elsewhere")
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/memos?limit=1&cursor="+foreign, tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, "a cursor from another calendar is still refused")
}

// Two deletes of one photo are one deletion.
func TestConcurrentDeletesOfOnePhotoLogOnce(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	for round := range 10 {
		photoID := uploadPhotoBytes(t, tt, tt.CalendarID, distinctPNG(fmt.Sprintf("double-delete-%d", round)), "twice")
		before := eventCount(t, tt.CalendarID, "calendar.photo.deleted")

		statuses := concurrently(t, 4, http.MethodDelete, calURL+"/albums/"+photoID, tt.AccessToken)

		deleted := 0
		for _, s := range statuses {
			if s == http.StatusNoContent {
				deleted++
			} else {
				require.Equal(t, http.StatusNotFound, s, "round %d: %v", round, statuses)
			}
		}
		require.Equal(t, 1, deleted, "round %d: %v", round, statuses)
		require.Equal(t, before+1, eventCount(t, tt.CalendarID, "calendar.photo.deleted"), "round %d", round)
	}
}

// One person following a single-use link twice at once joins once: the second
// request finds the membership the first made, spends nothing and logs no
// second join.
func TestDoubleAcceptOfASingleUseLinkJoinsOnce(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	for round := range 10 {
		joiner := helpers.NewTenant(t, testServerURL)
		calendarID := newCalendar(t, owner, fmt.Sprintf("Double accept %d", round))
		calURL := testServerURL + "/calendars/" + calendarID
		inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"role": "editor", "maxUses": 1})

		statuses := race(
			func() int { status, _ := acceptInvite(t, inv.Token, joiner.AccessToken); return status },
			func() int { status, _ := acceptInvite(t, inv.Token, joiner.AccessToken); return status },
		)

		require.Equal(t, []int{200, 200}, statuses, "round %d", round)
		require.Equal(t, 1, useCountOf(t, calURL, owner.AccessToken, inv.ID), "round %d", round)
		require.Equal(t, 1, eventCount(t, calendarID, "calendar.member.joined"), "round %d", round)
	}
}

// A link created while its calendar is being deleted is either refused or
// logged before the deletion; the log never shows one created after it.
func TestInviteCreationRacingCalendarDeletion(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	for round := range 15 {
		calendarID := newCalendar(t, owner, fmt.Sprintf("Doomed invite %d", round))
		calURL := testServerURL + "/calendars/" + calendarID

		statuses := race(
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", owner.AccessToken, map[string]any{"role": "editor"})
				return status
			},
			func() int {
				status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, owner.AccessToken, nil)
				return status
			},
		)

		require.Equal(t, 204, statuses[1], "round %d", round)
		require.Contains(t, []int{200, 404}, statuses[0], "round %d", round)
		created := lastEventID(t, calendarID, "calendar.invite.created")
		deleted := lastEventID(t, calendarID, "calendar.deleted")
		require.Less(t, created, deleted, "round %d: a link was created after the calendar was deleted", round)
	}
}
