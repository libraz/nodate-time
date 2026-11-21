package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type restoreFixture struct {
	t      *testing.T
	tenant *helpers.TestTenant
	cal    string
}

func newRestoreFixture(t *testing.T) restoreFixture {
	t.Helper()
	bootstrap(t)
	t.Parallel()
	tt := helpers.NewTenant(t, testServerURL)
	return restoreFixture{t: t, tenant: tt, cal: testServerURL + "/calendars/" + tt.CalendarID}
}

func (f restoreFixture) create(title string) string {
	f.t.Helper()
	var evt ctEvent
	helpers.DoJSON(f.t, http.MethodPost, f.cal+"/events", f.tenant.AccessToken, map[string]any{
		"title": title, "allDay": false,
		"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
	}, &evt)
	return evt.ID
}

func (f restoreFixture) del(id string) {
	f.t.Helper()
	var evt ctEvent
	helpers.DoJSON(f.t, http.MethodGet, f.cal+"/events/"+id, f.tenant.AccessToken, nil, &evt)
	status, raw := helpers.DoJSONStatusWithHeaders(f.t, http.MethodDelete, f.cal+"/events/"+id,
		f.tenant.AccessToken, nil, map[string]string{"If-Match": evt.Etag})
	require.Equal(f.t, http.StatusNoContent, status, string(raw))
}

func (f restoreFixture) restore(id string) (int, []byte) {
	f.t.Helper()
	return helpers.DoJSONStatus(f.t, http.MethodPost, f.cal+"/events/"+id+"/restore", f.tenant.AccessToken, nil)
}

func (f restoreFixture) enabled(id string) bool {
	f.t.Helper()
	var on bool
	require.NoError(f.t, testDB.QueryRow(
		"SELECT enabled FROM calendar_events WHERE public_id = UUID_TO_BIN(?)", id).Scan(&on))
	return on
}

// backdateDeletion moves the event's deletion row past the grace period.
func (f restoreFixture) backdateDeletion(id string) {
	f.t.Helper()
	_, err := testDB.Exec(`UPDATE events SET occurred_at = occurred_at - INTERVAL 11 MINUTE
		WHERE subject_public_id = UUID_TO_BIN(?) AND type = 'calendar.event.deleted'`, id)
	require.NoError(f.t, err)
}

func (f restoreFixture) countLog(id, kind string) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, testDB.QueryRow(
		"SELECT COUNT(*) FROM events WHERE subject_public_id = UUID_TO_BIN(?) AND type = ?", id, kind).Scan(&n))
	return n
}

func TestRestoreWithinGraceBringsTheEventBack(t *testing.T) {
	f := newRestoreFixture(t)
	id := f.create("restorable")
	f.del(id)
	require.False(t, f.enabled(id))

	status, raw := f.restore(id)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.True(t, f.enabled(id))
	require.Equal(t, 1, f.countLog(id, "calendar.event.restored"))

	var got ctEvent
	helpers.DoJSON(t, http.MethodGet, f.cal+"/events/"+id, f.tenant.AccessToken, nil, &got)
	require.Equal(t, "restorable", got.Title)

	status, raw = f.restore(id)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "EVENT.NOT_DELETED", adCode(t, raw), "restoring twice")
}

func TestRestoreRefusedPastGrace(t *testing.T) {
	f := newRestoreFixture(t)
	id := f.create("too late")
	f.del(id)
	f.backdateDeletion(id)

	status, raw := f.restore(id)
	require.Equal(t, http.StatusGone, status, string(raw))
	require.Equal(t, "EVENT.RESTORE_EXPIRED", adCode(t, raw))
	require.False(t, f.enabled(id))
}

func TestRestoreOfALiveEventIsNotDeleted(t *testing.T) {
	f := newRestoreFixture(t)
	status, raw := f.restore(f.create("alive"))
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "EVENT.NOT_DELETED", adCode(t, raw))
}

func TestRestoreNamesTheCalendarTheEventIsIn(t *testing.T) {
	f := newRestoreFixture(t)
	id := f.create("elsewhere")
	f.del(id)

	other := helpers.NewTenant(t, testServerURL)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+other.CalendarID+"/events/"+id+"/restore", other.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, string(raw))
	require.False(t, f.enabled(id))

	status, raw = f.restore(uuid.NewString())
	require.Equal(t, http.StatusNotFound, status, string(raw))
}

