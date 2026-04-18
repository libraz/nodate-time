package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()


	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		AllDay   bool   `json:"allDay"`
		Color    string `json:"color"`
		Location string `json:"location"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "定例MTG",
			"allDay":   false,
			"startAt":  "2026-04-20T15:00:00+09:00",
			"endAt":    "2026-04-20T16:00:00+09:00",
			"color":    "#F35F8C",
			"location": "会議室A",
			"memo":     "議題：Q2計画",
		}, &evt)
	require.NotEmpty(t, evt.ID)
	require.Equal(t, "定例MTG", evt.Title)
	require.Equal(t, "#F35F8C", evt.Color)

	// List events in range
	var evts []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts)
	require.Len(t, evts, 1)
	require.Equal(t, evt.ID, evts[0].ID)

	// Get single event
	var got struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Location string `json:"location"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got)
	require.Equal(t, "会議室A", got.Location)

	// Update event
	var updated struct {
		Title string `json:"title"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, tt.AccessToken,
		map[string]any{
			"title":    "定例MTG（更新）",
			"allDay":   false,
			"startAt":  "2026-04-20T15:00:00+09:00",
			"endAt":    "2026-04-20T17:00:00+09:00",
			"color":    "#2ECC87",
			"location": "会議室B",
			"memo":     "",
		}, &updated)
	require.Equal(t, "定例MTG（更新）", updated.Title)
	require.Equal(t, "#2ECC87", updated.Color)

	// Delete event
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// List should be empty
	var evts2 []struct{ ID string }
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts2)
	require.Len(t, evts2, 0)
}

func TestEventAllDay(t *testing.T) {
	bootstrap(t)
	t.Parallel()


	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID    string `json:"id"`
		AllDay bool   `json:"allDay"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "旅行",
			"allDay":  true,
			"startAt": "2026-04-25T00:00:00+09:00",
			"endAt":   "2026-04-27T00:00:00+09:00",
			"color":   "#FDC02D",
		}, &evt)
	require.True(t, evt.AllDay)
}

func TestEventComments(t *testing.T) {
	bootstrap(t)
	t.Parallel()


	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct{ ID string `json:"id"` }
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "MTG", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
		}, &evt)

	// Create comment
	var comment struct {
		ID       string `json:"id"`
		UserName string `json:"userName"`
		Body     string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", tt.AccessToken,
		map[string]any{"content": "了解です！"}, &comment)
	require.NotEmpty(t, comment.ID)
	require.Equal(t, "了解です！", comment.Body)

	// Create second comment
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", tt.AccessToken,
		map[string]any{"content": "資料を準備します"}, nil)

	// List comments
	var comments []struct {
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/activities", tt.AccessToken, nil, &comments)
	require.Len(t, comments, 2)
	require.Equal(t, "了解です！", comments[0].Body)
	require.Equal(t, "資料を準備します", comments[1].Body)
}

func TestRecurringEventWeekly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create a weekly recurring event on Fridays
	var evt struct {
		ID             string `json:"id"`
		RecurrenceRule *struct {
			Freq     string   `json:"freq"`
			Interval int      `json:"interval"`
			ByDay    []string `json:"byDay"`
		} `json:"recurrenceRule"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
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
	require.NotNil(t, evt.RecurrenceRule)
	assert.Equal(t, "weekly", evt.RecurrenceRule.Freq)

	// List events for April — should have 4 Friday instances (3, 10, 17, 24)
	type eventItem struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		IsRecurrence bool   `json:"isRecurrence"`
	}
	var evts []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts)
	require.Len(t, evts, 4)

	for _, e := range evts {
		assert.Equal(t, "Weekly meeting", e.Title)
		assert.True(t, e.IsRecurrence)
		assert.True(t, strings.Contains(e.ID, "_"), "instance ID should be composite")
	}

	// Get a specific instance by composite ID
	var instance struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evts[1].ID, tt.AccessToken, nil, &instance)
	assert.Equal(t, "Weekly meeting", instance.Title)

	// Delete the recurring event (deletes all instances)
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evts[0].ID, tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// List should be empty
	var evts2 []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts2)
	require.Len(t, evts2, 0)
}

func TestRecurringEventWithCount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create daily event with count=3
	var evt struct{ ID string `json:"id"` }
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Standup",
			"allDay":  false,
			"startAt": "2026-04-01T09:00:00+09:00",
			"endAt":   "2026-04-01T09:15:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "daily",
				"interval": 1,
				"count":    3,
			},
		}, &evt)

	// Should have exactly 3 instances
	type eventItem struct {
		ID string `json:"id"`
	}
	var evts []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts)
	require.Len(t, evts, 3)
}

func TestRecurringEventUpdate(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create weekly recurring event
	var evt struct{ ID string `json:"id"` }
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Team sync",
			"allDay":  false,
			"startAt": "2026-04-06T10:00:00+09:00", // Monday
			"endAt":   "2026-04-06T11:00:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"MO"},
			},
		}, &evt)

	// List to get instance IDs
	type eventItem struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var evts []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts)
	require.True(t, len(evts) >= 3)

	// Update via an instance ID — should update the parent
	var updated struct{ Title string `json:"title"` }
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evts[0].ID, tt.AccessToken,
		map[string]any{
			"title":   "Team sync v2",
			"allDay":  false,
			"startAt": "2026-04-06T10:00:00+09:00",
			"endAt":   "2026-04-06T11:00:00+09:00",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"MO"},
			},
		}, &updated)
	assert.Equal(t, "Team sync v2", updated.Title)

	// Re-list — all instances should have the new title
	var evts2 []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts2)
	for _, e := range evts2 {
		assert.Equal(t, "Team sync v2", e.Title)
	}
}
