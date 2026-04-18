package recurrence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s)
	return t
}

func ptr(s string) *string { return &s }

func TestExpandDaily(t *testing.T) {
	rule := &Rule{Freq: "daily", Interval: 1}
	start := d("2026-04-01 10:00")
	end := d("2026-04-01 11:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-04-05 00:00"))
	require.Len(t, results, 4)
	assert.Equal(t, d("2026-04-01 10:00"), results[0].StartAt)
	assert.Equal(t, d("2026-04-04 10:00"), results[3].StartAt)
	assert.Equal(t, d("2026-04-04 11:00"), results[3].EndAt)
}

func TestExpandDailyWithInterval(t *testing.T) {
	rule := &Rule{Freq: "daily", Interval: 3}
	start := d("2026-04-01 09:00")
	end := d("2026-04-01 10:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-04-15 00:00"))
	require.Len(t, results, 5) // 1, 4, 7, 10, 13
	assert.Equal(t, d("2026-04-01 09:00"), results[0].StartAt)
	assert.Equal(t, d("2026-04-04 09:00"), results[1].StartAt)
	assert.Equal(t, d("2026-04-13 09:00"), results[4].StartAt)
}

func TestExpandWeekly(t *testing.T) {
	rule := &Rule{Freq: "weekly", Interval: 1, ByDay: []string{"FR"}}
	start := d("2026-04-03 15:00") // Friday
	end := d("2026-04-03 16:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-05-01 00:00"))
	require.Len(t, results, 4) // Apr 3, 10, 17, 24
	assert.Equal(t, d("2026-04-03 15:00"), results[0].StartAt)
	assert.Equal(t, d("2026-04-10 15:00"), results[1].StartAt)
	assert.Equal(t, d("2026-04-17 15:00"), results[2].StartAt)
	assert.Equal(t, d("2026-04-24 15:00"), results[3].StartAt)
}

func TestExpandWeekdays(t *testing.T) {
	rule := &Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO", "TU", "WE", "TH", "FR"}}
	start := d("2026-04-06 09:00") // Monday
	end := d("2026-04-06 17:00")

	results := Expand(rule, start, end, d("2026-04-06 00:00"), d("2026-04-13 00:00"))
	require.Len(t, results, 5) // Mon-Fri of that week
	assert.Equal(t, time.Monday, results[0].StartAt.Weekday())
	assert.Equal(t, time.Friday, results[4].StartAt.Weekday())
}

func TestExpandMonthlyByDate(t *testing.T) {
	rule := &Rule{Freq: "monthly", Interval: 1, ByMonthDay: 15}
	start := d("2026-01-15 10:00")
	end := d("2026-01-15 11:00")

	results := Expand(rule, start, end, d("2026-01-01 00:00"), d("2026-07-01 00:00"))
	require.Len(t, results, 6) // Jan-Jun 15th
	assert.Equal(t, 15, results[0].StartAt.Day())
	assert.Equal(t, time.June, results[5].StartAt.Month())
}

func TestExpandMonthlyByDateClamp(t *testing.T) {
	// Month-end clamping: 31st should clamp to 28/29 in Feb
	rule := &Rule{Freq: "monthly", Interval: 1, ByMonthDay: 31}
	start := d("2026-01-31 10:00")
	end := d("2026-01-31 11:00")

	results := Expand(rule, start, end, d("2026-01-01 00:00"), d("2026-05-01 00:00"))
	require.Len(t, results, 4) // Jan 31, Feb 28, Mar 31, Apr 30
	assert.Equal(t, 31, results[0].StartAt.Day())
	assert.Equal(t, 28, results[1].StartAt.Day()) // Feb 2026
	assert.Equal(t, 31, results[2].StartAt.Day())
	assert.Equal(t, 30, results[3].StartAt.Day()) // Apr
}

func TestExpandMonthlyByNthWeekday(t *testing.T) {
	// 3rd Sunday of month
	rule := &Rule{Freq: "monthly", Interval: 1, BySetPos: 3, ByDay: []string{"SU"}}
	start := d("2026-04-19 10:00") // 3rd Sunday of April
	end := d("2026-04-19 11:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-07-01 00:00"))
	require.Len(t, results, 3) // Apr, May, Jun
	assert.Equal(t, time.Sunday, results[0].StartAt.Weekday())
	assert.Equal(t, 19, results[0].StartAt.Day()) // Apr 19
	assert.Equal(t, 17, results[1].StartAt.Day()) // May 17
	assert.Equal(t, 21, results[2].StartAt.Day()) // Jun 21
}

func TestExpandYearly(t *testing.T) {
	rule := &Rule{Freq: "yearly", Interval: 1}
	start := d("2026-04-19 00:00")
	end := d("2026-04-20 00:00")

	results := Expand(rule, start, end, d("2026-01-01 00:00"), d("2030-01-01 00:00"))
	require.Len(t, results, 4) // 2026, 2027, 2028, 2029
	assert.Equal(t, 2026, results[0].StartAt.Year())
	assert.Equal(t, 2029, results[3].StartAt.Year())
}

func TestExpandWithCount(t *testing.T) {
	rule := &Rule{Freq: "daily", Interval: 1, Count: 3}
	start := d("2026-04-01 10:00")
	end := d("2026-04-01 11:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-12-31 00:00"))
	require.Len(t, results, 3)
}

func TestExpandWithUntil(t *testing.T) {
	until := "2026-04-10"
	rule := &Rule{Freq: "daily", Interval: 1, Until: &until}
	start := d("2026-04-01 10:00")
	end := d("2026-04-01 11:00")

	results := Expand(rule, start, end, d("2026-04-01 00:00"), d("2026-12-31 00:00"))
	require.Len(t, results, 10) // Apr 1-10
}

func TestExpandWindowFiltering(t *testing.T) {
	rule := &Rule{Freq: "daily", Interval: 1}
	start := d("2026-04-01 10:00")
	end := d("2026-04-01 11:00")

	// Query window starts on Apr 5
	results := Expand(rule, start, end, d("2026-04-05 00:00"), d("2026-04-08 00:00"))
	require.Len(t, results, 3) // Apr 5, 6, 7
	assert.Equal(t, d("2026-04-05 10:00"), results[0].StartAt)
}

func TestParseRule(t *testing.T) {
	data := json.RawMessage(`{"freq":"weekly","interval":2,"byDay":["MO","WE"]}`)
	r := ParseRule(data)
	require.NotNil(t, r)
	assert.Equal(t, "weekly", r.Freq)
	assert.Equal(t, 2, r.Interval)
	assert.Equal(t, []string{"MO", "WE"}, r.ByDay)
}

func TestParseRuleNull(t *testing.T) {
	assert.Nil(t, ParseRule(nil))
	assert.Nil(t, ParseRule(json.RawMessage("null")))
}

func TestComputeEnd(t *testing.T) {
	t.Run("with until", func(t *testing.T) {
		until := "2026-06-01"
		rule := &Rule{Freq: "daily", Interval: 1, Until: &until}
		end := ComputeEnd(rule, d("2026-04-01 00:00"))
		assert.Equal(t, 2026, end.Year())
		assert.Equal(t, time.June, end.Month())
	})

	t.Run("infinite", func(t *testing.T) {
		rule := &Rule{Freq: "daily", Interval: 1}
		end := ComputeEnd(rule, d("2026-04-01 00:00"))
		assert.Equal(t, 2099, end.Year())
	})

	t.Run("nil rule", func(t *testing.T) {
		end := ComputeEnd(nil, d("2026-04-01 10:00"))
		assert.Equal(t, d("2026-04-01 10:00"), end)
	})
}
