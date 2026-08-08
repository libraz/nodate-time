package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestConcurrentFirstEditsOfOccurrencesDoNotDeadlock edits several occurrences
// of one series at the same time, each for the first time.
//
// A first edit writes an override row that points at the series it belongs to,
// which takes a shared lock on the parent row to check that foreign key. The
// same transaction then updates that parent row -- to carry the series end
// past a dragged occurrence, and to bump the revision the series answers with
// -- and those need an exclusive lock on the row it already holds a shared one
// on. Two people editing different occurrences each hold the shared lock and
// each wait for the other to let go. MySQL breaks the tie by rolling one back,
// and the person who dragged an occurrence gets a 500.
//
// One series is enough, and that is the point: every edit of it converges on
// the same parent row, so this needs no coincidence beyond two people working
// at once. It stayed rare only because a first edit of a given occurrence
// happens once.
//
// The fix is ordering -- the parent is written before the statement that
// shares it -- so nothing here needs to know about locks. The test only has to
// put two first edits in flight together.
func TestConcurrentFirstEditsOfOccurrencesDoNotDeadlock(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Four Fridays, none of them yet overridden.
	occurrences := createWeeklyFriday(t, calURL, owner.AccessToken)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	statuses := make([]int, len(occurrences))
	bodies := make([]string, len(occurrences))

	for i, occ := range occurrences {
		done.Add(1)
		go func(i int, id string) {
			defer done.Done()
			start.Wait()
			status, body := helpers.DoJSONStatus(t, http.MethodPut,
				calURL+"/events/"+id+"?scope=this", owner.AccessToken,
				map[string]any{
					"title":              fmt.Sprintf("Moved occurrence %d", i),
					"allDay":             false,
					"startAt":            "2026-04-03T18:00:00+09:00",
					"endAt":              "2026-04-03T19:00:00+09:00",
					"location":           "",
					"memo":               "",
					"url":                "",
					"notificationOffset": nil,
					"participants":       []string{},
					"ownerId":            nil,
					"recurrenceRule":     nil,
				})
			statuses[i] = status
			bodies[i] = string(body)
		}(i, occ.ID)
	}
	start.Done()
	done.Wait()

	for i, status := range statuses {
		require.True(t, status >= 200 && status < 300,
			"editing occurrence %d beside its siblings must not fail: %s", i, bodies[i])
	}
}
