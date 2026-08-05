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

// An avatar's storage key is derived from the user and the digest of the
// bytes, so uploading the same picture again reserves the key the confirmed
// avatar is already served from. Abandoning that second reservation must not
// take the first one's bytes with it — the user's row still points at them,
// so the avatar would 404 with nothing to explain why.
func TestAbandonedRepeatAvatarUploadKeepsTheLivePicture(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)},
		&pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var confirmed userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID}, &confirmed)
	require.NotEmpty(t, confirmed.AvatarURL)

	var storageKey string
	require.NoError(t, testDB.QueryRow(`
		SELECT so.storage_key FROM users u
		INNER JOIN storage_objects so ON so.id = u.avatar_storage_object_id
		WHERE u.email = ?`, tt.Email).Scan(&storageKey))

	// The same picture again, then walk away from it.
	var second avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)},
		&second)
	require.NotEqual(t, pres.AvatarID, second.AvatarID)

	_, err := testDB.Exec(
		"UPDATE avatar_uploads SET expires_at = ? WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-30*24*time.Hour), second.AvatarID)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists, "the confirmed avatar's bytes must survive an abandoned repeat upload")

	var me userResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &me)
	require.NotEmpty(t, me.AvatarURL, "and the account must still serve it")
}

// A reservation for a picture that was never confirmed anywhere still gets its
// bytes reclaimed: nothing in storage_objects claims that key.
func TestAbandonedAvatarUploadReclaimsItsOwnBytes(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)},
		&pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var storageKey string
	require.NoError(t, testDB.QueryRow(
		"SELECT storage_key FROM avatar_uploads WHERE public_id = UUID_TO_BIN(?)", pres.AvatarID,
	).Scan(&storageKey))

	_, err := testDB.Exec(
		"UPDATE avatar_uploads SET expires_at = ? WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-30*24*time.Hour), pres.AvatarID)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "an upload nobody confirmed must not keep its bytes forever")
}
