package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Managers adding the fiftieth label at the same moment get one label between
// them: each holds only its own membership row, so the cap is decided under a
// lock they share.
func TestConcurrentLabelCreatesStopAtTheCap(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	tokens := []string{owner.AccessToken}
	for range 5 {
		manager := helpers.NewTenant(t, testServerURL)
		joinAs(t, calURL, owner.AccessToken, manager, "editor")
		require.Equal(t, http.StatusOK, promote(t, calURL, owner.AccessToken, manager, "manager"))
		tokens = append(tokens, manager.AccessToken)
	}
	for n := range 49 {
		lbCreateLabel(t, calURL, owner.AccessToken, "L"+strings.Repeat("x", n), "#7A7A7A")
	}

	for round := range 8 {
		statuses := runTogether(len(tokens), func(i int) int {
			status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", tokens[i],
				map[string]any{"name": fmt.Sprintf("Race %d-%d", round, i), "color": "#000000"})
			return status
		})
		require.Equal(t, 1, countStatus(statuses, http.StatusCreated), "round %d: %v", round, statuses)
		require.Equal(t, len(tokens)-1, countStatus(statuses, http.StatusConflict), "round %d: %v", round, statuses)

		labels := lbLabels(t, calURL, owner.AccessToken)
		require.Len(t, labels, 50, "round %d", round)
		for _, l := range labels {
			if strings.HasPrefix(l.Name, "Race ") {
				status, raw := lbSend(t, http.MethodDelete, calURL+"/labels/"+l.ID, owner.AccessToken, l.ETag, nil)
				require.Equal(t, http.StatusNoContent, status, string(raw))
			}
		}
	}
}
