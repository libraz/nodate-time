package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// currentCalendarETag reads the entity tag a write to the calendar must carry.
func currentCalendarETag(t *testing.T, token, calendarID string) string {
	t.Helper()
	var cal struct {
		ETag string `json:"etag"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+calendarID, token, nil, &cal)
	return cal.ETag
}

// deleteCalendarWithETag deletes a calendar against the entity tag a fresh read
// of it returns.
func deleteCalendarWithETag(t *testing.T, tt *helpers.TestTenant, calendarID string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatusWithHeaders(t, http.MethodDelete,
		testServerURL+"/calendars/"+calendarID, tt.AccessToken, nil,
		ifMatch(currentCalendarETag(t, tt.AccessToken, calendarID)))
}

func photoStorageKey(t *testing.T, photoID string) string {
	t.Helper()
	var key string
	require.NoError(t, testDB.QueryRow(
		"SELECT storage_key FROM album_photos WHERE public_id = UUID_TO_BIN(?)", photoID,
	).Scan(&key))
	return key
}

// Deleting a calendar only disables the calendar row, so nothing cascades to
// its album. Photos left enabled sit in object storage for good, with no API
// path left that could name them again.
func TestDeletingACalendarRetiresItsAlbumPhotos(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	pres := uploadOnePhoto(t, tt, map[string]any{"caption": "goes with the calendar"})
	storageKey := photoStorageKey(t, pres.PhotoID)

	status, raw := deleteCalendarWithETag(t, tt, tt.CalendarID)
	require.Equal(t, http.StatusNoContent, status, "delete calendar: %s", string(raw))

	var enabled bool
	require.NoError(t, testDB.QueryRow(
		"SELECT enabled FROM album_photos WHERE public_id = UUID_TO_BIN(?)", pres.PhotoID,
	).Scan(&enabled))
	require.False(t, enabled, "a photo with no reachable calendar must be retired")

	// Age it past the retention window and confirm the bytes are reclaimed.
	_, err := testDB.Exec(
		"UPDATE album_photos SET updated_at = ? WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-30*24*time.Hour), pres.PhotoID)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "storage must be reclaimed once nothing can reach the photo")
}

// Retention runs from when a photo went out of use, not from when it was
// uploaded. Ageing by creation time gets it exactly backwards: a photo kept
// for a year would be collected on the very next pass after going out of use,
// while one that went out of use this morning would wait a year.
func TestRetiredPhotoKeepsItsBytesUntilTheWindowPasses(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	pres := uploadOnePhoto(t, tt, map[string]any{"caption": "long-lived"})
	storageKey := photoStorageKey(t, pres.PhotoID)

	// A photo that has existed for a year, retired just now with its calendar.
	_, err := testDB.Exec(
		"UPDATE album_photos SET created_at = ? WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-365*24*time.Hour), pres.PhotoID)
	require.NoError(t, err)

	status, raw := deleteCalendarWithETag(t, tt, tt.CalendarID)
	require.Equal(t, http.StatusNoContent, status, "delete calendar: %s", string(raw))

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists,
		"a photo retired moments ago must keep its bytes for the retention window")

	var rows int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM album_photos WHERE public_id = UUID_TO_BIN(?)", pres.PhotoID,
	).Scan(&rows))
	require.Equal(t, 1, rows, "and its row must still be there to be collected later")
}
