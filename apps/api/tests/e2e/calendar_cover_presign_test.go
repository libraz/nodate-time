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

type coverCalendar struct {
	CoverImage string `json:"coverImageUrl"`
	ETag       string `json:"etag"`
}

// coverPNG is a small image no other test uploads, so its digest is its own.
func coverPNG(t *testing.T) []byte {
	t.Helper()
	marker := uuid.New()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i, v := range marker {
		img.SetNRGBA(i%4, i/4, color.NRGBA{R: v, B: 160, A: 255})
	}
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func coverPresign(t *testing.T, calURL, token string, data []byte) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost, calURL+"/cover/presign", token, map[string]any{
		"sha256": helpers.SHA256Hex(data), "contentType": "image/png", "byteSize": len(data),
	})
}

func coverConfirm(t *testing.T, calURL, token, coverID string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPut, calURL+"/cover", token, map[string]any{"coverId": coverID})
}

type coverReservation struct {
	CoverID   string `json:"coverId"`
	UploadURL string `json:"uploadUrl"`
}

// coverReserve presigns data and returns the reservation.
func coverReserve(t *testing.T, calURL, token string, data []byte) coverReservation {
	t.Helper()
	status, raw := coverPresign(t, calURL, token, data)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var out coverReservation
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotEmpty(t, out.CoverID)
	return out
}

func coverErrorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(raw, &e))
	return e.Code
}

// A cover is reserved with the digest of its bytes, uploaded to the URL that
// reservation returns, and attached only when the confirm has read the bytes
// back and found them to be those.
func TestCalendarCoverIsPresignedThenConfirmed(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	editor := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, editor, "editor")
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &invite)

	data := coverPNG(t)

	// An editor may not change the cover, at either step.
	status, raw := coverPresign(t, calURL, editor.AccessToken, data)
	require.Equal(t, http.StatusForbidden, status, string(raw))
	status, raw = coverConfirm(t, calURL, editor.AccessToken, uuid.NewString())
	require.Equal(t, http.StatusForbidden, status, string(raw))

	// Reserving changes nothing about the calendar.
	reserved := coverReserve(t, calURL, owner.AccessToken, data)
	require.NotEmpty(t, reserved.UploadURL)
	var before coverCalendar
	helpers.DoJSON(t, http.MethodGet, calURL, owner.AccessToken, nil, &before)
	require.Empty(t, before.CoverImage)

	// An ID nobody reserved names nothing.
	status, raw = coverConfirm(t, calURL, owner.AccessToken, uuid.NewString())
	require.Equal(t, http.StatusNotFound, status, string(raw))

	// Confirming before the bytes arrive finds nothing to attach.
	status, raw = coverConfirm(t, calURL, owner.AccessToken, reserved.CoverID)
	require.Equal(t, http.StatusConflict, status, string(raw))

	helpers.UploadToSignedURL(t, reserved.UploadURL, "image/png", data)
	status, raw = coverConfirm(t, calURL, owner.AccessToken, reserved.CoverID)
	require.Equal(t, http.StatusOK, status, string(raw))
	var confirmed coverCalendar
	require.NoError(t, json.Unmarshal(raw, &confirmed))
	require.Equal(t, data, helpers.FetchURL(t, confirmed.CoverImage))

	// The same bytes need no second upload, and confirming again changes nothing.
	again := coverReserve(t, calURL, owner.AccessToken, data)
	require.Empty(t, again.UploadURL)
	require.Equal(t, reserved.CoverID, again.CoverID)
	status, raw = coverConfirm(t, calURL, owner.AccessToken, again.CoverID)
	require.Equal(t, http.StatusOK, status, string(raw))

	// Removing the cover answers with no body, and the calendar shows none.
	status, raw = helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/cover", owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	var after coverCalendar
	helpers.DoJSON(t, http.MethodGet, calURL, owner.AccessToken, nil, &after)
	require.Empty(t, after.CoverImage)
}

// Bytes that are not the ones declared are refused with the digest code and do
// not stay behind as a reservation.
func TestCalendarCoverWithTheWrongBytesIsRefused(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	declared := coverPNG(t)
	sent := bytes.Clone(declared)
	sent[len(sent)-1] ^= 0x01
	digest := helpers.SHA256Hex(declared)

	reserved := coverReserve(t, calURL, owner.AccessToken, declared)
	helpers.UploadToSignedURL(t, reserved.UploadURL, "image/png", sent)

	status, raw := coverConfirm(t, calURL, owner.AccessToken, reserved.CoverID)
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "UPLOAD.DIGEST_MISMATCH", coverErrorCode(t, raw))

	var left int
	require.NoError(t, testDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM storage_objects WHERE workspace_id IS NOT NULL AND sha256 = UNHEX(?)`, digest).Scan(&left))
	require.Zero(t, left, "the rejected reservation is dropped")
	var calendar coverCalendar
	helpers.DoJSON(t, http.MethodGet, calURL, owner.AccessToken, nil, &calendar)
	require.Empty(t, calendar.CoverImage)
}

// What is not a JPEG, PNG or WebP image is refused even when its bytes match
// the digest it was reserved with.
func TestCalendarCoverMustBeAnImage(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><!--` + uuid.NewString() + `--></svg>`)
	reserved := coverReserve(t, calURL, owner.AccessToken, data)
	helpers.UploadToSignedURL(t, reserved.UploadURL, "image/png", data)

	status, raw := coverConfirm(t, calURL, owner.AccessToken, reserved.CoverID)
	require.Equal(t, http.StatusBadRequest, status, string(raw))
	require.Equal(t, "CALENDAR.COVER_INVALID", coverErrorCode(t, raw))
}

// The declaration is checked before anything is reserved.
func TestCalendarCoverPresignChecksTheDeclaration(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	digest := helpers.SHA256Hex(coverPNG(t))

	for name, body := range map[string]map[string]any{
		"upper-case digest": {"sha256": "ABCDEF" + digest[6:], "contentType": "image/png", "byteSize": 10},
		"short digest":      {"sha256": digest[:60], "contentType": "image/png", "byteSize": 10},
		"not an image":      {"sha256": digest, "contentType": "image/svg+xml", "byteSize": 10},
		"empty":             {"sha256": digest, "contentType": "image/png", "byteSize": 0},
	} {
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/cover/presign", owner.AccessToken, body)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		require.Equal(t, "REQUEST.VALIDATION", coverErrorCode(t, raw), name)
	}
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/cover/presign", owner.AccessToken,
		map[string]any{"sha256": digest, "contentType": "image/png", "byteSize": (5 << 20) + 1})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
}
