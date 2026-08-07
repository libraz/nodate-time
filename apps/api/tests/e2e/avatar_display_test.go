package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadAvatar puts a picture on the tenant's account through the full
// presign/PUT/confirm path and returns the bytes that were stored, so a caller
// can prove a URL it was handed elsewhere resolves to this picture.
func uploadAvatar(t *testing.T, baseURL string, tt *helpers.TestTenant) []byte {
	t.Helper()
	png := helpers.TinyPNG()

	var pres avatarPresignResp
	helpers.DoJSON(t, http.MethodPost, baseURL+"/user/avatar/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)},
		&pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	var confirmed userResp
	helpers.DoJSON(t, http.MethodPut, baseURL+"/user/avatar", tt.AccessToken,
		map[string]any{"avatarId": pres.AvatarID}, &confirmed)
	require.NotEmpty(t, confirmed.AvatarURL, "confirm should answer with the new picture")
	return png
}

// requireShowsPicture asserts a URL a response handed out is a fetchable link
// to the uploaded picture rather than the storage key, an empty string, or a
// stale external URL. Nothing short of fetching it distinguishes those: a key
// presigned as if it were a key produces a plausible-looking URL that 404s.
func requireShowsPicture(t *testing.T, where, url string, want []byte) {
	t.Helper()
	require.NotEmpty(t, url, "%s should carry the uploaded avatar", where)
	assert.Equal(t, want, helpers.FetchURL(t, url), "%s should resolve to the uploaded picture", where)
}

// TestUploadedAvatarShowsWhereverAUserIsRendered covers the whole set of
// responses that draw a person: an avatar that only appears on the profile
// page is, from the reader's side, an avatar that does not work.
func TestUploadedAvatarShowsWhereverAUserIsRendered(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")

	png := uploadAvatar(t, testServerURL, member)

	// The member sheet.
	var members []struct {
		ID     string `json:"id"`
		Avatar string `json:"avatar"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &members)
	found := false
	for _, m := range members {
		if m.ID == member.UserID {
			found = true
			requireShowsPicture(t, "the member list", m.Avatar, png)
		}
	}
	require.True(t, found, "the member who uploaded should be in the list")

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "打ち合わせ",
			"allDay":  false,
			"startAt": "2026-09-03T10:00:00+09:00",
			"endAt":   "2026-09-03T11:00:00+09:00",
		}, &evt)
	eventURL := calURL + "/events/" + evt.ID

	// A comment, both as the write answers and as the thread reads back.
	var created struct {
		UserAvatar string `json:"userAvatar"`
	}
	helpers.DoJSON(t, http.MethodPost, eventURL+"/activities", member.AccessToken,
		map[string]any{"content": "資料を持っていきます"}, &created)
	requireShowsPicture(t, "the comment that was just posted", created.UserAvatar, png)

	var thread struct {
		Items []struct {
			UserPublicID string `json:"userPublicId"`
			UserAvatar   string `json:"userAvatar"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, eventURL+"/activities", owner.AccessToken, nil, &thread)
	require.Len(t, thread.Items, 1)
	requireShowsPicture(t, "the comment thread", thread.Items[0].UserAvatar, png)

	// The album listing.
	uploadOnePhotoAs(t, calURL, member, map[string]any{"caption": "現地の写真"})
	var album albumListResp
	helpers.DoJSON(t, http.MethodGet, calURL+"/albums", owner.AccessToken, nil, &album)
	require.Len(t, album.Items, 1)
	requireShowsPicture(t, "the album listing", album.Items[0].UploadedBy.AvatarURL, png)

	// The activity feed.
	var feed struct {
		Items []struct {
			Actor *struct {
				ID        string `json:"id"`
				AvatarURL string `json:"avatarUrl"`
			} `json:"actor"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity", owner.AccessToken, nil, &feed)
	seen := false
	for _, item := range feed.Items {
		if item.Actor != nil && item.Actor.ID == member.UserID {
			seen = true
			requireShowsPicture(t, "the activity feed", item.Actor.AvatarURL, png)
		}
	}
	require.True(t, seen, "the member's actions should appear in the feed")
}

// TestUploadedAvatarSurvivesAsAFallbackForTheExternalOne pins that uploading a
// picture does not erase the address the identity provider supplied. The
// uploaded object wins while it is there, but the column behind it is cleared
// by a foreign key when the blob is swept, and an account that had lost its
// external URL would then have no picture at all.
func TestUploadedAvatarSurvivesAsAFallbackForTheExternalOne(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	const external = "https://provider.example/picture.png"
	_, err := testDB.Exec(`UPDATE users SET avatar_url = ? WHERE public_id = UUID_TO_BIN(?)`, external, tt.UserID)
	require.NoError(t, err)

	uploadAvatar(t, testServerURL, tt)

	var stored string
	require.NoError(t, testDB.QueryRow(
		`SELECT COALESCE(avatar_url, '') FROM users WHERE public_id = UUID_TO_BIN(?)`, tt.UserID,
	).Scan(&stored))
	assert.Equal(t, external, stored, "uploading a picture must not discard the provider's")
}

// TestMemberListAvatarsCostAFixedNumberOfQueries pins the shape of the read.
//
// Resolving a picture needs its storage key, and asking for one member's key at
// a time is a listing that stays correct as it degrades: the sheet renders,
// just at one more round trip per person who has uploaded one.
func TestMemberListAvatarsCostAFixedNumberOfQueries(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	srv, counter := helpers.NewCountingTestServer(t, testDB)
	owner := helpers.NewTenant(t, srv.BaseURL)
	calURL := srv.BaseURL + "/calendars/" + owner.CalendarID
	uploadAvatar(t, srv.BaseURL, owner)

	measure := func() (queries, withAvatars int) {
		t.Helper()
		counter.Reset()
		var members []struct {
			Avatar string `json:"avatar"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &members)
		queries = counter.Count()
		for _, m := range members {
			if m.Avatar != "" {
				withAvatars++
			}
		}
		return queries, withAvatars
	}

	before, avatarsBefore := measure()
	require.Equal(t, 1, avatarsBefore, "the owner's picture should already show")

	for range 3 {
		joiner := helpers.NewTenant(t, srv.BaseURL)
		joinAs2(t, srv.BaseURL, calURL, owner.AccessToken, joiner, "editor")
		uploadAvatar(t, srv.BaseURL, joiner)
	}

	after, avatarsAfter := measure()
	require.Equal(t, 4, avatarsAfter, "every member's picture should show")
	require.Equal(t, before, after,
		"four uploaded avatars should cost the same as one: %d then %d queries for %d then %d pictures",
		before, after, avatarsBefore, avatarsAfter)
}

// uploadOnePhotoAs is uploadOnePhoto against a named calendar, so a member
// other than its owner can be the uploader.
func uploadOnePhotoAs(t *testing.T, calURL string, tt *helpers.TestTenant, body map[string]any) {
	t.Helper()
	png := helpers.TinyPNG()
	body["contentType"] = "image/png"
	body["byteSize"] = len(png)
	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, calURL+"/albums/presign", tt.AccessToken, body, &pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)
	helpers.DoJSON(t, http.MethodPost, fmt.Sprintf("%s/albums/%s/confirm", calURL, pres.PhotoID),
		tt.AccessToken, nil, nil)
}
