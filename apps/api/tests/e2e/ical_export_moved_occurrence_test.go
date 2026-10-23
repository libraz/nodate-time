package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func instanceTitles(evts []recInstance) []string {
	out := make([]string, 0, len(evts))
	for _, e := range evts {
		out = append(out, e.Title)
	}
	return out
}

// An occurrence dragged to before its series' start is still an occurrence of
// that series. Every windowed reader -- the week and month views, the share
// page and a windowed export -- has to find the series through it, or the
// occurrence looks deleted.
func TestOccurrenceMovedBeforeTheSeriesStartStaysVisible(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, map[string]any{
		"title": "Weekly review", "allDay": false, "timezone": "UTC",
		"startAt": "2026-11-02T10:00:00Z", "endAt": "2026-11-02T11:00:00Z", // Monday
		"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"MO"}},
	}, nil)
	var first []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-11-02&end=2026-11-02&tz=UTC",
		tt.AccessToken, nil, &first)
	require.Len(t, first, 1)

	const moved = "Weekly review brought forward"
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+first[0].ID+"?scope=this", tt.AccessToken,
		eventBody(moved, "2026-10-30T10:00:00Z", "2026-10-30T11:00:00Z"), nil) // the Friday before

	for name, query := range map[string]string{
		"week view":  "start=2026-10-26&end=2026-11-01&tz=UTC",
		"month view": "start=2026-10-01&end=2026-10-31&tz=UTC",
		"day view":   "start=2026-10-30&end=2026-10-30&tz=UTC",
	} {
		var listed []recInstance
		helpers.DoJSON(t, http.MethodGet, calURL+"/events?"+query, tt.AccessToken, nil, &listed)
		require.Equal(t, []string{moved}, instanceTitles(listed), name)
	}

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &invite)
	require.NotEmpty(t, invite.Token)
	var shared []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/share/"+invite.Token+"/events?start=2026-10-01&end=2026-10-31&tz=UTC", "", nil, &shared)
	require.Len(t, shared, 1, "share listing")
	require.Equal(t, moved, shared[0].Title, "share listing")

	for _, format := range []string{"ics", "csv"} {
		body, h := exportResponse(t, calURL, tt.AccessToken,
			"format="+format+"&from=2026-10-30&to=2026-10-30&tz=UTC")
		require.Equal(t, "2026-10-30/2026-10-31", h.Get("X-Export-Window"), format)
		require.Contains(t, body, moved, "%s export of the day the occurrence moved to", format)
	}

	// The occurrence moved off the series' first Monday, which is empty now.
	var monday []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-11-02&end=2026-11-02&tz=UTC",
		tt.AccessToken, nil, &monday)
	require.Empty(t, monday)
}
