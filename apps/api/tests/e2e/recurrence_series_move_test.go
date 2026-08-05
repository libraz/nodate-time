package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// seriesBody is the full update payload a series-wide edit needs.
func seriesBody(title, startAt, endAt string) map[string]any {
	return map[string]any{
		"title":              title,
		"allDay":             false,
		"startAt":            startAt,
		"endAt":              endAt,
		"timezone":           "America/New_York",
		"location":           "",
		"memo":               "",
		"url":                "",
		"notificationOffset": nil,
		"participants":       []string{},
		"ownerId":            nil,
		"recurrenceRule": map[string]any{
			"freq":     "weekly",
			"interval": 1,
			"byDay":    []string{"TH"},
		},
	}
}

func startsOf(evts []recInstance) []string {
	out := make([]string, 0, len(evts))
	for _, e := range evts {
		out = append(out, e.StartAt)
	}
	return out
}

// A cancelled occurrence is stored as the instant it would have started, so a
// series-wide move has to carry it along. The move is in calendar units in the
// event's own zone, not in absolute time: a weekly 09:00 New York series that
// starts in winter and moves into summer shifts by a whole number of days but
// by one hour less than that in absolute terms. Carrying the cancellation by
// the absolute difference lands it an hour off every occurrence the rule now
// produces, and the deleted occurrence silently comes back.
func TestMovingASeriesAcrossDSTKeepsItsCancellations(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		seriesBody("Weekly review", "2026-02-19T09:00:00-05:00", "2026-02-19T10:00:00-05:00"), &evt)
	require.NotEmpty(t, evt.ID)

	// Cancel the occurrence on 19 March, which is already on summer time.
	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		calURL+"/events/"+evt.ID+"_20260319?scope=this", tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "cancel occurrence: %s", string(raw))

	var march []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-03-01&end=2026-03-31", tt.AccessToken, nil, &march)
	require.Len(t, march, 3, "March has five Thursdays; one is cancelled and one is April's window")

	// Move the whole series seven weeks later, keeping 09:00 local.
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID+"?scope=all", tt.AccessToken,
		seriesBody("Weekly review", "2026-04-09T09:00:00-04:00", "2026-04-09T10:00:00-04:00"), nil)

	var may []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-04-09&end=2026-05-14", tt.AccessToken, nil, &may)

	starts := startsOf(may)
	require.NotContains(t, starts, "2026-05-07T13:00:00Z",
		"the cancellation must land on the occurrence it moved with, not an hour off it")
	require.Contains(t, starts, "2026-04-30T13:00:00Z", "its neighbours must still be there")
	require.Contains(t, starts, "2026-05-14T13:00:00Z")
	for _, s := range starts {
		require.Equal(t, "13:00:00Z", s[11:],
			"every occurrence still fires at 09:00 local: %s", s)
	}
}

// An edited occurrence is a row keyed to the occurrence it replaces. The same
// unit problem applies: shifted by the absolute difference it names an instant
// the rule no longer produces, so it stops replacing anything and is rendered
// a second time alongside the occurrence it was supposed to stand in for.
func TestMovingASeriesAcrossDSTKeepsItsEditedOccurrenceAttached(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		seriesBody("Weekly review", "2026-02-19T09:00:00-05:00", "2026-02-19T10:00:00-05:00"), &evt)

	// Edit the 19 March occurrence, giving it a title and a later hour.
	var override recInstance
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID+"_20260319?scope=this", tt.AccessToken,
		map[string]any{
			"title":              "Quarterly review",
			"allDay":             false,
			"startAt":            "2026-03-19T14:00:00-04:00",
			"endAt":              "2026-03-19T15:00:00-04:00",
			"timezone":           "America/New_York",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &override)
	require.Equal(t, "Quarterly review", override.Title)

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID+"?scope=all", tt.AccessToken,
		seriesBody("Weekly review", "2026-04-09T09:00:00-04:00", "2026-04-09T10:00:00-04:00"), nil)

	var may []recInstance
	helpers.DoJSON(t, http.MethodGet,
		calURL+"/events?start=2026-04-09&end=2026-05-14", tt.AccessToken, nil, &may)

	edited := 0
	for _, e := range may {
		if e.Title == "Quarterly review" {
			edited++
			require.Equal(t, "2026-05-07T18:00:00Z", e.StartAt,
				"the edited occurrence keeps the 14:00 local hour it was given")
			require.Equal(t, evt.ID+"_20260507", e.ID,
				"and it still names the occurrence it replaces")
		}
	}
	require.Equal(t, 1, edited, "the edited occurrence must appear exactly once")

	for _, e := range may {
		require.NotEqual(t, "2026-05-07T13:00:00Z", e.StartAt,
			"the occurrence it replaces must not be rendered alongside it")
	}
}
