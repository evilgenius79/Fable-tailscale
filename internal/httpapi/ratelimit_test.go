package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := baseTime
	l := newRateLimiter(10, 5, func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if !l.allow("a") {
			t.Fatalf("request %d denied within burst", i)
		}
	}
	if l.allow("a") {
		t.Fatal("burst exceeded but allowed")
	}
	if !l.allow("b") {
		t.Fatal("independent key denied")
	}
	now = now.Add(100 * time.Millisecond) // one token
	if !l.allow("a") || l.allow("a") {
		t.Fatal("refill of one token wrong")
	}
	now = now.Add(time.Hour) // capped at burst
	for i := 0; i < 5; i++ {
		if !l.allow("a") {
			t.Fatalf("after refill request %d denied", i)
		}
	}
	if l.allow("a") {
		t.Fatal("bucket exceeded burst capacity")
	}
}

func TestRateLimiterEviction(t *testing.T) {
	now := baseTime
	l := newRateLimiter(10, 5, func() time.Time { return now })
	l.allow("old")
	now = now.Add(limiterIdleTTL + time.Minute)
	l.allow("new") // triggers the periodic sweep
	if l.size() != 1 {
		t.Errorf("size = %d, want 1 after idle eviction", l.size())
	}
	// Over capacity: the least recently used half is evicted immediately,
	// oldest keys first.
	l = newRateLimiter(10, 5, func() time.Time { return now })
	for i := 0; i < limiterMaxKeys; i++ {
		now = now.Add(time.Millisecond)
		l.allow(fmt.Sprintf("k%d", i))
	}
	if l.size() != limiterMaxKeys {
		t.Fatalf("size = %d, want %d", l.size(), limiterMaxKeys)
	}
	now = now.Add(time.Millisecond)
	l.allow("overflow")
	if l.size() != limiterMaxKeys/2+1 {
		t.Errorf("size = %d after overflow eviction, want %d", l.size(), limiterMaxKeys/2+1)
	}
	l.mu.Lock()
	_, oldest := l.buckets["k0"]
	_, newest := l.buckets[fmt.Sprintf("k%d", limiterMaxKeys-1)]
	l.mu.Unlock()
	if oldest || !newest {
		t.Errorf("eviction order wrong: k0 present=%v, newest present=%v", oldest, newest)
	}
}

func TestRateLimiterDefaults(t *testing.T) {
	l := newRateLimiter(0, 0, nil)
	if l.rate != rateLimitPerSecond || l.burst != rateLimitBurst || l.now == nil {
		t.Errorf("defaults = %+v", l)
	}
}
