package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Both export formats read every series' changed occurrences together, so the
// number of queries an export makes does not follow the number of series.
func TestExportCostDoesNotFollowTheNumberOfSeries(t *testing.T) {
	bootstrap(t)

	srv, counter := helpers.NewCountingTestServer(t, testDB)
	owner := helpers.NewTenant(t, srv.BaseURL)
	calURL := srv.BaseURL + "/calendars/" + owner.CalendarID

	addSeries := func(n, offset int) {
		for i := range n {
			day := offset + i + 1
			title := fmt.Sprintf("Series %d", day)
			var series struct {
				ID string `json:"id"`
			}
			helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
				"title": title, "allDay": false,
				"startAt":        fmt.Sprintf("2026-10-%02dT10:00:00Z", day),
				"endAt":          fmt.Sprintf("2026-10-%02dT11:00:00Z", day),
				"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 3},
			}, &series)
			var instances []recInstance
			helpers.DoJSON(t, http.MethodGet,
				calURL+fmt.Sprintf("/events?start=2026-10-%02d&end=2026-10-%02d&tz=UTC", day, day),
				owner.AccessToken, nil, &instances)
			var first string
			for _, inst := range instances {
				if inst.Title == title {
					first = inst.ID
				}
			}
			require.NotEmpty(t, first)
			helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+first+"?scope=this", owner.AccessToken,
				map[string]any{
					"title": title + " moved", "allDay": false,
					"startAt":  fmt.Sprintf("2026-10-%02dT15:00:00Z", day),
					"endAt":    fmt.Sprintf("2026-10-%02dT16:00:00Z", day),
					"location": "", "memo": "", "url": "", "notificationOffset": nil,
					"participants": []string{}, "ownerId": nil, "recurrenceRule": nil,
				}, nil)
		}
	}

	measure := func(format string, overrides int) int {
		t.Helper()
		counter.Reset()
		body, _ := exportResponse(t, calURL, owner.AccessToken,
			"format="+format+"&from=2026-10-01&to=2026-10-31&tz=UTC")
		n := counter.Count()
		require.Equal(t, overrides, strings.Count(body, " moved"), "%s export lost a changed occurrence", format)
		return n
	}

	addSeries(2, 0)
	icsBefore, csvBefore := measure("ics", 2), measure("csv", 2)
	addSeries(4, 2)
	require.Equal(t, icsBefore, measure("ics", 6), "iCalendar export queries follow the series count")
	require.Equal(t, csvBefore, measure("csv", 6), "CSV export queries follow the series count")
}

// The CSV export stops at its row cap and says so, rather than handing over a
// file that silently ends early.
func TestCSVExportReportsTruncation(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	small, h := exportResponse(t, calURL, tt.AccessToken, "format=csv")
	require.Empty(t, h.Get("X-Result-Truncated"))
	require.Equal(t, 1, strings.Count(small, "\r\n"), "header row only")

	// Four open-ended daily series over the default fifteen-year window are
	// over 21000 occurrences.
	for i := range 4 {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, map[string]any{
			"title": fmt.Sprintf("Daily %d", i), "allDay": false,
			"startAt":        "2020-01-01T09:00:00Z",
			"endAt":          "2020-01-01T09:30:00Z",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1},
		}, nil)
	}
	csv, h := exportResponse(t, calURL, tt.AccessToken, "format=csv")
	require.Equal(t, "true", h.Get("X-Result-Truncated"))
	require.Equal(t, 20000+1, strings.Count(csv, "\r\n"))

	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/export?format=csv&from=1990-01-01&to=2030-01-01", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, "a window wider than the expander covers is refused")
}
