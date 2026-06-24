package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recInstance struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	StartAt      string `json:"startAt"`
	IsRecurrence bool   `json:"isRecurrence"`
}

// createWeeklyFriday creates a weekly-on-Friday series starting 2026-04-03 and
// returns the four April instances (3, 10, 17, 24) sorted by start.
func createWeeklyFriday(t *testing.T, calURL, token string) []recInstance {
	t.Helper()
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token,
		map[string]any{
			"title":   "Weekly meeting",
			"allDay":  false,
			"startAt": "2026-04-03T15:00:00+09:00", // Friday
			"endAt":   "2026-04-03T16:00:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"FR"},
			},
		}, &evt)
	require.NotEmpty(t, evt.ID)

	var evts []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", token, nil, &evts)
	require.Len(t, evts, 4)
	return evts
}

// TestRecurringEditSingleOccurrence verifies that editing one occurrence with
// scope=this creates an override and leaves the rest of the series untouched.
func TestRecurringEditSingleOccurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)
	target := evts[1] // 2026-04-10
	require.True(t, strings.Contains(target.ID, "_"))

	// Edit only this occurrence: new title and a shifted time.
	var updated recInstance
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+target.ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title":   "Moved standup",
			"allDay":  false,
			"startAt": "2026-04-10T18:00:00+09:00",
			"endAt":   "2026-04-10T19:00:00+09:00",
		}, &updated)
	assert.Equal(t, "Moved standup", updated.Title)
	assert.True(t, updated.IsRecurrence)
	// The composite ID stays anchored to the original occurrence date.
	assert.Equal(t, target.ID, updated.ID)

	// Re-list: exactly one instance changed, the other three are unchanged.
	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &after)
	require.Len(t, after, 4)
	moved := 0
	original := 0
	for _, e := range after {
		switch e.Title {
		case "Moved standup":
			moved++
			assert.True(t, strings.Contains(e.StartAt, "T09:00"), "override should keep its shifted UTC time, got %s", e.StartAt)
		case "Weekly meeting":
			original++
		}
	}
	assert.Equal(t, 1, moved, "only one occurrence should be overridden")
	assert.Equal(t, 3, original, "the rest of the series stays intact")

	// Fetching the overridden instance returns the override data.
	var got recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+target.ID, tt.AccessToken, nil, &got)
	assert.Equal(t, "Moved standup", got.Title)
}

// TestRecurringDeleteSingleOccurrence verifies that deleting one occurrence with
// scope=this removes only that instance and preserves the rest of the series.
func TestRecurringDeleteSingleOccurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)
	target := evts[1] // 2026-04-10

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+target.ID+"?scope=this", tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// Three instances remain, and the deleted one is absent.
	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &after)
	require.Len(t, after, 3)
	for _, e := range after {
		assert.NotEqual(t, target.ID, e.ID, "the cancelled occurrence must not reappear")
	}

	// The cancelled instance is no longer individually retrievable.
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+target.ID, tt.AccessToken, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// TestRecurringDeleteAllPurgesOverrides verifies that a whole-series delete
// cascades to override rows so no orphan instances linger.
func TestRecurringDeleteAllPurgesOverrides(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)

	// Override one occurrence first.
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evts[2].ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title":   "Special",
			"allDay":  false,
			"startAt": "2026-04-17T20:00:00+09:00",
			"endAt":   "2026-04-17T21:00:00+09:00",
		}, nil)

	// Delete the entire series.
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evts[0].ID+"?scope=all", tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &after)
	require.Len(t, after, 0, "series delete must also remove overrides")
}
