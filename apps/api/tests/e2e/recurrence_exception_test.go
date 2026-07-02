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

// TestRecurringEditAllFromOccurrencePreservesPastInstances verifies scope=all
// from an expanded occurrence shifts the master by delta instead of re-anchoring
// the whole series to the occurrence date.
func TestRecurringEditAllFromOccurrencePreservesPastInstances(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)
	target := evts[2] // 2026-04-17 occurrence
	require.True(t, strings.Contains(target.ID, "_"))

	var updated recInstance
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+target.ID+"?scope=all", tt.AccessToken,
		map[string]any{
			"title":   "Weekly meeting shifted",
			"allDay":  false,
			"startAt": "2026-04-17T18:00:00+09:00",
			"endAt":   "2026-04-17T19:00:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"FR"},
			},
		}, &updated)
	assert.False(t, strings.Contains(updated.ID, "_"))
	assert.Contains(t, updated.StartAt, "T09:00:00Z")

	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &after)
	require.Len(t, after, 4)
	for _, e := range after {
		assert.Equal(t, "Weekly meeting shifted", e.Title)
		assert.Contains(t, e.StartAt, "T09:00:00Z", "all instances should shift to 18:00 JST, got %s", e.StartAt)
	}
}

// TestRecurringDeletedOccurrenceStaysDeletedAfterSeriesShift verifies a
// cancellation tombstone keeps applying after a whole-series time edit changes
// the absolute occurrence start instant.
func TestRecurringDeletedOccurrenceStaysDeletedAfterSeriesShift(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)
	deleted := evts[1] // 2026-04-10
	parentID := strings.Split(evts[0].ID, "_")[0]

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+deleted.ID+"?scope=this", tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	var updated recInstance
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+parentID+"?scope=all", tt.AccessToken,
		map[string]any{
			"title":   "Weekly meeting shifted later",
			"allDay":  false,
			"startAt": "2026-04-03T16:00:00+09:00",
			"endAt":   "2026-04-03T17:00:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"FR"},
			},
		}, &updated)
	assert.Equal(t, parentID, updated.ID)

	var after []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &after)
	require.Len(t, after, 3, "deleted occurrence must not reappear after a series time shift")
	for _, e := range after {
		assert.NotEqual(t, deleted.ID, e.ID)
		assert.NotContains(t, e.StartAt, "2026-04-10T07:00:00Z")
	}

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+deleted.ID, tt.AccessToken, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

func TestRecurringParticipantsRoundTripOnInstances(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "member"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", guest.AccessToken, nil, nil)

	var master struct {
		ID           string   `json:"id"`
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":        "Weekly with participants",
			"allDay":       false,
			"startAt":      "2026-04-03T15:00:00+09:00",
			"endAt":        "2026-04-03T16:00:00+09:00",
			"participants": []string{guest.UserID},
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"FR"},
			},
		}, &master)
	require.Equal(t, []string{guest.UserID}, master.Participants)

	var instances []struct {
		ID           string   `json:"id"`
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", owner.AccessToken, nil, &instances)
	require.Len(t, instances, 4)
	for _, inst := range instances {
		require.Equal(t, []string{guest.UserID}, inst.Participants)
	}

	var got struct {
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+instances[1].ID, owner.AccessToken, nil, &got)
	require.Equal(t, []string{guest.UserID}, got.Participants)

	var updated struct {
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+instances[1].ID+"?scope=this", owner.AccessToken,
		map[string]any{
			"title":        "Owner-only occurrence",
			"allDay":       false,
			"startAt":      "2026-04-10T15:00:00+09:00",
			"endAt":        "2026-04-10T16:00:00+09:00",
			"participants": []string{owner.UserID},
		}, &updated)
	require.Equal(t, []string{owner.UserID}, updated.Participants)

	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+instances[1].ID, owner.AccessToken, nil, &got)
	require.Equal(t, []string{owner.UserID}, got.Participants)
}
