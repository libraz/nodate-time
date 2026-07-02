package audit

import "testing"

func TestActivityCursorRoundTrip(t *testing.T) {
	const id uint64 = 12345
	cursor := encodeActivityCursor(id)
	if cursor == "" {
		t.Fatal("cursor is empty")
	}
	got, err := decodeActivityCursor(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if got != id {
		t.Fatalf("cursor id = %d, want %d", got, id)
	}
}

func TestActivityCursorRejectsInvalidInput(t *testing.T) {
	for _, cursor := range []string{"not-base64!", encodeActivityCursor(0)} {
		if _, err := decodeActivityCursor(cursor); err == nil {
			t.Fatalf("decodeActivityCursor(%q) succeeded, want error", cursor)
		}
	}
}
