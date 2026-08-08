package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestConcurrentEventCreationDoesNotDeadlock creates events on one calendar
// from several requests at once, each naming a participant.
//
// Creating an event disables the attendees the event does not have yet, then
// inserts the ones it does. On a row created moments earlier the first step
// matches nothing, but the new event id is above every value in the attendee
// index, so the scan holds a gap lock at the end of it -- and the insert that
// follows needs to write into that same gap. Two creators are enough: each
// holds the gap the other must write into, and MySQL rolls one of them back.
//
// The participant matters. Without one there is no insert, so nothing collides
// with the gap lock and the deadlock never appears.
func TestConcurrentEventCreationDoesNotDeadlock(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept",
		guest.AccessToken, nil, nil)

	const writers = 8
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	statuses := make([]int, writers)
	bodies := make([]string, writers)

	for i := range writers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events",
				owner.AccessToken, map[string]any{
					"title":        fmt.Sprintf("Concurrent %d", i),
					"allDay":       false,
					"startAt":      "2026-04-22T12:00:00+09:00",
					"endAt":        "2026-04-22T13:00:00+09:00",
					"participants": []string{guest.UserID},
				})
			statuses[i] = status
			bodies[i] = string(body)
		}(i)
	}
	start.Done()
	done.Wait()

	for i, status := range statuses {
		require.Equal(t, http.StatusCreated, status,
			"writer %d: creating an event beside another must not fail: %s", i, bodies[i])
	}
}
