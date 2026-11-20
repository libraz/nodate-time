package cleanup

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeObjectDeleter struct {
	failed  map[string]error
	deleted []string
	// events records object and row steps in the order they happen.
	events *[]string
}

func (f *fakeObjectDeleter) DeleteObject(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	if f.events != nil {
		*f.events = append(*f.events, "object:"+key)
	}
	return f.failed[key]
}

func TestDeleteObjectsRetainsRowsWhenStorageDeletionFails(t *testing.T) {
	storage := &fakeObjectDeleter{failed: map[string]error{"keep": errors.New("storage unavailable")}}
	deletedRows := make([]string, 0, 1)

	deleteObjects(context.Background(), storage, []string{"remove", "keep"}, func(_ context.Context, key string) error {
		deletedRows = append(deletedRows, key)
		return nil
	})

	require.Equal(t, []string{"remove", "keep"}, storage.deleted)
	assert.Equal(t, []string{"remove"}, deletedRows)
}

func TestDeleteObjectsDeletesTheObjectBeforeTheRow(t *testing.T) {
	var events []string
	storage := &fakeObjectDeleter{events: &events}

	deleteObjects(context.Background(), storage, []string{"a", "b"}, func(_ context.Context, key string) error {
		events = append(events, "row:"+key)
		return nil
	})

	assert.Equal(t, []string{"object:a", "row:a", "object:b", "row:b"}, events)
}

func TestDeleteObjectsKeepsReferenceWhenRowDeletionFails(t *testing.T) {
	var events []string
	storage := &fakeObjectDeleter{events: &events}
	rowCalls := 0
	failRow := func(_ context.Context, key string) error {
		rowCalls++
		events = append(events, "row:"+key)
		return errors.New("database unavailable")
	}

	deleteObjects(context.Background(), storage, []string{"retry"}, failRow)
	assert.Equal(t, []string{"object:retry", "row:retry"}, events, "the object goes first, so a failed row delete loses nothing")

	// The row survived, so the next tick lists it again and deletes the
	// already-gone object a second time without complaint.
	deleteObjects(context.Background(), storage, []string{"retry"}, failRow)
	assert.Equal(t, 2, rowCalls)
	assert.Equal(t, []string{"retry", "retry"}, storage.deleted)
}

// A listing page full of objects the sweep cannot remove must not keep the
// sweep from reaching the collectable one behind it: each page starts after
// the last id seen, whatever reclaim did with it.
func TestPageByIDReachesRowsBehindAFullPageItCannotRemove(t *testing.T) {
	const stuck = storageSweepBatchSize + 20
	removed := map[uint32]bool{}
	list := func(afterID uint32) ([]candidate, error) {
		var page []candidate
		for id := afterID + 1; id <= stuck+1 && len(page) < storageSweepBatchSize; id++ {
			if !removed[id] {
				page = append(page, candidate{id: id})
			}
		}
		return page, nil
	}
	pageByID(context.Background(), "test", list, func(c candidate) {
		if c.id == stuck+1 {
			removed[c.id] = true
		}
	})
	require.True(t, removed[stuck+1], "the collectable row behind the stuck ones is reached")
}

// A listing error ends the sweep instead of retrying the same page.
func TestPageByIDStopsOnAListingError(t *testing.T) {
	calls := 0
	pageByID(context.Background(), "test", func(uint32) ([]candidate, error) {
		calls++
		return nil, errors.New("down")
	}, func(candidate) { t.Fatal("nothing listed, nothing reclaimed") })
	require.Equal(t, 1, calls)
}

// A queued key a confirmed or still-referenced storage object holds keeps its
// blob; a discarded reservation holds nothing. The queue row goes either way. Written in a transaction
// that is rolled back, against a fake store, so no other test's blobs move.
func TestDrainBlobRemovalsKeepsKeysAnObjectHolds(t *testing.T) {
	if os.Getenv("TC_TEST_INTEGRATION") == "" {
		t.Skip("set TC_TEST_INTEGRATION=1 to run")
	}
	ctx := context.Background()
	db := helpers.OpenTestDB(t)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	id, err := uuid.NewV7()
	require.NoError(t, err)
	ws, err := tx.ExecContext(ctx, `INSERT INTO workspaces (public_id, slug, name) VALUES (?, ?, ?)`,
		id[:], "drain-"+id.String(), "Drain test")
	require.NoError(t, err)
	workspaceID, err := ws.LastInsertId()
	require.NoError(t, err)

	prefix := "drain-test/" + id.String() + "/"
	confirmed, reserved := prefix+"confirmed", prefix+"reserved"
	discarded, unheld := prefix+"discarded", prefix+"unheld"
	_, err = tx.ExecContext(ctx, `INSERT INTO storage_objects
		(public_id, workspace_id, sha256, byte_size, content_type, storage_key, ref_count, uploaded_at)
		VALUES (UUID_TO_BIN(UUID()), ?, UNHEX(SHA2(?, 256)), 1, 'image/png', ?, 0, CURRENT_TIMESTAMP(3)),
		       (UUID_TO_BIN(UUID()), ?, UNHEX(SHA2(?, 256)), 1, 'image/png', ?, 1, NULL),
		       (UUID_TO_BIN(UUID()), ?, UNHEX(SHA2(?, 256)), 1, 'image/png', ?, 0, NULL)`,
		workspaceID, confirmed, confirmed, workspaceID, reserved, reserved,
		workspaceID, discarded, discarded)
	require.NoError(t, err)
	for _, key := range []string{confirmed, reserved, discarded, unheld} {
		_, err = tx.ExecContext(ctx, `INSERT INTO storage_blob_removals (storage_key) VALUES (?)`, key)
		require.NoError(t, err)
	}

	store := &fakeObjectDeleter{}
	drainBlobRemovals(ctx, generated.New(db).WithTx(tx), store)

	assert.True(t, slices.Contains(store.deleted, unheld), "a key nothing holds is removed")
	assert.False(t, slices.Contains(store.deleted, confirmed), "a confirmed object's key is kept")
	assert.False(t, slices.Contains(store.deleted, reserved), "a referenced reservation's key is kept")
	assert.True(t, slices.Contains(store.deleted, discarded), "an unreferenced reservation holds nothing")
	var queued int
	require.NoError(t, tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM storage_blob_removals WHERE storage_key IN (?, ?, ?, ?)`,
		confirmed, reserved, discarded, unheld).Scan(&queued))
	assert.Zero(t, queued, "every queue row is dropped")
}
