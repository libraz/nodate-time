package e2e

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireStorage skips the test if MinIO is not enabled.
func requireStorage(t *testing.T) {
	t.Helper()
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}
}

type avatarPresignResp struct {
	AvatarID  string `json:"avatarId"`
	UploadURL string `json:"uploadUrl"`
}

type userResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func TestAvatarUploadHappyPath(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)},
		&pres)
	require.NotEmpty(t, pres.UploadURL)
	require.NotEmpty(t, pres.AvatarID)

	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var confirmed userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID},
		&confirmed)
	require.NotEmpty(t, confirmed.AvatarURL, "avatarUrl should be populated after confirm")

	// /user should return the avatar URL too.
	var me userResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &me)
	require.NotEmpty(t, me.AvatarURL)

	// The presigned URL should be fetchable and return the same bytes.
	got := helpers.FetchURL(t, me.AvatarURL)
	assert.Equal(t, png, got)
}

func TestAvatarTooLarge(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, body := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 6 * 1024 * 1024, "sha256": helpers.SHA256Hex([]byte("oversized"))})
	assert.Equal(t, 400, status)
	assert.True(t, strings.Contains(string(body), "5MB") || strings.Contains(string(body), "exceeds"),
		"expected size-limit message, got %s", string(body))
}

func TestAvatarInvalidContentType(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "application/pdf", "byteSize": 100, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	assert.Equal(t, 400, status)
}

func TestAvatarConfirmWithoutUpload(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 64, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		&pres)
	// Skip the PUT — confirm should fail because no object exists.
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	assert.Equal(t, 404, status)
}

// TestAvatarPresignedPutRejectsMismatchedContentLength verifies the presigned
// PUT itself — not just Confirm — rejects a body whose length disagrees with
// the byteSize declared (and signed) at presign time, closing the window
// where a much larger object could land in storage before any size check ran.
func TestAvatarPresignedPutRejectsMismatchedContentLength(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png) - 1, "sha256": helpers.SHA256Hex(png)}, &pres)
	status, _ := helpers.UploadToPresignedURLStatus(t, pres.UploadURL, "image/png", png)
	assert.True(t, status >= 400, "expected the signed Content-Length mismatch to be rejected, got %d", status)

	// Nothing was ever stored, so Confirm legitimately finds no object.
	confirmStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	assert.Equal(t, http.StatusNotFound, confirmStatus)
}

// TestAvatarConfirmRejectsMismatchedActualSizeAndDeletesObject verifies the
// Confirm-time defense-in-depth: if an object at the presigned key ever
// disagrees with what was declared (however it got there — the presigned PUT
// itself now rejects a mismatch, so this exercises the fallback), Confirm
// rejects it and removes the object rather than leaving an orphan.
func TestAvatarConfirmRejectsMismatchedActualSizeAndDeletesObject(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png) - 1, "sha256": helpers.SHA256Hex(png)}, &pres)
	storageKey := helpers.AvatarStorageKey(tt.UserID, helpers.SHA256Hex(png))
	// Bypass the presigned URL to place an object whose size disagrees with
	// what was declared, simulating the object ending up mismatched regardless
	// of the upload path.
	helpers.PutRawObject(t, getTestBucket(), storageKey, "image/png", png)

	status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	assert.Equal(t, http.StatusBadRequest, status)

	if storageClient := getTestStorage(); storageClient != nil {
		_, exists, err := storageClient.StatObject(testCtx(), storageKey)
		require.NoError(t, err)
		assert.False(t, exists, "invalid avatar object should be removed")
	}
	status, _ = helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	assert.Equal(t, http.StatusNotFound, status)
}

func TestAvatarPresignLimitsActiveSessionsPerUser(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	for range 5 {
		var pres avatarPresignResp
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
			map[string]any{"contentType": "image/png", "byteSize": 64, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}, &pres)
		require.NotEmpty(t, pres.AvatarID)
	}

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 64, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	assert.Equal(t, http.StatusTooManyRequests, status)
}

