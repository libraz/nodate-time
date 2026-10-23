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
