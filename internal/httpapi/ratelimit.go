package httpapi

import (
	"slices"
	"sync"
	"time"
)

const (
	// rateLimitPerSecond and rateLimitBurst are the per-identity request
	// budget documented in docs/API.md.
	rateLimitPerSecond = 20.0
	rateLimitBurst     = 60

	// limiterMaxKeys bounds memory: beyond this many tracked identities the
	// least recently used half is evicted.
	limiterMaxKeys = 4096
	// limiterIdleTTL is how long an untouched identity is kept.
	limiterIdleTTL = 5 * time.Minute
	// limiterSweepInterval is how often idle entries are evicted.
	limiterSweepInterval = time.Minute
)

// bucket is one token bucket.
type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a per-key token-bucket limiter (rate tokens per second,
// capacity burst). It is safe for concurrent use and bounded in size: idle
// keys are evicted during calls to allow, at most once per
// limiterSweepInterval or immediately when the map is over capacity, so no
// background goroutine is needed.
type rateLimiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// newRateLimiter returns a limiter allowing rate requests per second with
// the given burst per key. Non-positive values fall back to the defaults;
// a nil clock uses time.Now.
func newRateLimiter(rate float64, burst int, now func() time.Time) *rateLimiter {
	if rate <= 0 {
		rate = rateLimitPerSecond
	}
	if burst <= 0 {
		burst = rateLimitBurst
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		rate:    rate,
		burst:   float64(burst),
		now:     now,
		buckets: make(map[string]*bucket),
	}
}

// allow consumes one token for key and reports whether one was available.
func (l *rateLimiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// size returns the number of tracked keys (tests).
func (l *rateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// sweepLocked drops idle entries at most once per limiterSweepInterval, or
// immediately when the map is over capacity.
func (l *rateLimiter) sweepLocked(now time.Time) {
	over := len(l.buckets) >= limiterMaxKeys
	if !over && now.Sub(l.lastSweep) < limiterSweepInterval {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > limiterIdleTTL {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < limiterMaxKeys {
		return
	}
	// Still full: evict the least recently used half.
	type entry struct {
		key  string
		last time.Time
	}
	entries := make([]entry, 0, len(l.buckets))
	for k, b := range l.buckets {
		entries = append(entries, entry{k, b.last})
	}
	slices.SortFunc(entries, func(a, b entry) int { return a.last.Compare(b.last) })
	for _, e := range entries[:len(entries)-limiterMaxKeys/2] {
		delete(l.buckets, e.key)
	}
}
