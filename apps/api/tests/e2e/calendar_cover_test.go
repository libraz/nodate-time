package e2e

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type calendarCoverBody struct {
	ID         string `json:"id"`
	CoverImage string `json:"coverImageUrl"`
	CanUpload  bool   `json:"coverUploadAllowed"`
	Public     bool   `json:"publicShared"`
}

func newCalendarCoverPNG(t *testing.T) []byte {
	t.Helper()
	marker := uuid.New()
	cover := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for index, value := range marker {
		cover.SetNRGBA(index%4, index/4, color.NRGBA{R: value, B: 160, A: 255})
	}
	var body bytes.Buffer
	require.NoError(t, png.Encode(&body, cover))
	return body.Bytes()
}

// putCalendarCover reserves, uploads and confirms a cover, and answers with
// the status and body of the last step it reached.
func putCalendarCover(t *testing.T, calURL, token string, image []byte) (int, []byte) {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/cover/presign", token, map[string]any{
		"sha256": helpers.SHA256Hex(image), "contentType": "image/png", "byteSize": len(image),
	})
	if status != http.StatusCreated {
		return status, raw
	}
	var reserved struct {
		CoverID   string `json:"coverId"`
		UploadURL string `json:"uploadUrl"`
	}
	require.NoError(t, json.Unmarshal(raw, &reserved))
	if reserved.UploadURL != "" {
		helpers.UploadToSignedURL(t, reserved.UploadURL, "image/png", image)
	}
	return helpers.DoJSONStatus(t, http.MethodPut, calURL+"/cover", token, map[string]any{"coverId": reserved.CoverID})
}

func TestCalendarCoverUpload(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	manager := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, manager, "editor")
	require.Equal(t, http.StatusOK, promote(t, calURL, owner.AccessToken, manager, "manager"))
	joinAs(t, calURL, owner.AccessToken, member, "editor")
	var publicInvite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &publicInvite)
	require.NotEmpty(t, publicInvite.Token)

	// Uploaded covers survive ordinary calendar edits and are the only cover
	// field returned by the calendar API.
	image := newCalendarCoverPNG(t)
	status, body := putCalendarCover(t, calURL, owner.AccessToken, image)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var uploaded calendarCoverBody
	require.NoError(t, json.Unmarshal(body, &uploaded))
	require.NotContains(t, string(body), `"coverUrl"`)
	require.True(t, uploaded.CanUpload)
	require.True(t, uploaded.Public)
	require.Equal(t, image, helpers.FetchURL(t, uploaded.CoverImage))
	status, body = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/"+publicInvite.Token, "", nil)
	require.Equal(t, http.StatusOK, status, "%s", body)
	require.NotContains(t, string(body), "coverImageUrl")
	var edited calendarCoverBody
	patchNowOK(t, calURL, owner.AccessToken, map[string]any{"name": "New name"}, &edited)
	require.Equal(t, image, helpers.FetchURL(t, edited.CoverImage))

	status, body = putCalendarCover(t, calURL, member.AccessToken, image)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	status, body = putCalendarCover(t, calURL, owner.AccessToken, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	require.True(t, status == http.StatusBadRequest || status == http.StatusUnsupportedMediaType,
		"invalid image should be rejected: %d %s", status, body)
	status, body = putCalendarCover(t, calURL, owner.AccessToken, make([]byte, 5<<20))
	require.Equal(t, http.StatusBadRequest, status, "a 5 MiB body reaches image validation: %s", body)
	status, body = putCalendarCover(t, calURL, owner.AccessToken, make([]byte, (5<<20)+1))
	require.Equal(t, http.StatusUnprocessableEntity, status, "a declaration over 5 MiB is refused at presign: %s", body)

	var listed []calendarCoverBody
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", member.AccessToken, nil, &listed)
	var memberCalendar calendarCoverBody
	for _, result := range listed {
		if result.ID == owner.CalendarID {
			memberCalendar = result
			break
		}
	}
	require.Equal(t, owner.CalendarID, memberCalendar.ID)
	require.False(t, memberCalendar.CanUpload)
	require.Equal(t, image, helpers.FetchURL(t, memberCalendar.CoverImage))
	status, body = helpers.DoJSONStatus(t, http.MethodGet, calURL, outsider.AccessToken, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, status)
	require.NotContains(t, string(body), "coverImageUrl")
	status, body = helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/cover", member.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status, "%s", body)

	// The calendar grant alone must not issue new signed links once workspace
	// access has been revoked.
	_, err := testDB.ExecContext(t.Context(), `UPDATE workspace_members SET enabled = FALSE
		WHERE user_id = (SELECT id FROM users WHERE public_id = UUID_TO_BIN(?))`, member.UserID)
	require.NoError(t, err)
	status, body = helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars", member.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "%s", body)
	require.NotContains(t, string(body), owner.CalendarID)

	refCount := func(contents []byte) uint32 {
		var refs uint32
		require.NoError(t, testDB.QueryRowContext(t.Context(),
			`SELECT ref_count FROM storage_objects WHERE workspace_id IS NOT NULL AND sha256 = UNHEX(?)`,
			helpers.SHA256Hex(contents)).Scan(&refs))
		return refs
	}
	status, body = putCalendarCover(t, calURL, owner.AccessToken, image)
	require.Equal(t, http.StatusOK, status, "%s", body)
	require.EqualValues(t, 1, refCount(image), "retrying the same upload keeps one reference")
	secondURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Other shared calendar")
	status, body = putCalendarCover(t, secondURL, owner.AccessToken, image)
	require.Equal(t, http.StatusOK, status, "%s", body)
	require.EqualValues(t, 2, refCount(image), "two shared calendars deduplicate the bytes")

	replacement := newCalendarCoverPNG(t)
	status, body = putCalendarCover(t, calURL, manager.AccessToken, replacement)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var replaced calendarCoverBody
	require.NoError(t, json.Unmarshal(body, &replaced))
	require.Equal(t, replacement, helpers.FetchURL(t, replaced.CoverImage))
	require.EqualValues(t, 1, refCount(image), "replacement releases only the old reference")
	require.EqualValues(t, 1, refCount(replacement))

	var removed, stillShared calendarCoverBody
	status, body = helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/cover", owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	helpers.DoJSON(t, http.MethodGet, calURL, owner.AccessToken, nil, &removed)
	require.Empty(t, removed.CoverImage)
	require.True(t, removed.Public)
	require.EqualValues(t, 0, refCount(replacement), "removing a cover releases its object")
	require.EqualValues(t, 1, refCount(image), "removing a cover leaves the other calendar's reference")
	helpers.DoJSON(t, http.MethodGet, secondURL, owner.AccessToken, nil, &stillShared)
	require.Equal(t, image, helpers.FetchURL(t, stillShared.CoverImage))

	status = taggedStatus(t, http.MethodDelete, secondURL, owner.AccessToken, currentTag(t, secondURL, owner.AccessToken), nil)
	require.Equal(t, http.StatusNoContent, status)
	require.EqualValues(t, 0, refCount(image), "deleting a calendar releases its uploaded cover")
}

