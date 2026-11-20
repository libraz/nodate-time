package e2e

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// albumRows writes raw rows the way another product sharing the database
// sees them, inside a transaction the test rolls back.
type albumRows struct {
	t   *testing.T
	ctx context.Context
	tx  *sql.Tx
	tag string
}

func newAlbumRows(t *testing.T) *albumRows {
	t.Helper()
	tx, err := testDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return &albumRows{t: t, ctx: t.Context(), tx: tx, tag: uuid.NewString()}
}

func (r *albumRows) insert(query string, args ...any) int64 {
	r.t.Helper()
	res, err := r.tx.ExecContext(r.ctx, query, args...)
	require.NoError(r.t, err)
	id, err := res.LastInsertId()
	require.NoError(r.t, err)
	return id
}

func (r *albumRows) run(query string, args ...any) {
	r.t.Helper()
	_, err := r.tx.ExecContext(r.ctx, query, args...)
	require.NoError(r.t, err)
}

func (r *albumRows) count(query string, args ...any) int {
	r.t.Helper()
	var n int
	require.NoError(r.t, r.tx.QueryRowContext(r.ctx, query, args...).Scan(&n))
	return n
}

func (r *albumRows) key(name string) string { return "albums/" + r.tag + "/" + name }

func (r *albumRows) workspace() int64 {
	return r.insert(`INSERT INTO workspaces (public_id, slug, name) VALUES (UUID_TO_BIN(UUID()), ?, 'Album lifecycle')`,
		"album-"+uuid.NewString())
}

func (r *albumRows) user() int64 {
	return r.insert(`INSERT INTO users (public_id, email, display_name) VALUES (UUID_TO_BIN(UUID()), ?, 'Album user')`,
		"album-"+uuid.NewString()+"@example.com")
}

// calendar takes owner 0 for a calendar nobody owns.
func (r *albumRows) calendar(workspaceID, ownerID int64) int64 {
	var owner any
	if ownerID != 0 {
		owner = ownerID
	}
	return r.insert(`INSERT INTO calendars (public_id, workspace_id, name, owner_user_id) VALUES (UUID_TO_BIN(UUID()), ?, 'Album', ?)`,
		workspaceID, owner)
}

func (r *albumRows) object(workspaceID int64, key string, refs int) int64 {
	return r.insert(`INSERT INTO storage_objects
		(public_id, workspace_id, sha256, byte_size, content_type, storage_key, ref_count, uploaded_at)
		VALUES (UUID_TO_BIN(UUID()), ?, UNHEX(SHA2(?, 256)), 1, 'image/jpeg', ?, ?, CURRENT_TIMESTAMP(3))`,
		workspaceID, key, key, refs)
}

// photo takes object and thumbnail 0 for none.
func (r *albumRows) photo(workspaceID, calendarID, uploaderID int64, key string, object, thumbnail int64) int64 {
	nullable := func(id int64) any {
		if id == 0 {
			return nil
		}
		return id
	}
	return r.insert(`INSERT INTO album_photos
		(public_id, workspace_id, calendar_id, uploaded_by_user_id, storage_key, storage_object_id, thumbnail_object_id)
		VALUES (UUID_TO_BIN(UUID()), ?, ?, ?, ?, ?, ?)`,
		workspaceID, calendarID, uploaderID, key, nullable(object), nullable(thumbnail))
}

func (r *albumRows) queued(key string) bool {
	return r.count(`SELECT COUNT(*) FROM storage_blob_removals WHERE storage_key = ?`, key) == 1
}

func (r *albumRows) refCount(objectID int64) int {
	return r.count(`SELECT ref_count FROM storage_objects WHERE id = ?`, objectID)
}

// Flow deletes a workspace without naming album_photos. Every photo key, its
// .thumb, and every object key the photos point at must be queued, including
// a photo deduplicated onto an object under another key.
func TestAlbumLifecycleWorkspaceDeleteQueuesEveryPhotoKey(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	r := newAlbumRows(t)

	ws := r.workspace()
	uploader := r.user()
	cal := r.calendar(ws, 0)
	shared := r.object(ws, r.key("shared"), 2)
	thumb := r.object(ws, r.key("shared.thumb"), 1)
	first := r.photo(ws, cal, uploader, r.key("shared"), shared, thumb)
	second := r.photo(ws, cal, uploader, r.key("dedup"), shared, 0)

	r.run(`DELETE FROM workspaces WHERE id = ?`, ws)

	for _, key := range []string{r.key("shared"), r.key("shared.thumb"), r.key("dedup"), r.key("dedup.thumb")} {
		require.True(t, r.queued(key), "%s is queued for removal", key)
	}
	require.Zero(t, r.count(`SELECT COUNT(*) FROM album_photos WHERE id IN (?, ?)`, first, second))
	require.Zero(t, r.count(`SELECT COUNT(*) FROM storage_objects WHERE id IN (?, ?)`, shared, thumb))
}

