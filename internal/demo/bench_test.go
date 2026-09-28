package demo

import (
	"context"
	"testing"
	"time"
)

// BenchmarkFirstStatus measures a fresh Sim's first poll, which includes the
// one-off per-device period integrals.
func BenchmarkFirstStatus(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s, _ := newSim(b, 1, baseTime)
		if _, err := s.Status(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInstant measures the steady-state cost of one device instant.
func BenchmarkInstant(b *testing.B) {
	s, _ := newSim(b, 1, baseTime)
	d := s.devs[1]
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick()
	s.instantAt(d, baseTime, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.instantAt(d, baseTime.Add(time.Duration(i)*15*time.Second), true)
	}
}
