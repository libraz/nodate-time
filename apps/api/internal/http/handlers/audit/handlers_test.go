package audit

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
)

// A cursor is a value handed to a client, so what matters is what it says
// rather than that it round-trips: it names the row and nothing else.
func TestActivityCursorNamesTheRowAndNothingElse(t *testing.T) {
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new uuid: %v", err)
	}
	cursor := encodeActivityCursor(id[:])
	if cursor != id.String() {
		t.Fatalf("cursor = %q, want %q", cursor, id.String())
	}
	if _, err := uuid.Parse(cursor); err != nil {
		t.Fatalf("cursor is not a public id: %v", err)
	}
}

// The internal id is a single deployment-wide sequence. A cursor that is a
// decimal number, however it is wrapped, tells its holder how much the whole
// instance has written.
func TestActivityCursorCarriesNoInternalNumber(t *testing.T) {
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new uuid: %v", err)
	}
	cursor := encodeActivityCursor(id[:])
	if _, err := strconv.ParseUint(cursor, 10, 64); err == nil {
		t.Fatalf("cursor %q reads as a number", cursor)
	}
}
