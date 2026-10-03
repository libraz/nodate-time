package daterange

import (
	"slices"
	"testing"
	"time"
)

// The cap keeps the earliest items whatever order they arrive in, and says
// when it dropped something.
func TestEarliestKeepsTheHeadOfTheWindow(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	e := NewEarliest[int](3)
	for _, day := range []int{9, 2, 7, 1, 8, 3} {
		e.Offer(base.AddDate(0, 0, day), day)
	}
	if got := e.Items(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("Items() = %v, want [1 2 3]", got)
	}
	if !e.Truncated() {
		t.Fatal("dropping items must be reported")
	}

	under := NewEarliest[string](3)
	under.Offer(base, "b")
	under.Offer(base, "a")
	if got := under.Items(); !slices.Equal(got, []string{"b", "a"}) || under.Truncated() {
		t.Fatalf("under the cap: %v truncated=%v; want offer order on ties and no truncation", got, under.Truncated())
	}
}
