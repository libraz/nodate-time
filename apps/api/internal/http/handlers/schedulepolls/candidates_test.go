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

func TestUpcomingSlotsDropsStartedCandidates(t *testing.T) {
	now := time.Date(2026, 11, 22, 6, 0, 0, 0, time.UTC)
	at := func(hour, minute int) availability.Slot {
		start := time.Date(2026, 11, 22, hour, minute, 0, 0, time.UTC)
		return availability.Slot{Start: start, End: start.Add(time.Hour)}
	}
	slots := upcomingSlots([]availability.Slot{at(5, 30), at(6, 0), at(6, 15)}, now)
	require.Equal(t, []availability.Slot{at(6, 15)}, slots)
	require.Empty(t, upcomingSlots([]availability.Slot{at(5, 0)}, now))
}
