package events

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/stretchr/testify/require"
)

// TestAttendeeReadFailureIsNotAnEmptyGuestList pins the difference between
// "this event has no attendees" and "the attendees could not be read".
//
// The participant list is also the write format for a full-replace update, so
// a response that reports an unreadable list as empty hands the client an
// authoritative-looking emptiness. The next save sends it back and every
// attendee is removed -- a read failure turned into a write.
func TestAttendeeReadFailureIsNotAnEmptyGuestList(t *testing.T) {
	// A closed handle fails every query without needing a database at all,
	// which is what makes the failure branch reachable at will.
	db, err := sql.Open("mysql", "nobody:nobody@tcp(127.0.0.1:1)/nowhere")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	deps := Deps{Queries: generated.New(db)}

	ids, attendees, err := eventAttendees(context.Background(), deps, 1)
	require.Error(t, err, "an unreadable attendee list must be reported, not rendered as empty")
	require.Nil(t, ids)
	require.Nil(t, attendees)

	// And the response filler refuses to write the emptiness into a response.
	resp := EventResponse{Participants: []string{"someone"}}
	require.Error(t, setAttendees(context.Background(), deps, &resp, 1))
	require.Equal(t, []string{"someone"}, resp.Participants,
		"a failed read must leave the response untouched rather than blanking it")
}
