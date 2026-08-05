package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A finite series is read from range queries through its end boundary, and a
// moved occurrence is a row hanging off the master rather than one the rule
// produces. Dragging the last occurrence past that boundary therefore hides
// it from every view, export and share while the row is still there — from
// the user's side it is indistinguishable from having deleted it.
func TestMovingTheLastOccurrencePastTheSeriesEndKeepsItVisible(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "Five-day standup",
			"allDay":   false,
			"startAt":  "2026-06-01T10:00:00+09:00",
			"endAt":    "2026-06-01T10:30:00+09:00",
			"timezone": "Asia/Tokyo",
			"recurrenceRule": map[string]any{
				"freq":     "daily",
				"interval": 1,
				"count":    5,
			},
		}, &evt)
	require.NotEmpty(t, evt.ID)

	var june []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-06-01&end=2026-06-30", tt.AccessToken, nil, &june)
	require.Len(t, june, 5)

	// Drag the last occurrence six weeks out, well past the series end.
	var moved recInstance
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID+"_20260605?scope=this", tt.AccessToken,
		map[string]any{
			"title":              "Five-day standup",
			"allDay":             false,
			"startAt":            "2026-07-15T10:00:00+09:00",
			"endAt":              "2026-07-15T10:30:00+09:00",
			"timezone":           "Asia/Tokyo",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &moved)
	require.Equal(t, "2026-07-15T01:00:00Z", moved.StartAt)

	var july []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-07-01&end=2026-07-31", tt.AccessToken, nil, &july)
	require.Len(t, july, 1, "the moved occurrence must show up where it was moved to")
	require.Equal(t, moved.ID, july[0].ID)
	require.Equal(t, "2026-07-15T01:00:00Z", july[0].StartAt)

	// And it must have left the day it came from.
	var stillJune []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-06-01&end=2026-06-30", tt.AccessToken, nil, &stillJune)
	require.Len(t, stillJune, 4)
}