func TestCalendarCoverDoesNotBlockWorkspaceTeardown(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	ctx := t.Context()
	tx, err := testDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	id, err := uuid.NewV7()
	require.NoError(t, err)
	workspace, err := tx.ExecContext(ctx, `INSERT INTO workspaces (public_id, slug, name) VALUES (?, ?, ?)`,
		id[:], "cover-"+id.String(), "Cover teardown test")
	require.NoError(t, err)
	workspaceID, err := workspace.LastInsertId()
	require.NoError(t, err)

	calendar, err := tx.ExecContext(ctx, `INSERT INTO calendars (public_id, workspace_id, name) VALUES (UUID_TO_BIN(UUID()), ?, ?)`,
		workspaceID, "Shared calendar")
	require.NoError(t, err)
	calendarID, err := calendar.LastInsertId()
	require.NoError(t, err)
	object, err := tx.ExecContext(ctx, `INSERT INTO storage_objects
		(public_id, workspace_id, sha256, byte_size, content_type, storage_key, ref_count, uploaded_at)
		VALUES (UUID_TO_BIN(UUID()), ?, UNHEX(SHA2(?, 256)), 1, 'image/png', ?, 1, CURRENT_TIMESTAMP(3))`,
		workspaceID, id.String(), "calendar-covers/"+id.String())
	require.NoError(t, err)
	objectID, err := object.LastInsertId()
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO calendar_cover_images (calendar_id, storage_object_id) VALUES (?, ?)`, calendarID, objectID)
	require.NoError(t, err)

	// Flow deletes the common workspace without access to Time's link table.
	_, err = tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, workspaceID)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM storage_objects WHERE id = ?`, objectID).Scan(&remaining))
	require.Zero(t, remaining)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM calendar_cover_images WHERE calendar_id = ?`, calendarID).Scan(&remaining))
	require.Zero(t, remaining)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM storage_blob_removals WHERE storage_key = ?`,
		"calendar-covers/"+id.String()).Scan(&remaining))
	require.Equal(t, 1, remaining, "the blob key survives teardown for the cleanup worker")
}
