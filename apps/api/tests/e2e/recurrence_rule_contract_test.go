package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A rule carrying a field the expander would ignore, or one with no
// occurrence at all, is refused on create and on a series edit rather than
// stored and echoed back while the calendar shows something else.
func TestRulesTheExpanderWouldNotHonourAreRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	occ := createWeeklyFriday(t, calURL, owner.AccessToken)
	seriesURL := calURL + "/events/" + strings.Split(occ[0].ID, "_")[0]

	refused := map[string]map[string]any{
		"daily with byDay":                {"freq": "daily", "interval": 1, "byDay": []string{"MO"}},
		"yearly with byMonthDay":          {"freq": "yearly", "interval": 1, "byMonthDay": 3},
		"weekly with bySetPos":            {"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "bySetPos": 2},
		"monthly bySetPos without byDay":  {"freq": "monthly", "interval": 1, "bySetPos": 2},
		"monthly bySetPos with two days":  {"freq": "monthly", "interval": 1, "byDay": []string{"MO", "FR"}, "bySetPos": 2},
		"monthly bySetPos and byMonthDay": {"freq": "monthly", "interval": 1, "byDay": []string{"FR"}, "bySetPos": 1, "byMonthDay": 3},
		"until the day before the start":  {"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "until": "2026-04-02"},
		"until earlier the same day":      {"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "until": "2026-04-03T14:00:00+09:00"},
	}
	for name, rule := range refused {
		create := map[string]any{
			"title":          "Refused",
			"allDay":         false,
			"startAt":        "2026-04-03T15:00:00+09:00",
			"endAt":          "2026-04-03T16:00:00+09:00",
			"timezone":       "Asia/Tokyo",
			"recurrenceRule": rule,
		}
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken, create)
		require.Equal(t, http.StatusBadRequest, status, "create, %s: %s", name, raw)

		update := eventBody("Weekly meeting", "2026-04-03T15:00:00+09:00", "2026-04-03T16:00:00+09:00")
		update["recurrenceRule"] = rule
		status, raw = helpers.DoJSONStatus(t, http.MethodPut, seriesURL, owner.AccessToken, update)
		require.Equal(t, http.StatusBadRequest, status, "update, %s: %s", name, raw)
	}

	// An until on the start day itself still has its first occurrence.
	accepted := map[string]any{
		"title":          "One occurrence",
		"allDay":         false,
		"startAt":        "2026-04-03T15:00:00+09:00",
		"endAt":          "2026-04-03T16:00:00+09:00",
		"timezone":       "Asia/Tokyo",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "until": "2026-04-03"},
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, accepted, nil)
	var april []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-03&end=2026-04-03&tz=Asia/Tokyo",
		owner.AccessToken, nil, &april)
	titles := map[string]int{}
	for _, e := range april {
		titles[e.Title]++
	}
	require.Equal(t, 1, titles["One occurrence"])
}

// The last occurrence of a zero-length series sits exactly where the series
// ends. A window opening on it selects the series, as it would a one-off
// marker at that instant.
func TestZeroLengthSeriesEndingAtTheWindowStartIsListed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Midnight marker",
			"allDay":         false,
			"startAt":        "2026-05-01T00:00:00+09:00",
			"endAt":          "2026-05-01T00:00:00+09:00",
			"timezone":       "Asia/Tokyo",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 3},
		}, nil)

	var day []recInstance
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-05-03&end=2026-05-03&tz=Asia/Tokyo",
		owner.AccessToken, nil, &day)
	require.Len(t, day, 1, "the third marker opens the day it is on")
	require.Equal(t, "2026-05-02T15:00:00Z", day[0].StartAt)
}