func TestRestoreRefusedWhenTheUIDWasReused(t *testing.T) {
	f := newRestoreFixture(t)
	row := ctEventRow{Title: "imported", Start: "2026-05-12 09:00:00", End: "2026-05-12 10:00:00",
		SourceUID: "reused-" + uuid.NewString()}
	id, _ := ctInsertEvent(t, f.tenant.CalendarID, f.tenant.UserID, row)
	f.del(id)
	reused, _ := ctInsertEvent(t, f.tenant.CalendarID, f.tenant.UserID, row)

	status, raw := f.restore(id)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "EVENT.RESTORE_CONFLICT", adCode(t, raw))
	require.False(t, f.enabled(id))
	require.True(t, f.enabled(reused))
}

// A deletion another product wrote names no attachments: only the event row
// comes back.
func TestRestoreOfAForeignDeletionBringsBackOnlyTheEvent(t *testing.T) {
	f := newRestoreFixture(t)
	id, internal := ctInsertEvent(t, f.tenant.CalendarID, f.tenant.UserID, ctEventRow{
		Title: "deleted elsewhere", Start: "2026-05-12 09:00:00", End: "2026-05-12 10:00:00"})
	_, err := testDB.Exec("UPDATE calendar_events SET enabled = FALSE WHERE id = ?", internal)
	require.NoError(t, err)
	pub := uuid.New()
	_, err = testDB.Exec(`INSERT INTO events (public_id, workspace_id, calendar_id, subject_public_id, type, payload_json, occurred_at)
		SELECT ?, workspace_id, calendar_id, UUID_TO_BIN(?), 'calendar.event.deleted',
		       JSON_OBJECT('eventId', ?, 'title', title), NOW(3)
		FROM calendar_events WHERE id = ?`, pub[:], id, id, internal)
	require.NoError(t, err)

	status, raw := f.restore(id)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.True(t, f.enabled(id))
}

func TestRestoreRefusedForATaskProjection(t *testing.T) {
	f := newRestoreFixture(t)
	id := f.create("projected")
	projectOntoTask(t, id)

	status, raw := f.restore(id)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "EVENT.TASK_OWNED", adCode(t, raw))
}

func TestRestoreAfterASingleOccurrenceCancellationIsNotDeleted(t *testing.T) {
	f := newRestoreFixture(t)
	var series ctEvent
	helpers.DoJSON(t, http.MethodPost, f.cal+"/events", f.tenant.AccessToken, map[string]any{
		"title": "daily", "allDay": false,
		"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 4},
	}, &series)
	var occ ctEvent
	for _, e := range ctListEvents(t, f.cal, f.tenant.AccessToken, "2026-05-13", "2026-05-13").Items {
		occ = e
	}
	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, f.cal+"/events/"+occ.ID+"?scope=this",
		f.tenant.AccessToken, nil, map[string]string{"If-Match": occ.Etag})
	require.Equal(t, http.StatusNoContent, status, string(raw))

	status, raw = f.restore(series.ID)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "EVENT.NOT_DELETED", adCode(t, raw))
}

// Only the attachments the deletion retired come back.
func TestRestoreBringsBackTheAttachmentsTheDeletionRetired(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
	}
	f := newRestoreFixture(t)
	id, earlier := attachEventWithAttachment(t, f.tenant, "with files", uniqueAttachmentBody("removed first"))

	var pres struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
	body := uniqueAttachmentBody("kept")
	helpers.DoJSON(t, http.MethodPost, f.cal+"/events/"+id+"/attachments/presign", f.tenant.AccessToken,
		map[string]any{"filename": "kept.pdf", "contentType": "application/pdf",
			"byteSize": len(body), "sha256": helpers.SHA256Hex(body)}, &pres)
	if pres.UploadURL != "" {
		helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	}
	helpers.DoJSON(t, http.MethodPost, f.cal+"/events/"+id+"/attachments/"+pres.AttachmentID+"/confirm",
		f.tenant.AccessToken, nil, nil)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		f.cal+"/events/"+id+"/attachments/"+earlier, f.tenant.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	f.del(id)
	status, raw = f.restore(id)
	require.Equal(t, http.StatusOK, status, string(raw))

	var listed []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, f.cal+"/events/"+id+"/attachments", f.tenant.AccessToken, nil, &listed)
	require.Len(t, listed, 1)
	require.Equal(t, pres.AttachmentID, listed[0].ID, "the attachment removed before the delete stays removed")
}
