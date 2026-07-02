package e2e

import (
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
	AvatarID   string `json:"avatarId"`
	UploadURL  string `json:"uploadUrl"`
	StorageKey string `json:"storageKey"`
}

type userResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
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
		map[string]any{"contentType": "image/png", "byteSize": len(png)},
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
		map[string]any{"contentType": "image/png", "byteSize": 6 * 1024 * 1024})
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
		map[string]any{"contentType": "application/pdf", "byteSize": 100})
	assert.Equal(t, 400, status)
}

func TestAvatarConfirmWithoutUpload(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 64},
		&pres)
	// Skip the PUT — confirm should fail because no object exists.
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	assert.Equal(t, 404, status)
}

func TestAvatarDeleteRestoresEmoji(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png)}, &pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var confirmed userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID}, &confirmed)
	require.NotEmpty(t, confirmed.AvatarURL)

	var deleted userResp
	helpers.DoJSON(t, http.MethodDelete, testServerURL+"/user/avatar", tt.AccessToken, nil, &deleted)
	assert.Empty(t, deleted.AvatarURL)
	assert.NotEmpty(t, deleted.Icon, "emoji icon should still be present")
}

func TestAvatarReplaceClearsOldKey(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	// First upload
	var pres1 avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png)}, &pres1)
	helpers.UploadToPresignedURL(t, pres1.UploadURL, "image/png", png)
	var u1 userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres1.AvatarID}, &u1)
	require.NotEmpty(t, u1.AvatarURL)

	// Second upload — should replace and leave only the new object.
	var pres2 avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png)}, &pres2)
	require.NotEqual(t, pres1.StorageKey, pres2.StorageKey)
	helpers.UploadToPresignedURL(t, pres2.UploadURL, "image/png", png)
	var u2 userResp
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres2.AvatarID}, &u2)
	require.NotEmpty(t, u2.AvatarURL)

	// The first object should now be gone from MinIO. We assert via StatObject.
	if testStorageClient := getTestStorage(); testStorageClient != nil {
		_, exists, err := testStorageClient.StatObject(testCtx(), pres1.StorageKey)
		require.NoError(t, err)
		assert.False(t, exists, "previous avatar object should be removed")
	}
}

func TestAvatarUnauthorized(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", "",
		map[string]any{"contentType": "image/png", "byteSize": 100})
	assert.Equal(t, 401, status)
}

func TestAvatarWithoutStorageAvailable(t *testing.T) {
	bootstrap(t)
	if helpers.StorageEnabled() {
		t.Skip("only meaningful when storage is disabled")
	}
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 100})
	assert.Equal(t, 503, status)
}
