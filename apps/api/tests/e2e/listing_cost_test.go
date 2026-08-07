package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestListingCostDoesNotFollowTheNumberOfEvents pins the shape of the month
// read.
//
// A listing that asks per event stays correct as it degrades, so nothing but a
// count notices it: the calendar renders, just slower every month it is used.
// The SPA fetches every visible calendar in parallel and refetches on each
// month change, so the per-event queries multiply rather than add.
func TestListingCostDoesNotFollowTheNumberOfEvents(t *testing.T) {
	bootstrap(t)

	srv, counter := helpers.NewCountingTestServer(t, testDB)
	owner := helpers.NewTenant(t, srv.BaseURL)
	guest := helpers.NewTenant(t, srv.BaseURL)
	calURL := srv.BaseURL + "/calendars/" + owner.CalendarID

	joinAs2(t, srv.BaseURL, calURL, owner.AccessToken, guest, "editor")

	// Two shapes of event, because they take different paths through the
	// handler: plain rows, and series that get expanded.
	const plain, series = 6, 4
	for i := range plain {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			map[string]any{
				"title":        fmt.Sprintf("単発 %d", i),
				"allDay":       false,
				"startAt":      fmt.Sprintf("2026-10-%02dT10:00:00+09:00", i+1),
				"endAt":        fmt.Sprintf("2026-10-%02dT11:00:00+09:00", i+1),
				"participants": []string{guest.UserID},
			}, nil)
	}
	for i := range series {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			map[string]any{
				"title":          fmt.Sprintf("繰り返し %d", i),
				"allDay":         false,
				"startAt":        fmt.Sprintf("2026-10-%02dT14:00:00+09:00", i+1),
				"endAt":          fmt.Sprintf("2026-10-%02dT15:00:00+09:00", i+1),
				"participants":   []string{guest.UserID},
				"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "count": 4},
			}, nil)
	}

	measure := func() (int, int) {
		t.Helper()
		counter.Reset()
		var listed []struct {
			ID           string   `json:"id"`
			Participants []string `json:"participants"`
		}
		helpers.DoJSON(t, http.MethodGet,
			calURL+"/events?start=2026-10-01&end=2026-10-31&tz=Asia/Tokyo",
			owner.AccessToken, nil, &listed)
		// Every event carries its participants, so an answer that lost them
		// would make the count meaningless.
		for _, e := range listed {
			require.Len(t, e.Participants, 1, "event %s should still report its participant", e.ID)
		}
		return counter.Count(), len(listed)
	}

	before, listedBefore := measure()
	require.Greater(t, listedBefore, plain, "the series should have expanded")

	// Double the calendar's contents. If any read is per event, the query
	// count follows it.
	for i := range plain {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			map[string]any{
				"title":        fmt.Sprintf("追加 %d", i),
				"allDay":       false,
				"startAt":      fmt.Sprintf("2026-10-%02dT18:00:00+09:00", i+1),
				"endAt":        fmt.Sprintf("2026-10-%02dT19:00:00+09:00", i+1),
				"participants": []string{guest.UserID},
			}, nil)
	}
	for i := range series {
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			map[string]any{
				"title":          fmt.Sprintf("追加繰り返し %d", i),
				"allDay":         false,
				"startAt":        fmt.Sprintf("2026-10-%02dT20:00:00+09:00", i+1),
				"endAt":          fmt.Sprintf("2026-10-%02dT21:00:00+09:00", i+1),
				"participants":   []string{guest.UserID},
				"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "count": 4},
			}, nil)
	}

	after, listedAfter := measure()
	require.Greater(t, listedAfter, listedBefore, "the second listing should be larger")
	require.Equal(t, before, after,
		"twice the events should not cost more queries: %d then %d, for %d then %d events",
		before, after, listedBefore, listedAfter)
}

// joinAs2 is joinAs against a server other than the package-wide one.
func joinAs2(t *testing.T, baseURL, calURL, ownerToken string, joiner *helpers.TestTenant, role string) {
	t.Helper()
	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", ownerToken,
		map[string]any{"role": role}, &inv)
	helpers.DoJSON(t, http.MethodPost, baseURL+"/invites/"+inv.Token+"/accept", joiner.AccessToken, nil, nil)
}
