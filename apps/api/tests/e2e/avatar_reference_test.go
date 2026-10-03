package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// setAvatar runs presign / PUT / confirm. The same picture again comes back
// with no upload URL, because its bytes are already at the key.
func setAvatar(t *testing.T, tt *helpers.TestTenant, png []byte) {
	t.Helper()
	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)}, &pres)
	if pres.UploadURL != "" {
		helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", png)
	}
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID}, nil)
}

// Confirming the same picture twice is one user row pointing at one object,
// so it is one reference -- and clearing the avatar brings it to zero.
func TestReconfirmingTheSameAvatarHoldsOneReference(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()
	setAvatar(t, tt, png)
	key := avatarObjectKey(t, tt.Email)
	require.Equal(t, 1, storageObjectRefCount(t, key))

	setAvatar(t, tt, png)
	require.Equal(t, key, avatarObjectKey(t, tt.Email))
	require.Equal(t, 1, storageObjectRefCount(t, key), "the same picture again is not a second reference")

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/user/avatar", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 0, storageObjectRefCount(t, key), "and clearing it releases the only one")
}

// The avatar column is ON DELETE SET NULL, so a foreign key does not stop the
// sweep the way it does for attachments and photos. An object a user row still
// points at must not be swept even when its count says nothing holds it.
func TestTheSweepLeavesAnObjectAnAvatarPointsAt(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	setAvatar(t, tt, helpers.TinyPNG())
	key := avatarObjectKey(t, tt.Email)

	_, err := testDB.Exec(
		"UPDATE storage_objects SET ref_count = 0, created_at = ? WHERE storage_key = ?",
		time.Now().Add(-30*24*time.Hour), key)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	require.Equal(t, key, avatarObjectKey(t, tt.Email), "the avatar must survive a drifted count")
}

// A refused confirm deletes what it was handed only when no object claims the
// key. The same picture declared again with another type lands on the key the
// live avatar is served from, and refusing it must not blank that avatar.
func TestARefusedAvatarConfirmLeavesTheLivePicture(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()
	setAvatar(t, tt, png)
	key := avatarObjectKey(t, tt.Email)

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/jpeg", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)}, &pres)
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	require.Equal(t, http.StatusBadRequest, status)

	_, exists, err := getTestStorage().StatObject(testCtx(), key)
	require.NoError(t, err)
	require.True(t, exists, "the live avatar's bytes must survive a refused confirm")
	require.Equal(t, key, avatarObjectKey(t, tt.Email))
}
