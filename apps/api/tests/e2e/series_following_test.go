package e2e

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type flRule struct {
	Freq       string   `json:"freq"`
	Interval   int      `json:"interval"`
	ByDay      []string `json:"byDay"`
	ByMonthDay []int    `json:"byMonthDay"`
	Until      *string  `json:"until"`
	Count      int      `json:"count"`
}

type flEvent struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	StartAt        string  `json:"startAt"`
	EndAt          string  `json:"endAt"`
	Etag           string  `json:"etag"`
	SeriesEtag     string  `json:"seriesEtag"`
	OwnerID        string  `json:"ownerId"`
	CreatorID      string  `json:"creatorId"`
	RecurrenceDate *string `json:"recurrenceDate"`
	RecurrenceRule *flRule `json:"recurrenceRule"`
	Attendees      []struct {
		UserID  string `json:"userId"`
		Rsvp    string `json:"rsvp"`
		CanEdit bool   `json:"canEdit"`
	} `json:"attendees"`
}

// key is the occurrence's start as one comparable instant.
func (e flEvent) key(t *testing.T) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339, e.StartAt)
	require.NoError(t, err)
	return at.UTC().Format(time.RFC3339)
}

const flWindow = "start=2026-03-01&end=2026-08-01"

func flList(t *testing.T, calURL, token string) []flEvent {
	t.Helper()
	var out struct {
		Items []flEvent `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?"+flWindow, token, nil, &out)
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].key(t) < out.Items[j].key(t) })
	return out.Items
}

func flKeys(t *testing.T, items []flEvent) []string {
	t.Helper()
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.key(t))
	}
	return keys
}

// flDay is the date key of the n-th weekly occurrence of a series starting on
// Monday 2026-03-02.
func flDay(n int) string {
	return time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*n).Format("20060102")
}

// flAt reads the occurrence of the series named by its position on the rule's
// grid, with the tags current at this moment.
func flAt(t *testing.T, calURL, token string, n int) flEvent {
	t.Helper()
	for _, it := range flList(t, calURL, token) {
		if it.RecurrenceDate != nil && *it.RecurrenceDate == flDay(n) {
			return it
		}
	}
	t.Fatalf("occurrence %d (%s) is not listed", n, flDay(n))
	return flEvent{}
}

// flAtOf is flAt for one series, for a calendar that holds several on the same dates.
func flAtOf(t *testing.T, calURL, token, seriesID string, n int) flEvent {
	t.Helper()
	for _, it := range flList(t, calURL, token) {
		if it.RecurrenceDate != nil && *it.RecurrenceDate == flDay(n) && strings.HasPrefix(it.ID, seriesID+"_") {
			return it
		}
	}
	t.Fatalf("occurrence %d (%s) of %s is not listed", n, flDay(n), seriesID)
	return flEvent{}
}

func flGet(t *testing.T, calURL, token, id string) flEvent {
	t.Helper()
	var out flEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, token, nil, &out)
	return out
}

// flCreate makes a weekly series starting Monday 2026-03-02 10:00 in zone.
func flCreate(t *testing.T, calURL, token, zone string, rule map[string]any, extra map[string]any) flEvent {
	t.Helper()
	offset := "Z"
	if zone == "America/New_York" {
		offset = "-05:00"
	}
	body := map[string]any{
		"title":          "Original",
		"allDay":         false,
		"startAt":        "2026-03-02T10:00:00" + offset,
		"endAt":          "2026-03-02T11:00:00" + offset,
		"timezone":       zone,
		"recurrenceRule": rule,
	}
	for k, v := range extra {
		body[k] = v
	}
	var out flEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, body, &out)
	require.NotEmpty(t, out.ID)
	return out
}

func weekly(extra map[string]any) map[string]any {
	rule := map[string]any{"freq": "weekly", "interval": 1}
	for k, v := range extra {
		rule[k] = v
	}
	return rule
}

func flPatch(t *testing.T, calURL, token string, occ flEvent, scope, ifMatch string, body map[string]any) (int, []byte, flEvent) {
	t.Helper()
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch,
		calURL+"/events/"+occ.ID+"?scope="+scope, token, body, map[string]string{"If-Match": ifMatch})
	var out flEvent
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &out))
	}
	return status, raw, out
}

func flDelete(t *testing.T, calURL, token string, occ flEvent, scope, ifMatch string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		calURL+"/events/"+occ.ID+"?scope="+scope, token, nil, map[string]string{"If-Match": ifMatch})
}

// flOverride changes one occurrence on its own, moving it by shift.
func flOverride(t *testing.T, calURL, token string, n int, title string, shift time.Duration) {
	t.Helper()
	occ := flAt(t, calURL, token, n)
	start, err := time.Parse(time.RFC3339, occ.StartAt)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, occ.EndAt)
	require.NoError(t, err)
	status, raw, _ := flPatch(t, calURL, token, occ, "this", occ.Etag, map[string]any{
		"title":   title,
		"startAt": start.Add(shift).UTC().Format(time.RFC3339),
		"endAt":   end.Add(shift).UTC().Format(time.RFC3339),
	})
	require.Equal(t, http.StatusOK, status, string(raw))
}

func flCancel(t *testing.T, calURL, token string, n int) {
	t.Helper()
	occ := flAt(t, calURL, token, n)
	status, raw := flDelete(t, calURL, token, occ, "this", occ.Etag)
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

func flTitles(items []flEvent) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// Cutting a series at an occurrence leaves the set of occurrences exactly as
// it was, whatever bounds the rule has and wherever the cut falls; only the
// edit shows, from the cut on.
func TestFollowingSplitKeepsTheOccurrenceSet(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	rules := map[string]map[string]any{
		"count":     weekly(map[string]any{"count": 6}),
		"until":     weekly(map[string]any{"until": "2026-04-06T10:00:00Z"}),
		"unbounded": weekly(nil),
	}
	for bound, rule := range rules {
		for _, where := range []string{"first", "middle", "last"} {
			t.Run(bound+"/"+where, func(t *testing.T) {
				owner := helpers.NewTenant(t, testServerURL)
				calURL := testServerURL + "/calendars/" + owner.CalendarID
				series := flCreate(t, calURL, owner.AccessToken, "UTC", rule, nil)

				before := flList(t, calURL, owner.AccessToken)
				n := len(before)
				idx := map[string]int{"first": 0, "middle": n / 2, "last": n - 1}[where]
				occ := before[idx]

				status, raw, got := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag,
					map[string]any{"title": "Renamed"})
				require.Equal(t, http.StatusOK, status, string(raw))
				require.NotEmpty(t, got.Etag)

				after := flList(t, calURL, owner.AccessToken)
				require.Equal(t, flKeys(t, before), flKeys(t, after), "the split changed which occurrences exist")
				for i, it := range after {
					want := "Original"
					if i >= idx {
						want = "Renamed"
					}
					require.Equal(t, want, it.Title, "occurrence %d", i)
				}

				if idx == 0 {
					require.Equal(t, series.ID, got.ID, "a cut at the first occurrence is an edit of the series")
					return
				}
				require.NotEqual(t, series.ID, got.ID)
				head, tail := flGet(t, calURL, owner.AccessToken, series.ID), got.RecurrenceRule
				require.NotNil(t, head.RecurrenceRule)
				require.NotNil(t, tail)
				switch bound {
				case "count":
					require.Equal(t, idx, head.RecurrenceRule.Count)
					require.Equal(t, n-idx, tail.Count)
				case "until":
					require.NotNil(t, head.RecurrenceRule.Until)
					require.Equal(t, before[idx-1].key(t), *head.RecurrenceRule.Until)
					require.NotNil(t, tail.Until)
					require.Equal(t, "2026-04-06T10:00:00Z", *tail.Until)
				default:
					require.NotNil(t, head.RecurrenceRule.Until)
					require.Equal(t, before[idx-1].key(t), *head.RecurrenceRule.Until)
					require.Nil(t, tail.Until)
					require.Zero(t, tail.Count)
				}
			})
		}
	}
}

// A cut that moves the time carries later overrides and cancellations along,
// each still naming the same occurrence on the new grid -- across a DST
// transition included.
func TestFollowingSplitWithATimeShiftKeepsLaterOverrides(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	const zone = "America/New_York"
	ny, err := time.LoadLocation(zone)
	require.NoError(t, err)
	series := flCreate(t, calURL, owner.AccessToken, zone, weekly(map[string]any{"count": 8}), nil)

	flOverride(t, calURL, owner.AccessToken, 3, "moved three", time.Hour)
	flOverride(t, calURL, owner.AccessToken, 5, "moved five", -2*time.Hour)
	flCancel(t, calURL, owner.AccessToken, 6)

	before := flList(t, calURL, owner.AccessToken)
	require.Len(t, before, 7)
	occ := flAt(t, calURL, owner.AccessToken, 2)
	start, err := time.Parse(time.RFC3339, occ.StartAt)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, occ.EndAt)
	require.NoError(t, err)
	shift := func(at time.Time) time.Time { return at.In(ny).AddDate(0, 0, 1).Add(30 * time.Minute) }

	status, raw, got := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{
		"startAt": shift(start).UTC().Format(time.RFC3339),
		"endAt":   shift(end).UTC().Format(time.RFC3339),
	})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NotEqual(t, series.ID, got.ID)

	after := flList(t, calURL, owner.AccessToken)
	want := make([]string, 0, len(before))
	for i, it := range before {
		at, err := time.Parse(time.RFC3339, it.StartAt)
		require.NoError(t, err)
		if i >= 2 {
			at = shift(at)
		}
		want = append(want, at.UTC().Format(time.RFC3339))
	}
	require.Equal(t, want, flKeys(t, after))
	require.Equal(t, flTitles(before), flTitles(after), "an override stays on the occurrence it replaced")
}

// A cut that changes the rule starts the new series from a clean grid: the
// cancellations and overrides after the cut no longer name anything.
func TestFollowingSplitWithARuleChangeDropsLaterExceptions(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), nil)

	flCancel(t, calURL, owner.AccessToken, 5)
	flOverride(t, calURL, owner.AccessToken, 7, "override seven", time.Hour)
	flCancel(t, calURL, owner.AccessToken, 1)

	before := flList(t, calURL, owner.AccessToken)
	occ := flAt(t, calURL, owner.AccessToken, 3)
	status, raw, _ := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{
		"recurrenceRule": weekly(map[string]any{"interval": 2}),
	})
	require.Equal(t, http.StatusOK, status, string(raw))

	after := flList(t, calURL, owner.AccessToken)
	cut := occ.key(t)
	var kept []string
	for _, k := range flKeys(t, before) {
		if k < cut {
			kept = append(kept, k)
		}
	}
	var got []string
	for _, k := range flKeys(t, after) {
		if k < cut {
			got = append(got, k)
		}
	}
	require.Equal(t, kept, got, "the part before the cut, its cancellation included, is as it was")

	start, err := time.Parse(time.RFC3339, occ.StartAt)
	require.NoError(t, err)
	for _, it := range after {
		require.NotEqual(t, "override seven", it.Title)
		at, err := time.Parse(time.RFC3339, it.StartAt)
		require.NoError(t, err)
		if at.Before(start) {
			continue
		}
		days := int(at.Sub(start).Hours() / 24)
		require.Zero(t, days%14, "%s is not on the fortnightly grid", it.StartAt)
	}
	fifth := flAt(t, calURL, owner.AccessToken, 5)
	require.Equal(t, "Original", fifth.Title, "the cancelled fifth is back on the new grid")
}

// A count is measured in what the rule generates, so occurrences already
// cancelled still take their place in it.
func TestFollowingSplitCountsGeneratedOccurrences(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(map[string]any{"count": 6}), nil)

	flCancel(t, calURL, owner.AccessToken, 1)
	before := flList(t, calURL, owner.AccessToken)
	require.Len(t, before, 5)

	occ := flAt(t, calURL, owner.AccessToken, 3)
	status, raw, got := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{"title": "Renamed"})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.Equal(t, 3, flGet(t, calURL, owner.AccessToken, series.ID).RecurrenceRule.Count)
	require.Equal(t, 3, got.RecurrenceRule.Count)
	require.Equal(t, flKeys(t, before), flKeys(t, flList(t, calURL, owner.AccessToken)))
}

// With every earlier occurrence cancelled, the cut still leaves a series of
// them behind; the new one holds exactly what remains.
func TestFollowingSplitWhenEveryEarlierOccurrenceIsCancelled(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(map[string]any{"count": 5}), nil)

	flCancel(t, calURL, owner.AccessToken, 0)
	flCancel(t, calURL, owner.AccessToken, 1)
	before := flList(t, calURL, owner.AccessToken)
	require.Len(t, before, 3)

	occ := before[0]
	status, raw, got := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{"title": "Renamed"})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NotEqual(t, series.ID, got.ID)
	require.Equal(t, 3, got.RecurrenceRule.Count)

	after := flList(t, calURL, owner.AccessToken)
	require.Equal(t, flKeys(t, before), flKeys(t, after))
	require.Equal(t, []string{"Renamed", "Renamed", "Renamed"}, flTitles(after))
}

// Deleting from an occurrence on ends the series before it and takes the
// overrides and cancellations after it along.
func TestFollowingDeleteEndsTheSeriesBeforeTheOccurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	t.Run("middle", func(t *testing.T) {
		series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(map[string]any{"count": 8}), nil)
		flOverride(t, calURL, owner.AccessToken, 1, "kept override", time.Hour)
		flOverride(t, calURL, owner.AccessToken, 5, "dropped override", time.Hour)
		flCancel(t, calURL, owner.AccessToken, 6)
		before := flList(t, calURL, owner.AccessToken)

		occ := flAt(t, calURL, owner.AccessToken, 3)
		status, raw := flDelete(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag)
		require.Equal(t, http.StatusNoContent, status, string(raw))

		after := flList(t, calURL, owner.AccessToken)
		require.Equal(t, flKeys(t, before)[:3], flKeys(t, after))
		require.Contains(t, flTitles(after), "kept override")
		require.NotContains(t, flTitles(after), "dropped override")
		require.Equal(t, 3, flGet(t, calURL, owner.AccessToken, series.ID).RecurrenceRule.Count)
	})

	t.Run("first occurrence removes the series", func(t *testing.T) {
		series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), nil)
		occ := flAtOf(t, calURL, owner.AccessToken, series.ID, 0)
		status, raw := flDelete(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag)
		require.Equal(t, http.StatusNoContent, status, string(raw))
		status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("unbounded series ends on the day before", func(t *testing.T) {
		series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), nil)
		occ := flAtOf(t, calURL, owner.AccessToken, series.ID, 2)
		status, raw := flDelete(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag)
		require.Equal(t, http.StatusNoContent, status, string(raw))
		rule := flGet(t, calURL, owner.AccessToken, series.ID).RecurrenceRule
		require.NotNil(t, rule.Until)
		require.Equal(t, "2026-03-09T10:00:00Z", *rule.Until)
	})
}

// The scope is refused where it means nothing, and where the caller's copy of
// the series is out of date or their rights stop short of it.
func TestFollowingScopeEdges(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	colleague := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	helpers.DoJSON(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": colleague.UserID, "role": "editor"}, nil)

	t.Run("a plain event does not repeat", func(t *testing.T) {
		var plain flEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			eventBody("Once", "2026-03-02T10:00:00Z", "2026-03-02T11:00:00Z"), &plain)
		status, raw, _ := flPatch(t, calURL, owner.AccessToken, plain, "following", plain.Etag, map[string]any{"title": "x"})
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "EVENT.NOT_RECURRING", adCode(t, raw))
		status, raw = flDelete(t, calURL, owner.AccessToken, plain, "following", plain.Etag)
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "EVENT.NOT_RECURRING", adCode(t, raw))
	})

	t.Run("a task projection is refused", func(t *testing.T) {
		var plain flEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
			eventBody("Task block", "2026-03-02T10:00:00Z", "2026-03-02T11:00:00Z"), &plain)
		projectOntoTask(t, plain.ID)
		plain = flGet(t, calURL, owner.AccessToken, plain.ID)
		status, raw, _ := flPatch(t, calURL, owner.AccessToken, plain, "following", plain.Etag, map[string]any{"title": "x"})
		require.Equal(t, http.StatusConflict, status, string(raw))
		require.Equal(t, "EVENT.TASK_OWNED", adCode(t, raw))
		status, raw = flDelete(t, calURL, owner.AccessToken, plain, "following", plain.Etag)
		require.Equal(t, http.StatusConflict, status, string(raw))
		require.Equal(t, "EVENT.TASK_OWNED", adCode(t, raw))
	})

	t.Run("an unknown scope is a validation error", func(t *testing.T) {
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), nil)
		occ := flAt(t, calURL, owner.AccessToken, 1)
		status, raw, _ := flPatch(t, calURL, owner.AccessToken, occ, "later", occ.SeriesEtag, map[string]any{"title": "x"})
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		status, raw = flDelete(t, calURL, owner.AccessToken, occ, "later", occ.SeriesEtag)
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	})

	t.Run("an occurrence's own tag is not the series'", func(t *testing.T) {
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), nil)
		flOverride(t, calURL, owner.AccessToken, 2, "edited", time.Hour)
		flOverride(t, calURL, owner.AccessToken, 4, "later edit", time.Hour)
		edited := flAt(t, calURL, owner.AccessToken, 2)
		require.NotEqual(t, edited.Etag, edited.SeriesEtag)
		status, raw, _ := flPatch(t, calURL, owner.AccessToken, edited, "following", edited.Etag, map[string]any{"title": "x"})
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
		require.Equal(t, "EVENT.STALE", adCode(t, raw))
		status, raw = flDelete(t, calURL, owner.AccessToken, edited, "following", edited.Etag)
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	})

	t.Run("an edit grant is not enough", func(t *testing.T) {
		series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), map[string]any{"participantIds": []string{colleague.UserID}})
		helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series.ID+"/attendees/"+colleague.UserID,
			owner.AccessToken, map[string]any{"canEdit": true}, nil)
		occ := flAtOf(t, calURL, colleague.AccessToken, series.ID, 2)
		status, raw, _ := flPatch(t, calURL, colleague.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{"title": "x"})
		require.Equal(t, http.StatusForbidden, status, string(raw))
		require.Equal(t, "EVENT.EDIT_DENIED", adCode(t, raw))
		status, raw = flDelete(t, calURL, colleague.AccessToken, occ, "following", occ.SeriesEtag)
		require.Equal(t, http.StatusForbidden, status, string(raw))
		require.Equal(t, "EVENT.EDIT_DENIED", adCode(t, raw))
	})
}

// The part from the cut on is a series in its own right, owned and created by
// the same people and carrying the same participants and checklist.
func TestFollowingSplitCarriesPeopleAndChecklist(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	colleague := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	helpers.DoJSON(t, http.MethodPost, calURL+"/members", owner.AccessToken,
		map[string]any{"userId": colleague.UserID, "role": "editor"}, nil)

	series := flCreate(t, calURL, owner.AccessToken, "UTC", weekly(nil), map[string]any{
		"ownerId":        colleague.UserID,
		"participantIds": []string{colleague.UserID},
	})
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series.ID+"/rsvp", colleague.AccessToken,
		map[string]any{"rsvp": "accepted"}, nil)
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series.ID+"/attendees/"+colleague.UserID,
		owner.AccessToken, map[string]any{"canEdit": true}, nil)
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+series.ID+"/checklist", owner.AccessToken,
		map[string]any{"title": "bring the deck"}, nil)

	occ := flAt(t, calURL, owner.AccessToken, 2)
	status, raw, got := flPatch(t, calURL, owner.AccessToken, occ, "following", occ.SeriesEtag, map[string]any{"title": "Renamed"})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NotEqual(t, series.ID, got.ID)

	require.Equal(t, colleague.UserID, got.OwnerID)
	require.Equal(t, owner.UserID, got.CreatorID)
	require.Len(t, got.Attendees, 1)
	require.Equal(t, colleague.UserID, got.Attendees[0].UserID)
	require.Equal(t, "accepted", got.Attendees[0].Rsvp)
	require.True(t, got.Attendees[0].CanEdit)

	for _, id := range []string{series.ID, got.ID} {
		var items []struct {
			Title string `json:"title"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id+"/checklist", owner.AccessToken, nil, &items)
		require.Len(t, items, 1, id)
		require.Equal(t, "bring the deck", items[0].Title)
	}
	head := flGet(t, calURL, owner.AccessToken, series.ID)
	require.Equal(t, colleague.UserID, head.OwnerID)
	require.Len(t, head.Attendees, 1)
}
