// Package bus is a tiny in-process publish/subscribe fan-out used to push
// collector ticks, events and alert changes to the alert engine and SSE
// clients. Slow subscribers never block publishers: messages are dropped for
// a subscriber whose buffer is full.
package bus

import "sync"

// Bus fans out values of type T to all subscribers.
type Bus[T any] struct {
	mu   sync.RWMutex
	subs map[int]chan T
	next int
	size int
}

// New creates a bus whose subscriber channels have the given buffer size.
func New[T any](buffer int) *Bus[T] {
	if buffer < 1 {
		buffer = 1
	}
	return &Bus[T]{subs: make(map[int]chan T), size: buffer}
}

// Subscribe returns a channel receiving published values and a cancel func.
func (b *Bus[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, b.size)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			// Drain so a blocked publisher (none, buffers are non-blocking) or
			// reader observes closure.
			close(ch)
		})
	}
}

// Publish delivers v to every subscriber without blocking. Returns the number
// of subscribers that received it.
func (b *Bus[T]) Publish(v T) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, ch := range b.subs {
		select {
		case ch <- v:
			n++
		default:
		}
	}
	return n
}

// Len returns the number of subscribers.
func (b *Bus[T]) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
