package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestASecondSaveFromAStaleCopyIsRefused covers the shared-calendar case the
// full-replace contract makes dangerous: an update carries every field, so a
// save from a copy read before someone else's save does not merge with it, it
// erases it -- and neither writer is told.
func TestASecondSaveFromAStaleCopyIsRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	partner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, partner, "editor")
	require.Equal(t, 200, promote(t, calURL, owner.AccessToken, partner, "manager"))

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "打ち合わせ",
			"allDay":  false,
			"startAt": "2026-09-10T10:00:00+09:00",
			"endAt":   "2026-09-10T11:00:00+09:00",
		}, &evt)
	eventURL := calURL + "/events/" + evt.ID

	// Both open the same event and hold the revision they read.
	_, _, ownerHeaders := helpers.DoJSONFull(t, http.MethodGet, eventURL, owner.AccessToken, nil, nil)
	_, _, partnerHeaders := helpers.DoJSONFull(t, http.MethodGet, eventURL, partner.AccessToken, nil, nil)
	openedAt := ownerHeaders.Get("ETag")
	require.NotEmpty(t, openedAt, "a read should say which revision it handed over")
	require.Equal(t, openedAt, partnerHeaders.Get("ETag"))

	// The first save goes through and produces a new revision.
	firstStatus, _, firstHeaders := helpers.DoJSONFull(t, http.MethodPut, eventURL, owner.AccessToken,
		eventBody("会場を変更", "2026-09-10T13:00:00+09:00", "2026-09-10T14:00:00+09:00"),
		map[string]string{"If-Match": openedAt})
	require.Equal(t, 200, firstStatus)
	require.NotEqual(t, openedAt, firstHeaders.Get("ETag"), "a save should hand back the revision it produced")

	// The second writer, still holding the copy from before, is refused rather
	// than silently replacing what the first one stored.
	refused, body := helpers.DoJSONStatusWithHeaders(t, http.MethodPut, eventURL, partner.AccessToken,
		eventBody("時間を変更", "2026-09-10T16:00:00+09:00", "2026-09-10T17:00:00+09:00"),
		map[string]string{"If-Match": openedAt})
	require.Equal(t, 409, refused, "a stale copy must not be applied: %s", string(body))
	require.Contains(t, string(body), "EVENT.STALE")

	// Re-reading gives a tag that works.
	_, _, fresh := helpers.DoJSONFull(t, http.MethodGet, eventURL, partner.AccessToken, nil, nil)
	accepted, retry := helpers.DoJSONStatusWithHeaders(t, http.MethodPut, eventURL, partner.AccessToken,
		eventBody("時間を変更", "2026-09-10T16:00:00+09:00", "2026-09-10T17:00:00+09:00"),
		map[string]string{"If-Match": fresh.Get("ETag")})
	require.Equal(t, 200, accepted, "a fresh copy should save: %s", string(retry))
}

// TestAnUnconditionalSaveStillWorks pins that the precondition is opt-in. An
// integration written before entity tags existed sends no If-Match, and
// refusing it would break a working caller in the name of protecting it.
func TestAnUnconditionalSaveStillWorks(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "健診",
			"allDay":  false,
			"startAt": "2026-09-11T10:00:00+09:00",
			"endAt":   "2026-09-11T11:00:00+09:00",
		}, &evt)

	status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken,
		eventBody("健診 (再)", "2026-09-11T14:00:00+09:00", "2026-09-11T15:00:00+09:00"))
	require.Equal(t, 200, status)
}

// TestChangingOneOccurrenceMovesTheSeriesRevision covers the case a per-row
// entity tag would miss. A single-occurrence edit writes a separate row, so
// the series row it hangs off is otherwise untouched and would keep reporting
// the revision the other writer is holding.
func TestChangingOneOccurrenceMovesTheSeriesRevision(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var series struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "朝会",
			"allDay":         false,
			"startAt":        "2026-09-14T09:00:00+09:00",
			"endAt":          "2026-09-14T09:30:00+09:00",
			"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 5},
		}, &series)

	_, _, opened := helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, nil)
	before := opened.Get("ETag")
	require.NotEmpty(t, before)

	// Move one occurrence only.
	occurrenceURL := calURL + "/events/" + series.ID + "_20260915?scope=this"
	moved, body, afterHeaders := helpers.DoJSONFull(t, http.MethodPut, occurrenceURL, owner.AccessToken,
		eventBody("朝会 (時間変更)", "2026-09-15T10:00:00+09:00", "2026-09-15T10:30:00+09:00"), nil)
	require.Equal(t, 200, moved, "move one occurrence: %s", string(body))
	require.NotEqual(t, before, afterHeaders.Get("ETag"),
		"a changed occurrence has to move the revision a caller holds for the series")

	// Someone still holding the copy from before is now refused.
	stale, staleBody := helpers.DoJSONStatusWithHeaders(t, http.MethodPut, calURL+"/events/"+series.ID,
		owner.AccessToken,
		eventBody("朝会 (全体変更)", "2026-09-14T11:00:00+09:00", "2026-09-14T11:30:00+09:00"),
		map[string]string{"If-Match": before})
	require.Equal(t, 409, stale, "%s", string(staleBody))
}
