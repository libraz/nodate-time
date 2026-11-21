package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type detailsWorld struct {
	tt      *helpers.TestTenant
	calURL  string
	eventID string
	base    string
}

func newDetailsWorld(t *testing.T) *detailsWorld {
	t.Helper()
	tt := helpers.NewTenant(t, testServerURL)
	w := &detailsWorld{tt: tt, calURL: testServerURL + "/calendars/" + tt.CalendarID}
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, w.calURL+"/events", tt.AccessToken, map[string]any{
		"title": "Details", "allDay": false,
		"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
	}, &evt)
	w.eventID = evt.ID
	w.base = w.calURL + "/events/" + evt.ID
	return w
}

func detailsCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(raw, &e), string(raw))
	return e.Code
}

func ifMatch(tag string) map[string]string { return map[string]string{"If-Match": tag} }

// tick waits out the millisecond a revision tag is built from.
func tick() { time.Sleep(5 * time.Millisecond) }

type detailItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	ETag  string `json:"etag"`
}

func TestChecklistItemIsPatchedAgainstItsETag(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	w := newDetailsWorld(t)
	tok := w.tt.AccessToken

	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPost, w.base+"/checklist", tok,
		map[string]any{"title": "Tent"}, nil)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var item detailItem
	require.NoError(t, json.Unmarshal(raw, &item))
	require.NotEmpty(t, item.ETag)
	require.Equal(t, item.ETag, hdr.Get("ETag"))
	url := w.base + "/checklist/" + item.ID

	t.Run("method", func(t *testing.T) {
		status, _ := helpers.DoJSONStatusWithHeaders(t, http.MethodPut, url, tok,
			map[string]any{"done": true}, ifMatch(item.ETag))
		require.Equal(t, http.StatusMethodNotAllowed, status)
	})
	t.Run("missing and malformed header", func(t *testing.T) {
		status, raw := helpers.DoJSONStatus(t, http.MethodPatch, url, tok, map[string]any{"done": true})
		require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
		require.Equal(t, "REQUEST.PRECONDITION_REQUIRED", detailsCode(t, raw))

		status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, tok,
			map[string]any{"done": true}, ifMatch("abc"))
		require.Equal(t, http.StatusBadRequest, status, string(raw))
		require.Equal(t, "REQUEST.BAD_IF_MATCH", detailsCode(t, raw))
	})
	t.Run("null is refused", func(t *testing.T) {
		status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, tok,
			map[string]any{"title": nil}, ifMatch(item.ETag))
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		require.Equal(t, "REQUEST.VALIDATION", detailsCode(t, raw))
	})

	tick()
	status, raw, hdr = helpers.DoJSONFull(t, http.MethodPatch, url, tok,
		map[string]any{"done": true}, ifMatch(item.ETag))
	require.Equal(t, http.StatusOK, status, string(raw))
	var ticked detailItem
	require.NoError(t, json.Unmarshal(raw, &ticked))
	require.True(t, ticked.Done)
	require.Equal(t, "Tent", ticked.Title)
	require.NotEqual(t, item.ETag, ticked.ETag)
	require.Equal(t, ticked.ETag, hdr.Get("ETag"))

	// The tag it was changed from now names a revision that is gone.
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, tok,
		map[string]any{"title": "Tents"}, ifMatch(item.ETag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Equal(t, "CHECKLIST.STALE", detailsCode(t, raw))

	var listed []detailItem
	helpers.DoJSON(t, http.MethodGet, w.base+"/checklist", tok, nil, &listed)
	require.Len(t, listed, 1)
	require.Equal(t, ticked.ETag, listed[0].ETag, "the list carries the tag a write needs")

	status, raw = helpers.DoJSONStatus(t, http.MethodDelete, url, tok, nil)
	require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, tok, nil, ifMatch(item.ETag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, tok, nil, ifMatch(ticked.ETag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

func TestCommentIsPatchedAgainstItsETagAndListedByCursor(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	w := newDetailsWorld(t)
	tok := w.tt.AccessToken

	var ids []string
	var first detailComment
	for _, text := range []string{"one", "two", "three"} {
		status, raw, hdr := helpers.DoJSONFull(t, http.MethodPost, w.base+"/activities", tok,
			map[string]any{"body": text}, nil)
		require.Equal(t, http.StatusCreated, status, string(raw))
		var c detailComment
		require.NoError(t, json.Unmarshal(raw, &c))
		require.NotEmpty(t, c.ETag)
		require.Equal(t, c.ETag, hdr.Get("ETag"))
		if text == "one" {
			first = c
		}
		ids = append(ids, c.ID)
		tick()
	}

	var page struct {
		Items      []detailComment `json:"items"`
		NextCursor string          `json:"nextCursor"`
	}
	helpers.DoJSON(t, http.MethodGet, w.base+"/activities?limit=2", tok, nil, &page)
	require.Len(t, page.Items, 2)
	require.Equal(t, []string{ids[1], ids[2]}, []string{page.Items[0].ID, page.Items[1].ID}, "newest page, oldest first")
	require.NotEmpty(t, page.NextCursor)
	require.NotEmpty(t, page.Items[0].ETag)

	var older struct {
		Items      []detailComment `json:"items"`
		NextCursor string          `json:"nextCursor"`
	}
	helpers.DoJSON(t, http.MethodGet, w.base+"/activities?limit=2&cursor="+page.NextCursor, tok, nil, &older)
	require.Len(t, older.Items, 1)
	require.Equal(t, ids[0], older.Items[0].ID)
	require.Empty(t, older.NextCursor)

	status, raw := helpers.DoJSONStatus(t, http.MethodGet, w.base+"/activities?limit=0", tok, nil)
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "REQUEST.VALIDATION", detailsCode(t, raw))

	url := w.base + "/activities/" + first.ID
	status, raw = helpers.DoJSONStatus(t, http.MethodPatch, url, tok, map[string]any{"body": "uno"})
	require.Equal(t, http.StatusPreconditionRequired, status, string(raw))

	tick()
	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPatch, url, tok,
		map[string]any{"body": "uno"}, ifMatch(first.ETag))
	require.Equal(t, http.StatusOK, status, string(raw))
	var edited detailComment
	require.NoError(t, json.Unmarshal(raw, &edited))
	require.Equal(t, "uno", edited.Body)
	require.NotEqual(t, first.ETag, edited.ETag)
	require.Equal(t, edited.ETag, hdr.Get("ETag"))

	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, tok,
		map[string]any{"body": "eins"}, ifMatch(first.ETag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Equal(t, "COMMENT.STALE", detailsCode(t, raw))

	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, tok, nil, ifMatch(first.ETag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, url, tok, nil, ifMatch(edited.ETag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

type detailComment struct {
	ID   string `json:"id"`
	Body string `json:"body"`
	ETag string `json:"etag"`
}

func TestChecklistStopsAtItsLimit(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	w := newDetailsWorld(t)
	tok := w.tt.AccessToken

	_, err := testDB.ExecContext(t.Context(), `
		INSERT INTO calendar_event_checklist_items
		  (public_id, workspace_id, event_id, created_by_user_id, title, done, sort_weight)
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 500)
		SELECT UUID_TO_BIN(UUID()), e.workspace_id, e.id, e.created_by_user_id, CONCAT('item ', n.i), FALSE, n.i
		FROM n JOIN calendar_events e ON e.public_id = UUID_TO_BIN(?)`, w.eventID)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, w.base+"/checklist", tok, map[string]any{"title": "one more"})
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "CHECKLIST.LIMIT_REACHED", detailsCode(t, raw))

	var listed []detailItem
	helpers.DoJSON(t, http.MethodGet, w.base+"/checklist", tok, nil, &listed)
	require.Len(t, listed, 500)

	// Room made by a delete is room again.
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, w.base+"/checklist/"+listed[0].ID, tok, nil,
		ifMatch(listed[0].ETag))
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = helpers.DoJSONStatus(t, http.MethodPost, w.base+"/checklist", tok, map[string]any{"title": "one more"})
	require.Equal(t, http.StatusCreated, status, string(raw))
}

func TestAttachmentsStopAtTheirLimit(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()
	w := newDetailsWorld(t)
	tok := w.tt.AccessToken

	body := uniqueAttachmentBody("limit")
	attachmentID := detailsAttach(t, w, tok, body)

	// Fill the event to its limit with rows sharing the one object.
	_, err := testDB.ExecContext(t.Context(), `
		INSERT INTO calendar_event_attachments
		  (public_id, workspace_id, event_id, uploader_id, storage_object_id, filename, enabled)
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 99)
		SELECT UUID_TO_BIN(UUID()), a.workspace_id, a.event_id, a.uploader_id, a.storage_object_id, CONCAT('copy ', n.i), TRUE
		FROM n JOIN calendar_event_attachments a ON a.public_id = UUID_TO_BIN(?)`, attachmentID)
	require.NoError(t, err)
	_, err = testDB.ExecContext(t.Context(), `
		UPDATE storage_objects so JOIN calendar_event_attachments a ON a.storage_object_id = so.id
		SET so.ref_count = so.ref_count + 99 WHERE a.public_id = UUID_TO_BIN(?)`, attachmentID)
	require.NoError(t, err)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, w.base+"/attachments/presign", tok, map[string]any{
		"filename": "more.pdf", "contentType": "application/pdf",
		"byteSize": len(body), "sha256": helpers.SHA256Hex(body),
	})
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Equal(t, "ATTACHMENT.LIMIT_REACHED", detailsCode(t, raw))

	var listed []json.RawMessage
	helpers.DoJSON(t, http.MethodGet, w.base+"/attachments", tok, nil, &listed)
	require.Len(t, listed, 100)
}

func detailsAttach(t *testing.T, w *detailsWorld, tok string, body []byte) string {
	t.Helper()
	var pres struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
	helpers.DoJSON(t, http.MethodPost, w.base+"/attachments/presign", tok, map[string]any{
		"filename": "a.pdf", "contentType": "application/pdf",
		"byteSize": len(body), "sha256": helpers.SHA256Hex(body),
	}, &pres)
	if pres.UploadURL != "" {
		helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	}
	helpers.DoJSON(t, http.MethodPost, w.base+"/attachments/"+pres.AttachmentID+"/confirm", tok, nil, nil)
	return pres.AttachmentID
}

func TestAttachmentDigestIsHeldToTheDeclaration(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	w := newDetailsWorld(t)
	tok := w.tt.AccessToken

	t.Run("a digest that is not lowercase hex is a validation failure", func(t *testing.T) {
		body := uniqueAttachmentBody("digest-case")
		for _, digest := range []string{"ABCDEF", helpers.SHA256Hex(body)[:63] + "G", toUpperHex(helpers.SHA256Hex(body))} {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, w.base+"/attachments/presign", tok, map[string]any{
				"filename": "a.pdf", "byteSize": len(body), "sha256": digest,
			})
			require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", digest, raw)
			require.Equal(t, "REQUEST.VALIDATION", detailsCode(t, raw))
		}
	})

	t.Run("bytes that are not the declared ones are refused at confirm", func(t *testing.T) {
		requireStorage(t)
		declared := uniqueAttachmentBody("digest-declared")
		sent := make([]byte, len(declared))
		copy(sent, declared)
		sent[0] ^= 0xff

		var pres struct {
			AttachmentID string `json:"attachmentId"`
			UploadURL    string `json:"uploadUrl"`
		}
		helpers.DoJSON(t, http.MethodPost, w.base+"/attachments/presign", tok, map[string]any{
			"filename": "a.pdf", "contentType": "application/pdf",
			"byteSize": len(declared), "sha256": helpers.SHA256Hex(declared),
		}, &pres)
		require.NotEmpty(t, pres.UploadURL)
		helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", sent)

		status, raw := helpers.DoJSONStatus(t, http.MethodPost,
			w.base+"/attachments/"+pres.AttachmentID+"/confirm", tok, nil)
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		require.Equal(t, "UPLOAD.DIGEST_MISMATCH", detailsCode(t, raw))

		var listed []json.RawMessage
		helpers.DoJSON(t, http.MethodGet, w.base+"/attachments", tok, nil, &listed)
		require.Empty(t, listed, "a refused upload leaves no attachment behind")
	})
}

