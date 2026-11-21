package events

import (
	"encoding/json"
	"testing"
	"time"

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestAllDayBounds(t *testing.T) {
	cases := []struct {
		name, start, end string
		milestone        bool
		spec             *apierrors.Spec
	}{
		{"one day", "2026-08-05", "2026-08-06", false, nil},
		{"upper bound", "9999-12-30", "9999-12-31", false, nil},
		{"leap day", "2028-02-29", "2028-03-01", false, nil},
		{"no such day", "2026-02-30", "2026-03-02", false, apierrors.EventDateInvalid},
		{"year zero", "0000-01-01", "0000-01-02", false, apierrors.EventDateInvalid},
		{"before year 1000", "0999-12-31", "1000-01-01", false, apierrors.EventDateInvalid},
		{"end equals start", "2026-08-05", "2026-08-05", false, apierrors.EventDateRangeInvalid},
		{"end before start", "2026-08-05", "2026-08-04", false, apierrors.EventDateRangeInvalid},
		{"milestone is a point", "2026-08-05", "2026-08-05", true, nil},
		{"milestone cannot end early", "2026-08-05", "2026-08-04", true, apierrors.EventDateRangeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, spec := allDayBounds(c.start, c.end, c.milestone)
			require.Equal(t, c.spec, spec)
			if spec == nil {
				require.Equal(t, time.UTC, start.Location())
				require.Equal(t, c.start, start.Format(dateLayout))
				require.Equal(t, c.end, end.Format(dateLayout))
			}
		})
	}
}

func TestUntilFormFollowsTheSeriesKind(t *testing.T) {
	rule := func(until string) *json.RawMessage {
		raw := json.RawMessage(`{"freq":"daily","interval":1,"until":"` + until + `"}`)
		return &raw
	}
	require.Nil(t, checkUntilForm(rule("2026-09-05"), true))
	require.Nil(t, checkUntilForm(rule("2026-09-05T10:00:00Z"), false))
	require.Equal(t, apierrors.EventOccurrenceForm, checkUntilForm(rule("2026-09-05T10:00:00Z"), true))
	require.Equal(t, apierrors.EventOccurrenceForm, checkUntilForm(rule("2026-09-05"), false))
	require.Nil(t, checkUntilForm(nil, true))
	noUntil := json.RawMessage(`{"freq":"daily","interval":1,"count":3}`)
	require.Nil(t, checkUntilForm(&noUntil, true))
}
