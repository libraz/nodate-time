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
}

func (f *fakeObjectDeleter) DeleteObject(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
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

func TestDeleteObjectsKeepsReferenceWhenRowDeletionFails(t *testing.T) {
	storage := &fakeObjectDeleter{failed: map[string]error{}}
	rowCalls := 0

	deleteObjects(context.Background(), storage, []string{"retry"}, func(_ context.Context, _ string) error {
		rowCalls++
		return errors.New("database unavailable")
	})

	assert.Equal(t, 1, rowCalls)
}
