package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestConcurrentFirstEditsAcrossSeriesDoNotDeadlock drives first edits of
// occurrences belonging to different series, each naming a participant.
//
// Its subject is the attendee write, not the parent row. A first edit creates
// an override and then replaces that row's attendees -- and the row is new, so
// the scan for rows to disable finds none and holds a gap lock to the end of
// the attendee index, with the insert of the participant behind it. That is
// the shape that deadlocked event creation.
//
// Two things have to hold at once for it to be reachable here, which is why
// the test looks the way it does:
//
//   - The edits must not share a parent series. Concurrent first edits of one
//     series queue on the parent row -- that is what the ordering fix in
//     UpdateEvent does -- and edits that queue never overlap inside the
//     attendee scan. So: one series per calendar, which also keeps
//     createWeeklyFriday's occurrence-count assertion honest.
//   - Each edit must name a participant. With none there is no insert behind
//     the gap lock and nothing to collide. A version of this test that edited
//     bare occurrences would pass whatever the code did.
func TestConcurrentFirstEditsAcrossSeriesDoNotDeadlock(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)

	const series = 6
	type target struct {
		calURL string
		id     string
	}
	targets := make([]target, series)

	for s := range series {
		calURL := testServerURL + "/calendars/" +
			newCalendar(t, owner, fmt.Sprintf("Series %d", s))

		// A participant has to be a member of the calendar the event is on,
		// so the guest joins each one.
		var invite struct {
			Token string `json:"token"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
			map[string]any{"role": "editor"}, &invite)
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept",
			guest.AccessToken, nil, nil)

		occurrences := createWeeklyFriday(t, calURL, owner.AccessToken)
		targets[s] = target{calURL: calURL, id: occurrences[1].ID}
	}

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	statuses := make([]int, series)
	bodies := make([]string, series)

	for i, tgt := range targets {
		done.Add(1)
		go func(i int, tgt target) {
			defer done.Done()
			start.Wait()
			status, body := helpers.DoJSONStatus(t, http.MethodPut,
				tgt.calURL+"/events/"+tgt.id+"?scope=this", owner.AccessToken,
				map[string]any{
					"title":              fmt.Sprintf("Moved series %d", i),
					"allDay":             false,
					"startAt":            "2026-04-10T18:00:00+09:00",
					"endAt":              "2026-04-10T19:00:00+09:00",
					"location":           "",
					"memo":               "",
					"url":                "",
					"notificationOffset": nil,
					"participants":       []string{guest.UserID},
					"ownerId":            nil,
					"recurrenceRule":     nil,
				})
			statuses[i] = status
			bodies[i] = string(body)
		}(i, tgt)
	}
	start.Done()
	done.Wait()

	for i, status := range statuses {
		require.True(t, status >= 200 && status < 300,
			"first edit on series %d must not fail beside the others: %s", i, bodies[i])
	}
}

// TestSecondEditOfAnOccurrenceReplacesItsParticipants covers the other side of
// the branch the fix introduces.
//
// A first edit adds attendees to a row that has none; a later edit of the same
// occurrence has to replace the ones already there. Getting that wrong does
// not deadlock and does not error -- it leaves the person removed from an
// occurrence still attending it, which no other test here would notice,
// because every existing recurrence test edits with an empty participant list.
func TestSecondEditOfAnOccurrenceReplacesItsParticipants(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	for _, guest := range []*helpers.TestTenant{first, second} {
		var invite struct {
			Token string `json:"token"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
			map[string]any{"role": "editor"}, &invite)
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept",
			guest.AccessToken, nil, nil)
	}

	occurrences := createWeeklyFriday(t, calURL, owner.AccessToken)
	target := occurrences[1].ID

	// The occurrence keeps its own times, so its composite id stays the one
	// both edits address.
	edit := func(participant string) {
		t.Helper()
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+target+"?scope=this", owner.AccessToken,
			map[string]any{
				"title":              "Occurrence with a guest",
				"allDay":             false,
				"startAt":            "2026-04-10T15:00:00+09:00",
				"endAt":              "2026-04-10T16:00:00+09:00",
				"location":           "",
				"memo":               "",
				"url":                "",
				"notificationOffset": nil,
				"participants":       []string{participant},
				"ownerId":            nil,
				"recurrenceRule":     nil,
			}, nil)
	}
	participantsOf := func() []string {
		t.Helper()
		var got struct {
			Participants []string `json:"participants"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+target, owner.AccessToken, nil, &got)
		return got.Participants
	}

	edit(first.UserID)
	require.Equal(t, []string{first.UserID}, participantsOf(),
		"the first edit writes the attendee onto a row that had none")

	edit(second.UserID)
	require.Equal(t, []string{second.UserID}, participantsOf(),
		"a later edit replaces the attendee rather than adding beside it")
}
