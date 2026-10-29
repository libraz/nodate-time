package cleanup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A tick that starts from the first photo every time spends its whole budget
// on the unreadable ones ahead of a readable photo, however many ticks pass.
// Everything is written in a transaction that is rolled back, so the photos
// neither reach nor slow any other test's backfill.
func TestBackfillGetsPastUnreadablePhotosAheadOfAReadableOne(t *testing.T) {
	if os.Getenv("TC_TEST_INTEGRATION") == "" || !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_INTEGRATION=1 and TC_TEST_STORAGE=1 to run")
	}
	ctx := context.Background()
	db := helpers.OpenTestDB(t)
	srv := helpers.NewTestServer(t, db)
	tt := helpers.NewTenant(t, srv.BaseURL)

	var calendarID, workspaceID, userID uint32
	require.NoError(t, db.QueryRow(
		`SELECT c.id, c.workspace_id, u.id FROM calendars c, users u
		  WHERE c.public_id = UUID_TO_BIN(?) AND u.public_id = UUID_TO_BIN(?)`,
		tt.CalendarID, tt.UserID).Scan(&calendarID, &workspaceID, &userID))

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	insert := func(key string, size int) uint32 {
		id, err := uuid.NewV7()
		require.NoError(t, err)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO album_photos (public_id, workspace_id, calendar_id, uploaded_by_user_id,
			                           content_type, byte_size, storage_key, enabled)
			 VALUES (?, ?, ?, ?, 'image/png', ?, ?, TRUE)`,
			id[:], workspaceID, calendarID, userID, size, key)
		require.NoError(t, err)
		rowID, err := res.LastInsertId()
		require.NoError(t, err)
		return uint32(rowID)
	}

	stamp := uuid.NewString()
	for i := range albumBackfillBatchSize*albumBackfillBatches + 1 {
		insert(fmt.Sprintf("backfill-test/%s/missing-%d", stamp, i), 1)
	}

	png := append(helpers.TinyPNG(), []byte("backfill-"+stamp)...)
	readableKey := "backfill-test/" + stamp + "/readable"
	require.NoError(t, srv.Storage.Put(ctx, readableKey, bytes.NewReader(png), int64(len(png)), "image/png"))
	readable := insert(readableKey, len(png))

	attached := func() bool {
		var n int
		require.NoError(t, tx.QueryRow(
			`SELECT COUNT(*) FROM album_photos WHERE id = ? AND storage_object_id IS NOT NULL`, readable).Scan(&n))
		return n == 1
	}

	var cursor uint32
	q := generated.New(db).WithTx(tx)
	const ticks = 10
	joined := dbtx.WithTx(ctx, tx)
	for tick := 1; tick <= ticks && !attached(); tick++ {
		backfillAlbumPhotoObjects(joined, db, q, srv.Storage, &cursor)
	}
	require.True(t, attached(), "the readable photo is reached within %d ticks of passing the unreadable ones", ticks)
}
