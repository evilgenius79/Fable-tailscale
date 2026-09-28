package main

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newFakeClock() *fakeClock               { return &fakeClock{t: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)} }
func countAllowed(l *rateLimiter, key string, n int) int {
	ok := 0
	for i := 0; i < n; i++ {
		if l.allow(key) {
			ok++
		}
	}
	return ok
}

func TestRateLimiterBurstAndRefill(t *testing.T) {
	c := newFakeClock()
	l := newRateLimiter(10, 20, c.now)
	if got := countAllowed(l, "a", 25); got != 20 {
		t.Fatalf("burst allowed %d, want 20", got)
	}
	c.advance(100 * time.Millisecond) // +1 token
	if got := countAllowed(l, "a", 3); got != 1 {
		t.Fatalf("after 100ms allowed %d, want 1", got)
	}
	c.advance(time.Second) // +10 tokens
	if got := countAllowed(l, "a", 15); got != 10 {
		t.Fatalf("after 1s allowed %d, want 10", got)
	}
	c.advance(time.Hour) // capped at burst
	if got := countAllowed(l, "a", 25); got != 20 {
		t.Fatalf("after 1h allowed %d, want 20", got)
	}
}

func TestRateLimiterPerKey(t *testing.T) {
	l := newRateLimiter(10, 2, newFakeClock().now)
	if countAllowed(l, "a", 5) != 2 || countAllowed(l, "b", 5) != 2 {
		t.Fatal("keys should have independent budgets")
	}
}

func TestRateLimiterDefaultsAndBounds(t *testing.T) {
	c := newFakeClock()
	l := newRateLimiter(0, 0, c.now)
	if l.rate != rateLimitPerSecond || l.burst != rateLimitBurst {
		t.Fatalf("defaults = %v/%v", l.rate, l.burst)
	}
	for i := 0; i < limiterMaxKeys+10; i++ {
		l.allow(fmt.Sprintf("ip%d", i))
	}
	if len(l.buckets) > limiterMaxKeys {
		t.Fatalf("limiter grew to %d keys", len(l.buckets))
	}
	c.advance(limiterIdleTTL + time.Minute + time.Second)
	l.allow("fresh")
	if len(l.buckets) != 1 {
		t.Fatalf("idle entries not swept: %d", len(l.buckets))
	}
}

func TestLogLimiter(t *testing.T) {
	c := newFakeClock()
	l := newLogLimiter(10*time.Second, c.now)
	if !l.allow("a") || l.allow("a") || !l.allow("b") {
		t.Fatal("first per key should pass, repeat should not")
	}
	c.advance(9 * time.Second)
	if l.allow("a") {
		t.Fatal("allowed before interval elapsed")
	}
	c.advance(time.Second)
	if !l.allow("a") {
		t.Fatal("not allowed after interval")
	}
}
