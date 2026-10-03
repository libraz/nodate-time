package daterange

import (
	"container/heap"
	"sort"
	"time"
)

// Earliest keeps the n earliest-starting items offered to it, so a listing
// bounded by MaxInstances loses the tail of its window rather than whichever
// series or rows happened to be read last. Ties keep the item offered first.
type Earliest[T any] struct {
	n         int
	seq       int
	items     latestFirst[T]
	truncated bool
}

// NewEarliest returns a collector keeping at most n items.
func NewEarliest[T any](n int) *Earliest[T] {
	return &Earliest[T]{n: n}
}

// Offer considers one item starting at start.
func (e *Earliest[T]) Offer(start time.Time, item T) {
	entry := ranked[T]{start: start, seq: e.seq, item: item}
	e.seq++
	if len(e.items) < e.n {
		heap.Push(&e.items, entry)
		return
	}
	e.truncated = true
	if len(e.items) == 0 || !entry.before(e.items[0]) {
		return
	}
	e.items[0] = entry
	heap.Fix(&e.items, 0)
}

// Truncated reports whether anything offered was left out.
func (e *Earliest[T]) Truncated() bool { return e.truncated }

// Items returns what was kept, earliest first.
func (e *Earliest[T]) Items() []T {
	sorted := append(latestFirst[T](nil), e.items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].before(sorted[j]) })
	out := make([]T, len(sorted))
	for i, r := range sorted {
		out[i] = r.item
	}
	return out
}

type ranked[T any] struct {
	start time.Time
	seq   int
	item  T
}

func (r ranked[T]) before(o ranked[T]) bool {
	if !r.start.Equal(o.start) {
		return r.start.Before(o.start)
	}
	return r.seq < o.seq
}

// latestFirst is a max-heap: its root is the item the cap drops next.
type latestFirst[T any] []ranked[T]

func (h latestFirst[T]) Len() int           { return len(h) }
func (h latestFirst[T]) Less(i, j int) bool { return h[j].before(h[i]) }
func (h latestFirst[T]) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *latestFirst[T]) Push(x any)        { *h = append(*h, x.(ranked[T])) }
func (h *latestFirst[T]) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}
