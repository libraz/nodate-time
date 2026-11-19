package schedulepolls

import (
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/stretchr/testify/require"
)

func TestGenerateCandidateSlotsAcrossMidnight(t *testing.T) {
	slots, err := generateCandidateSlots("Asia/Tokyo", "2026-11-20", "2026-11-21", "23:45", "00:30", 30)
	require.NoError(t, err)
	require.Len(t, slots, 4)
	require.Equal(t, "2026-11-20T14:45:00Z", slots[0].Start.Format(time.RFC3339))
	require.Equal(t, "2026-11-20T15:30:00Z", slots[1].End.Format(time.RFC3339))
	require.Equal(t, "2026-11-21T14:45:00Z", slots[2].Start.Format(time.RFC3339))
}

func TestGenerateCandidateSlotsAcrossClockChanges(t *testing.T) {
	const zone = "America/New_York"
	_, err := generateCandidateSlots(zone, "2026-03-08", "2026-03-08", "02:15", "04:00", 15)
	require.ErrorIs(t, err, availability.ErrInvalid)

	slots, err := generateCandidateSlots(zone, "2026-11-01", "2026-11-01", "00:45", "02:15", 15)
	require.NoError(t, err)
	require.Len(t, slots, 10)
	require.Equal(t, "2026-11-01T05:00:00Z", slots[1].Start.Format(time.RFC3339))
	require.Equal(t, "2026-11-01T06:00:00Z", slots[5].Start.Format(time.RFC3339))
}

func TestGenerateCandidateSlotsFailsRatherThanReturningTruncatedAvailability(t *testing.T) {
	_, err := generateCandidateSlots("UTC", "2026-01-01", "2026-12-31", "08:00", "18:00", 60)
	require.ErrorIs(t, err, availability.ErrLimit)
}