func TestAvatarDeleteLeavesTheAccountIntact(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)}, &pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var confirmed userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID}, &confirmed)
	require.NotEmpty(t, confirmed.AvatarURL)

	var deleted userResp
	helpers.DoJSON(t, http.MethodDelete, testServerURL+"/user/avatar", tt.AccessToken, nil, &deleted)
	assert.Empty(t, deleted.AvatarURL)
	// Removing a picture removes the picture, not the account: the name is
	// still there, and so is the reference count on the blob, which the
	// sweep collects once nothing points at it.
	assert.NotEmpty(t, deleted.Name, "the account must survive deleting its avatar")
}

// solidPNG returns a valid PNG that is not helpers.TinyPNG(). Replacement is
// only exercised by a picture whose bytes differ: the storage key is the digest
// of the bytes, so uploading the same image twice lands on the key the account
// already serves and nothing is ever replaced.
func solidPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// avatarObjectKey reads the storage key the account's avatar currently points
// at, which is what replacement has to move.
func avatarObjectKey(t *testing.T, email string) string {
	t.Helper()
	var key string
	require.NoError(t, testDB.QueryRow(`
		SELECT so.storage_key FROM users u
		INNER JOIN storage_objects so ON so.id = u.avatar_storage_object_id
		WHERE u.email = ?`, email).Scan(&key))
	return key
}

func storageObjectRefCount(t *testing.T, storageKey string) int {
	t.Helper()
	var refs int
	require.NoError(t, testDB.QueryRow(
		"SELECT ref_count FROM storage_objects WHERE storage_key = ?", storageKey).Scan(&refs))
	return refs
}

// Replacing an avatar has to actually swap the picture: the account serves the
// new bytes and lets go of the old object. The blob itself stays until the
// sweep, because the same bytes may still be somebody else's avatar -- so what
// replacement owes is the reference, not the deletion.
func TestAvatarReplaceServesTheNewImageAndReleasesTheOld(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	first := helpers.TinyPNG()
	second := solidPNG(t)
	firstKey := helpers.AvatarStorageKey(tt.UserID, helpers.SHA256Hex(first))
	secondKey := helpers.AvatarStorageKey(tt.UserID, helpers.SHA256Hex(second))
	require.NotEqual(t, firstKey, secondKey, "the two pictures must be different ones")

	var pres1 avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(first), "sha256": helpers.SHA256Hex(first)}, &pres1)
	helpers.UploadToPresignedURL(t, pres1.UploadURL, "image/png", first)
	var u1 userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres1.AvatarID}, &u1)
	require.NotEmpty(t, u1.AvatarURL)
	require.Equal(t, firstKey, avatarObjectKey(t, tt.Email))
	require.Equal(t, first, helpers.FetchURL(t, u1.AvatarURL))

	// Second upload -- a different picture, so a different key to move to.
	var pres2 avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(second), "sha256": helpers.SHA256Hex(second)}, &pres2)
	require.NotEqual(t, pres1.AvatarID, pres2.AvatarID)
	helpers.UploadToPresignedURL(t, pres2.UploadURL, "image/png", second)
	var u2 userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres2.AvatarID}, &u2)
	require.NotEmpty(t, u2.AvatarURL)

	// What a client sees: the replacement, not the picture it replaced.
	var me userResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &me)
	assert.Equal(t, secondKey, avatarObjectKey(t, tt.Email), "the account should point at the new picture")
	assert.Equal(t, second, helpers.FetchURL(t, me.AvatarURL), "the account must serve the new picture")

	// And the old object is nobody's now, which is what makes it collectable.
	assert.Equal(t, 0, storageObjectRefCount(t, firstKey), "replacing an avatar must release the old object")
	assert.Equal(t, 1, storageObjectRefCount(t, secondKey))

	// Its bytes stay put until the sweep judges them; dropping them here would
	// break every other account holding the same picture.
	if testStorageClient := getTestStorage(); testStorageClient != nil {
		_, exists, err := testStorageClient.StatObject(testCtx(), firstKey)
		require.NoError(t, err)
		assert.True(t, exists, "the released blob is the sweep's to collect, not the handler's")
	}
}

func TestAvatarUnauthorized(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", "",
		map[string]any{"contentType": "image/png", "byteSize": 100, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	assert.Equal(t, 401, status)
}

func TestStorageAbsentAvatarPresign(t *testing.T) {
	bootstrap(t)
	requireStorageAbsent(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 100, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	assert.Equal(t, 503, status)
}
