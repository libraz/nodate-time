package middleware

import (
	"container/list"
	"sync"
	"time"
)

// WindowCounter counts events per key in fixed windows and holds at most a
// fixed number of keys. Past that the oldest window is dropped, which forgets
// a key rather than refusing a new one, and every call costs amortized O(1):
// the keys are kept oldest window first -- a window only ever starts now, so
// appending keeps them sorted -- and expired ones are always at the front.
type WindowCounter struct {
	mu      sync.Mutex
	window  time.Duration
	maxKeys int
	keys    map[string]*list.Element
	order   *list.List
}

type windowBucket struct {
	key   string
	count int
	start time.Time
}

// NewWindowCounter counts in windows of the given length across at most
// maxKeys keys.
func NewWindowCounter(window time.Duration, maxKeys int) *WindowCounter {
	return &WindowCounter{
		window:  window,
		maxKeys: maxKeys,
		keys:    make(map[string]*list.Element),
		order:   list.New(),
	}
}

// Take counts one event for key and returns the count in key's current
// window, this one included, and when that window ends.
func (c *WindowCounter) Take(key string, now time.Time) (count int, reset time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for front := c.order.Front(); front != nil; front = c.order.Front() {
		if now.Sub(front.Value.(*windowBucket).start) < c.window {
			break
		}
		c.drop(front)
	}
	e, ok := c.keys[key]
	if !ok {
		if len(c.keys) >= c.maxKeys {
			c.drop(c.order.Front())
		}
		e = c.order.PushBack(&windowBucket{key: key, start: now})
		c.keys[key] = e
	}
	b := e.Value.(*windowBucket)
	b.count++
	return b.count, b.start.Add(c.window)
}

// Len reports how many keys are held.
func (c *WindowCounter) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.keys)
}

func (c *WindowCounter) drop(e *list.Element) {
	delete(c.keys, e.Value.(*windowBucket).key)
	c.order.Remove(e)
}
