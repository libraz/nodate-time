package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func etagHeader(tag string) map[string]string { return map[string]string{"If-Match": tag} }

// An event carries the version of the copy a reader holds in its body and in
// the ETag header, and a write is refused unless it names that version.
func TestEventWritesNameTheVersionTheyReplace(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var created ctEvent
	status, raw, header := helpers.DoJSONFull(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "versioned", "allDay": false,
		"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z"}, nil)
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &created))
	require.NotEmpty(t, created.Etag)
	require.Equal(t, created.Etag, header.Get("ETag"))

	var got ctEvent
	_, raw, header = helpers.DoJSONFull(t, http.MethodGet, calURL+"/events/"+created.ID, owner.AccessToken, nil, nil)
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, created.Etag, got.Etag)
	require.Equal(t, got.Etag, header.Get("ETag"))

	listed := ctListEvents(t, calURL, owner.AccessToken, "2026-09-01", "2026-09-01")
	require.Len(t, listed.Items, 1)
	require.Equal(t, got.Etag, listed.Items[0].Etag, "a listing item carries the tag a write needs")

	url := calURL + "/events/" + created.ID
	title := map[string]any{"title": "renamed"}

	t.Run("no header is refused", func(t *testing.T) {
		status, raw := helpers.DoJSONStatus(t, http.MethodPatch, url, owner.AccessToken, title)
		require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
		require.Equal(t, "REQUEST.PRECONDITION_REQUIRED", adCode(t, raw))
		status, raw = helpers.DoJSONStatus(t, http.MethodDelete, url, owner.AccessToken, nil)
		require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
	})

	t.Run("a value that is not an entity tag is a bad request", func(t *testing.T) {
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, owner.AccessToken, title, etagHeader("not-a-tag"))
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "REQUEST.BAD_IF_MATCH", adCode(t, raw))
	})

	t.Run("a weak tag or a list never matches", func(t *testing.T) {
		for _, header := range []string{"W/" + got.Etag, got.Etag + ", " + got.Etag} {
			status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, owner.AccessToken, title, etagHeader(header))
			require.Equal(t, http.StatusPreconditionFailed, status, header+": "+string(raw))
			require.Equal(t, "EVENT.STALE", adCode(t, raw))
		}
	})

	t.Run("a write answers with the new version", func(t *testing.T) {
		status, raw, header := helpers.DoJSONFull(t, http.MethodPatch, url, owner.AccessToken, title, etagHeader(got.Etag))
		require.Equal(t, http.StatusOK, status, string(raw))
		var updated ctEvent
		require.NoError(t, json.Unmarshal(raw, &updated))
		require.Equal(t, "renamed", updated.Title)
		require.NotEqual(t, got.Etag, updated.Etag)
		require.Equal(t, updated.Etag, header.Get("ETag"))

		// The copy the first writer held is now old.
		status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, owner.AccessToken,
			map[string]any{"title": "from the old copy"}, etagHeader(got.Etag))
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
		require.Equal(t, "EVENT.STALE", adCode(t, raw))
		status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, owner.AccessToken, nil, etagHeader(got.Etag))
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
		helpers.DoJSON(t, http.MethodGet, url, owner.AccessToken, nil, &got)
		require.Equal(t, "renamed", got.Title, "the refused writes changed nothing")
		got.Etag = updated.Etag
	})

	t.Run("a star matches an event that exists", func(t *testing.T) {
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, owner.AccessToken,
			map[string]any{"title": "starred"}, etagHeader("*"))
		require.Equal(t, http.StatusOK, status, string(raw))
	})

	t.Run("a delete names the version too", func(t *testing.T) {
		helpers.DoJSON(t, http.MethodGet, url, owner.AccessToken, nil, &got)
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, owner.AccessToken, nil, etagHeader(got.Etag))
		require.Equal(t, http.StatusNoContent, status, string(raw))
		status, _ = helpers.DoJSONStatus(t, http.MethodGet, url, owner.AccessToken, nil)
		require.Equal(t, http.StatusNotFound, status)
	})
}