// Either product disables a calendar with a bare UPDATE; its live photos are
// retired with it and keep their references for the sweep to release.
func TestAlbumLifecycleCalendarDisableRetiresPhotos(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	r := newAlbumRows(t)

	ws := r.workspace()
	uploader := r.user()
	cal := r.calendar(ws, 0)
	other := r.calendar(ws, 0)
	object := r.object(ws, r.key("live"), 1)
	live := r.photo(ws, cal, uploader, r.key("live"), object, 0)
	untouched := r.photo(ws, other, uploader, r.key("other"), 0, 0)

	r.run(`UPDATE calendars SET enabled = FALSE WHERE id = ?`, cal)

	require.Equal(t, 1, r.count(`SELECT COUNT(*) FROM album_photos
		WHERE id = ? AND enabled = FALSE AND retired_at IS NOT NULL`, live))
	require.Equal(t, 1, r.count(`SELECT COUNT(*) FROM album_photos
		WHERE id = ? AND enabled = TRUE AND retired_at IS NULL`, untouched))
	require.Equal(t, 1, r.refCount(object), "the reference stays held until the sweep")
}

// A hard-deleted user takes their uploads and every photo and cover on the
// calendars they own. Every reference those rows held is released exactly,
// and only keys no storage object holds are queued.
func TestAlbumLifecycleUserDeleteReleasesEveryReference(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	r := newAlbumRows(t)

	ws := r.workspace()
	gone := r.user()
	stays := r.user()
	ownedA := r.calendar(ws, gone)
	ownedB := r.calendar(ws, gone)
	shared := r.calendar(ws, 0)

	// A dedup pair the user uploaded to a calendar they do not own.
	dedup := r.object(ws, r.key("dedup"), 2)
	pairA := r.photo(ws, shared, gone, r.key("dedup"), dedup, 0)
	pairB := r.photo(ws, shared, gone, r.key("dedup-copy"), dedup, 0)
	// Another user's photo on a calendar the user owns.
	theirs := r.object(ws, r.key("theirs"), 1)
	theirsThumb := r.object(ws, r.key("theirs.thumb"), 1)
	onOwned := r.photo(ws, ownedA, stays, r.key("theirs"), theirs, theirsThumb)
	// Another user's photo elsewhere is untouched.
	kept := r.object(ws, r.key("kept"), 1)
	keptPhoto := r.photo(ws, shared, stays, r.key("kept"), kept, 0)
	// One cover object on both owned calendars and on a calendar that stays.
	cover := r.object(ws, "calendar-covers/"+r.tag, 3)
	for _, cal := range []int64{ownedA, ownedB, shared} {
		r.run(`INSERT INTO calendar_cover_images (calendar_id, storage_object_id) VALUES (?, ?)`, cal, cover)
	}

	r.run(`DELETE FROM users WHERE id = ?`, gone)

	require.Zero(t, r.refCount(dedup), "the dedup pair releases both references")
	require.Zero(t, r.refCount(theirs))
	require.Zero(t, r.refCount(theirsThumb))
	require.Equal(t, 1, r.refCount(kept))
	require.Equal(t, 1, r.refCount(cover), "two owned calendars release the shared cover twice")

	require.Zero(t, r.count(`SELECT COUNT(*) FROM album_photos WHERE id IN (?, ?, ?)`, pairA, pairB, onOwned))
	require.Equal(t, 1, r.count(`SELECT COUNT(*) FROM album_photos WHERE id = ?`, keptPhoto))
	require.Zero(t, r.count(`SELECT COUNT(*) FROM calendar_cover_images WHERE calendar_id IN (?, ?)`, ownedA, ownedB))
	require.Equal(t, 1, r.count(`SELECT COUNT(*) FROM calendar_cover_images WHERE calendar_id = ?`, shared))

	for _, key := range []string{r.key("dedup.thumb"), r.key("dedup-copy"), r.key("dedup-copy.thumb")} {
		require.True(t, r.queued(key), "%s is held by no object and is queued", key)
	}
	for _, key := range []string{r.key("dedup"), r.key("theirs"), r.key("theirs.thumb"), r.key("kept")} {
		require.False(t, r.queued(key), "%s is held by an object and is not queued", key)
	}
}
