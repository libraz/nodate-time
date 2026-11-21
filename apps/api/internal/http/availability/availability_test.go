package availability

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/stretchr/testify/require"
)

func TestAvailabilityUsesHalfOpenIntervalsAndHardPrecedence(t *testing.T) {
	base := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	slot := func(a, b int) Slot {
		return Slot{base.Add(time.Hour * time.Duration(a)), base.Add(time.Hour * time.Duration(b))}
	}
	var busy busyIntervals
	busy.add("busy", "fixed", slot(5, 6))
	busy.add("oof", "", slot(3, 5))
	busy.add("busy", "fixed", slot(2, 2))
	busy.add("free", "fixed", slot(0, 10))
	busy.add("tentative", "fixed", slot(2, 7))
	busy.add("oof", "conditional", slot(7, 8))
	busy.add("busy", "negotiable", slot(8, 9))
	require.Equal(t, []State{Hard, Open, Soft, Open, Hard, Soft, Soft, Soft},
		busy.states([]Slot{slot(4, 5), slot(0, 2), slot(2, 3), slot(9, 10), slot(5, 6), slot(6, 7), slot(7, 8), slot(8, 9)}))
}

func TestAllDaySeriesEndsAtLocalMidnightAcrossSpringForward(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	day := func(d int) time.Time { return time.Date(2026, time.March, d, 0, 0, 0, 0, zone) }
	rule := json.RawMessage(`{"freq":"weekly","count":2}`)
	parent := generated.CalendarEvent{
		ID: 1, Timezone: zone.String(), ShowAs: "busy", Flexibility: "fixed", RecurrenceRule: &rule,
		StartAt: sql.NullTime{Time: day(1).UTC(), Valid: true}, EndAt: sql.NullTime{Time: day(2).UTC(), Valid: true},
	}
	scan := calendarScan{window: Slot{day(8), day(10)}}
	require.NoError(t, scan.series(context.Background(), parent, nil, map[uint32]bool{1: true}))
	require.Equal(t, []State{Hard, Open}, scan.busy.states([]Slot{
		{day(9).Add(-time.Minute), day(9)}, {day(9), day(9).Add(time.Minute)},
	}))
}

func TestAvailabilityFailsClosedForUncheckedRecurrence(t *testing.T) {
	start := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	rule := json.RawMessage(`{"freq":"hourly"}`)
	parent := generated.CalendarEvent{ID: 1, RecurrenceRule: &rule, StartAt: sql.NullTime{Time: start, Valid: true}}
	scan := calendarScan{window: Slot{start, start.Add(time.Hour)}}
	require.ErrorIs(t, scan.series(context.Background(), parent, nil, nil), ErrLimit)
	rule = json.RawMessage(`{"freq":"daily"}`)
	parent.EndAt = sql.NullTime{Time: start.AddDate(35, 0, 0), Valid: true}
	require.ErrorIs(t, scan.series(context.Background(), parent, nil, nil), ErrLimit)
}

func TestAllDayRowOccupiesLocalDaysOfTheOwnersZone(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	scan := calendarScan{zones: map[uint32]*time.Location{7: zone}}
	event := generated.CalendarEvent{AllDay: true, OwnerUserID: 7}
	// The 5th, stored as UTC midnights with an exclusive end.
	slot, err := scan.slotOf(context.Background(), event,
		time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 5, 0, 0, 0, 0, zone), slot.Start)
	require.Equal(t, time.Date(2026, 8, 6, 0, 0, 0, 0, zone), slot.End)
}