func toUpperHex(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	// A digest with no letter would not be changed; force one.
	return "A" + string(b[1:])
}

// A member who sees an event only as a block of time has no comments,
// checklist or attachments to read, and none to add.
func TestMaskedEventHasNoSubresourcesToAMember(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "Closed", "allDay": false, "visibility": "private",
		"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
	}, &evt)
	base := calURL + "/events/" + evt.ID

	var item detailItem
	helpers.DoJSON(t, http.MethodPost, base+"/checklist", owner.AccessToken, map[string]any{"title": "Secret"}, &item)

	for _, c := range []struct {
		name, method, path string
		body               any
	}{
		{"list comments", http.MethodGet, base + "/activities", nil},
		{"list checklist", http.MethodGet, base + "/checklist", nil},
		{"list attachments", http.MethodGet, base + "/attachments", nil},
		{"add comment", http.MethodPost, base + "/activities", map[string]any{"body": "hi"}},
		{"add checklist item", http.MethodPost, base + "/checklist", map[string]any{"title": "hi"}},
		{"patch checklist item", http.MethodPatch, base + "/checklist/" + item.ID, map[string]any{"done": true}},
		{"delete checklist item", http.MethodDelete, base + "/checklist/" + item.ID, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, raw := helpers.DoJSONStatusWithHeaders(t, c.method, c.path, member.AccessToken, c.body, ifMatch("*"))
			require.Equal(t, http.StatusNotFound, status, string(raw))
			require.NotContains(t, string(raw), "Secret")
		})
	}

	var listed []detailItem
	helpers.DoJSON(t, http.MethodGet, base+"/checklist", owner.AccessToken, nil, &listed)
	require.Len(t, listed, 1, "the owner's checklist is untouched")
	require.False(t, listed[0].Done)
}
