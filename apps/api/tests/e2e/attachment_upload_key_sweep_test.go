package e2e

import (
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A reservation for bytes somebody else already stores uploads its own copy to
// a key only it names. When the reservation is abandoned and swept, that copy
// goes with it: nothing else tracks it, so it would otherwise stay in the
// bucket for good.
func TestAbandonedReservationTakesItsOwnUploadedCopyWithIt(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("abandoned copy")
	owner := helpers.NewTenant(t, testServerURL)
	attachEventWithAttachment(t, owner, "Owns the file", body)

	other := helpers.NewTenant(t, testServerURL)
	_, _, pres := reserveAttachment(t, other, body)
	require.NotEmpty(t, pres.UploadURL, "a caller who cannot read the stored file has to send it")
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)

	copyKey := "workspace/" + testWorkspacePublicID + "/uploads/" + pres.AttachmentID
	_, exists, err := getTestStorage().StatObject(testCtx(), copyKey)
	require.NoError(t, err)
	require.True(t, exists, "the upload landed at the reservation's own key")

	_, err = testDB.Exec(
		`UPDATE calendar_event_attachments SET created_at = ? WHERE public_id = UUID_TO_BIN(?)`,
		time.Now().Add(-30*24*time.Hour), pres.AttachmentID)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM calendar_event_attachments WHERE public_id = UUID_TO_BIN(?)`, pres.AttachmentID).Scan(&rows))
	require.Zero(t, rows, "the abandoned reservation is swept")
	_, exists, err = getTestStorage().StatObject(testCtx(), copyKey)
	require.NoError(t, err)
	require.False(t, exists, "and so is the copy it uploaded")
}
