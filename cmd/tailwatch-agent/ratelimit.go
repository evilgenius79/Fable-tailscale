package main

import (
	"slices"
	"sync"
	"time"
)

const (
	// rateLimitPerSecond and rateLimitBurst are the per-source-IP request
	// budget.
	rateLimitPerSecond = 10.0
	rateLimitBurst     = 20

	// warnLogInterval is the minimum spacing of rejected-request warnings
	// per source IP.
	warnLogInterval = 10 * time.Second

	// limiterMaxKeys bounds memory: beyond this many tracked keys, idle
	// entries are evicted (oldest first).
	limiterMaxKeys = 4096

	// limiterIdleTTL is how long an untouched key is kept.
	limiterIdleTTL = 5 * time.Minute
)

// bucket is one token bucket.
type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a small per-key token-bucket limiter (rate tokens per
// second, capacity burst). It is safe for concurrent use and bounded in
// size.
type rateLimiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// newRateLimiter returns a limiter allowing rate requests per second with
// the given burst per key. Non-positive values fall back to the defaults.
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

// allow consumes one token for key and reports whether it was available.
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
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
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

// sweepLocked drops idle entries at most once a minute, or immediately when
// the map is over capacity.
func (l *rateLimiter) sweepLocked(now time.Time) {
	over := len(l.buckets) >= limiterMaxKeys
	if !over && now.Sub(l.lastSweep) < time.Minute {
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

// logLimiter allows one event per key per interval; used to keep warnings
// about rejected requests from flooding the log.
type logLimiter struct {
	interval time.Duration
	now      func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

// newLogLimiter returns a limiter emitting at most one event per key per
// interval.
func newLogLimiter(interval time.Duration, now func() time.Time) *logLimiter {
	if interval <= 0 {
		interval = warnLogInterval
	}
	if now == nil {
		now = time.Now
	}
	return &logLimiter{interval: interval, now: now, last: make(map[string]time.Time)}
}

// allow reports whether an event for key may be logged now.
func (l *logLimiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.last[key]; ok && now.Sub(t) < l.interval {
		return false
	}
	if len(l.last) >= limiterMaxKeys {
		for k, t := range l.last {
			if now.Sub(t) >= l.interval {
				delete(l.last, k)
			}
		}
		if len(l.last) >= limiterMaxKeys {
			clear(l.last)
		}
	}
	l.last[key] = now
	return true
}