// An occurrence of a series is written against the row its id points at: the
// series until it has been edited, its own row afterwards.
func TestOccurrenceTagsFollowTheRowTheirIdPointsAt(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "daily", "allDay": false,
		"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 3}}, &series)

	items := ctListEvents(t, calURL, owner.AccessToken, "2026-09-01", "2026-09-30").Items
	require.Len(t, items, 3)
	var second ctEvent
	for _, e := range items {
		require.Equal(t, series.Etag, e.SeriesEtag, "every occurrence carries the series' tag")
		require.Equal(t, series.Etag, e.Etag, "before an edit the row an id points at is the series")
		if strings.HasPrefix(e.StartAt, "2026-09-02") {
			second = e
		}
	}
	require.NotEmpty(t, second.ID)

	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+second.ID+"?scope=this",
		owner.AccessToken, map[string]any{"title": "moved day"}, etagHeader(second.Etag))
	require.Equal(t, http.StatusOK, status, string(raw))
	var edited ctEvent
	require.NoError(t, json.Unmarshal(raw, &edited))
	require.Equal(t, second.ID, edited.ID)
	require.NotEmpty(t, edited.Etag)
	require.NotEqual(t, second.SeriesEtag, edited.SeriesEtag, "an edit of one occurrence is a new version of the series")

	// The occurrence now has a row of its own, and the copy that named the
	// series is old for it.
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+second.ID+"?scope=this",
		owner.AccessToken, map[string]any{"title": "again"}, etagHeader(second.Etag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Equal(t, "EVENT.STALE", adCode(t, raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+second.ID+"?scope=this",
		owner.AccessToken, map[string]any{"title": "again"}, etagHeader(edited.Etag))
	require.Equal(t, http.StatusOK, status, string(raw))

	// A write to the whole series is measured against the series.
	var master ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, &master)
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+series.ID,
		owner.AccessToken, map[string]any{"title": "whole series"}, etagHeader(series.Etag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+series.ID,
		owner.AccessToken, map[string]any{"title": "whole series"}, etagHeader(master.Etag))
	require.Equal(t, http.StatusOK, status, string(raw))
}

// Removing a whole series is the owner's side to decide; a participant who was
// trusted with the fields may take one occurrence off but not the series.
func TestDeletingAWholeSeriesNeedsTheRightToGrant(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	delegate := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, delegate, "editor")

	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "weekly", "allDay": false, "participantIds": []string{delegate.UserID},
		"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z",
		"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "count": 4}}, &series)
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+series.ID+"/attendees/"+delegate.UserID,
		owner.AccessToken, map[string]any{"canEdit": true}, nil)

	items := ctListEvents(t, calURL, delegate.AccessToken, "2026-09-01", "2026-09-30").Items
	require.Len(t, items, 4)
	var master ctEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, delegate.AccessToken, nil, &master)

	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, calURL+"/events/"+series.ID,
		delegate.AccessToken, nil, etagHeader(master.Etag))
	require.Equal(t, http.StatusForbidden, status, string(raw))
	require.Equal(t, "EVENT.EDIT_DENIED", adCode(t, raw))

	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, calURL+"/events/"+items[1].ID+"?scope=this",
		delegate.AccessToken, nil, etagHeader(items[1].Etag))
	require.Equal(t, http.StatusNoContent, status, string(raw))

	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+series.ID, owner.AccessToken, nil, &master)
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, calURL+"/events/"+series.ID,
		owner.AccessToken, nil, etagHeader(master.Etag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

// Deleting an event records which attachments the deletion retired, so the
// same ones can come back.
func TestDeletedEventLogNamesTheAttachmentsItRetired(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt ctEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "to delete", "allDay": false,
		"startAt": "2026-09-01T10:00:00Z", "endAt": "2026-09-01T11:00:00Z"}, &evt)
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, calURL+"/events/"+evt.ID,
		owner.AccessToken, nil, etagHeader(evt.Etag))
	require.Equal(t, http.StatusNoContent, status, string(raw))

	var ids string
	require.NoError(t, testDB.QueryRowContext(t.Context(), `SELECT CAST(JSON_EXTRACT(payload_json, '$.attachmentIds') AS CHAR)
		FROM events WHERE subject_public_id = UUID_TO_BIN(?) AND type = 'calendar.event.deleted'`, evt.ID).Scan(&ids))
	require.JSONEq(t, `[]`, ids, "the key is present, empty when nothing was attached")
}
