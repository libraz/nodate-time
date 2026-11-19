package schedulepolls

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
)

const (
	candidateInterval = 15 * time.Minute
	candidateSlotCap  = 4096
	candidateDayCap   = 366
)

// generateCandidateSlots interprets each day's search window in the requested
// zone. It steps through real time so a clock change cannot produce impossible
// candidates or collapse two distinct instants in a repeated hour.
func generateCandidateSlots(timezone, fromDate, toDate, startTime, endTime string, durationMinutes int) ([]availability.Slot, error) {
	zone, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" || timezone == "Local" {
		return nil, availability.ErrInvalid
	}
	first, err := time.Parse(time.DateOnly, fromDate)
	if err != nil || first.Year() < 1 {
		return nil, availability.ErrInvalid
	}
	last, err := time.Parse(time.DateOnly, toDate)
	if err != nil || last.Before(first) {
		return nil, availability.ErrInvalid
	}
	if last.After(first.AddDate(0, 0, candidateDayCap-1)) {
		return nil, availability.ErrLimit
	}
	start, err := time.Parse("15:04", startTime)
	if err != nil {
		return nil, availability.ErrInvalid
	}
	end, err := time.Parse("15:04", endTime)
	if err != nil || start.Equal(end) || durationMinutes < 1 || durationMinutes > 24*60 {
		return nil, availability.ErrInvalid
	}
	duration := time.Duration(durationMinutes) * time.Minute
	overnight := end.Before(start)

	slots := make([]availability.Slot, 0)
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		endDay := day
		if overnight {
			endDay = day.AddDate(0, 0, 1)
		}
		windowStart, startOK := wallClockBoundary(day, start, zone)
		windowEnd, endOK := wallClockBoundary(endDay, end, zone)
		if !startOK || !endOK || !windowEnd.After(windowStart) {
			return nil, availability.ErrInvalid
		}
		lastStart := windowEnd.Add(-duration)
		for candidate := windowStart; !candidate.After(lastStart); candidate = candidate.Add(candidateInterval) {
			if len(slots) == candidateSlotCap {
				return nil, availability.ErrLimit
			}
			slots = append(slots, availability.Slot{
				Start: candidate.UTC(),
				End:   candidate.Add(duration).UTC(),
			})
		}
	}
	return slots, nil
}

func wallClockBoundary(day, clock time.Time, zone *time.Location) (time.Time, bool) {
	year, month, date := day.Date()
	wallTime := time.Date(year, month, date, clock.Hour(), clock.Minute(), 0, 0, zone)
	return wallTime, year <= 9999 &&
		wallTime.Year() == year && wallTime.Month() == month && wallTime.Day() == date &&
		wallTime.Hour() == clock.Hour() && wallTime.Minute() == clock.Minute()
}
