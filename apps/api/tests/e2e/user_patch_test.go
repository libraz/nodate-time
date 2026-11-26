package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type patchedUser struct {
	Name      string `json:"name"`
	Locale    string `json:"locale"`
	Timezone  string `json:"timezone"`
	WeekStart string `json:"weekStart"`
}

func patchUser(t *testing.T, token string, body map[string]any) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPatch, testServerURL+"/user", token, body)
}

func TestUserPatchChangesOnlyWhatIsSent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	var before patchedUser
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &before)

	status, raw := patchUser(t, tt.AccessToken, map[string]any{"name": "Renamed"})
	require.Equal(t, http.StatusOK, status, string(raw))
	var got patchedUser
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, patchedUser{Name: "Renamed", Locale: before.Locale, Timezone: before.Timezone, WeekStart: before.WeekStart}, got)

	status, raw = patchUser(t, tt.AccessToken, map[string]any{"timezone": "America/New_York", "locale": "en"})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, patchedUser{Name: "Renamed", Locale: "en", Timezone: "America/New_York", WeekStart: before.WeekStart}, got)

	status, raw = patchUser(t, tt.AccessToken, map[string]any{})
	require.Equal(t, http.StatusOK, status, "an empty patch changes nothing: %s", raw)
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, patchedUser{Name: "Renamed", Locale: "en", Timezone: "America/New_York", WeekStart: before.WeekStart}, got)
}

func TestUserPatchRefusesWhatCannotBeCleared(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	for name, body := range map[string]map[string]any{
		"null name":       {"name": nil},
		"null locale":     {"locale": nil},
		"null timezone":   {"timezone": nil},
		"empty name":      {"name": ""},
		"long name":       {"name": strings.Repeat("a", 101)},
		"unknown zone":    {"timezone": "Mars/Olympus"},
		"empty timezone":  {"timezone": ""},
		"overlong locale": {"locale": strings.Repeat("a", 36)},
	} {
		status, raw := patchUser(t, tt.AccessToken, body)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		require.Equal(t, "REQUEST.VALIDATION", adCode(t, raw), name)
	}

	var after patchedUser
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &after)
	require.Equal(t, tt.Name, after.Name, "a refused patch changes nothing")
}

func TestUserPatchWeekStart(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	var before patchedUser
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &before)
	require.Contains(t, []string{"mon", "sun", "sat"}, before.WeekStart)

	for _, ws := range []string{"mon", "sun", "sat"} {
		status, raw := patchUser(t, tt.AccessToken, map[string]any{"weekStart": ws})
		require.Equal(t, http.StatusOK, status, string(raw))
		var got patchedUser
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, ws, got.WeekStart)

		var reread patchedUser
		helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &reread)
		require.Equal(t, ws, reread.WeekStart, "GET reflects the saved value")
	}

	// Leave it somewhere other than where it started, then patch another member.
	other := "sat"
	if before.WeekStart == "sat" {
		other = "sun"
	}
	status, raw := patchUser(t, tt.AccessToken, map[string]any{"weekStart": other})
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = patchUser(t, tt.AccessToken, map[string]any{"name": "Renamed"})
	require.Equal(t, http.StatusOK, status, string(raw))
	var got patchedUser
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, other, got.WeekStart, "an omitted weekStart is left alone")
}

func TestUserPatchRefusesBadWeekStart(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	var before patchedUser
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &before)

	for name, v := range map[string]any{
		"null":      nil,
		"empty":     "",
		"uppercase": "MON",
		"unknown":   "tue",
		"non-ASCII": "月曜",
		"number":    1,
		"boolean":   true,
	} {
		status, raw := patchUser(t, tt.AccessToken, map[string]any{"weekStart": v})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		require.Equal(t, "REQUEST.VALIDATION", adCode(t, raw), name)
	}

	var after patchedUser
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &after)
	require.Equal(t, before.WeekStart, after.WeekStart, "a refused patch changes nothing")
}

func TestUserPutIsGone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user", tt.AccessToken,
		map[string]any{"name": "Renamed"})
	require.Equal(t, http.StatusMethodNotAllowed, status)
}

// The digest is lowercase hex, and bytes that differ from the declared digest
// are refused with the shared upload error.
func TestAvatarDigestIsCheckedOnConfirm(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := helpers.TinyPNG()

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": strings.ToUpper(helpers.SHA256Hex(png))})
	require.Equal(t, http.StatusUnprocessableEntity, status, "uppercase digest: %s", raw)

	// Declare the digest of other bytes of the same length, then upload the real ones.
	other := append([]byte(nil), png...)
	other[len(other)-1] ^= 0xff
	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(other)}, &pres)
	require.NotEmpty(t, pres.UploadURL)
	helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", png)

	status, raw = helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "UPLOAD.DIGEST_MISMATCH", adCode(t, raw))
}
