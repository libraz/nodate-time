package cleanup

import (
	"context"
	"errors"
	"testing"

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
