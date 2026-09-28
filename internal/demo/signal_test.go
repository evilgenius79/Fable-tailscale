package demo

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestFloorDivMod(t *testing.T) {
	tests := []struct {
		a, b         int64
		wantQ, wantM int64
	}{
		{7, 3, 2, 1}, {-7, 3, -3, 2}, {6, 3, 2, 0}, {-6, 3, -2, 0}, {0, 5, 0, 0}, {-1, 86400, -1, 86399},
	}
	for _, tc := range tests {
		if q := floorDiv(tc.a, tc.b); q != tc.wantQ {
			t.Errorf("floorDiv(%d,%d) = %d, want %d", tc.a, tc.b, q, tc.wantQ)
		}
		if m := floorMod(tc.a, tc.b); m != tc.wantM {
			t.Errorf("floorMod(%d,%d) = %d, want %d", tc.a, tc.b, m, tc.wantM)
		}
	}
}

func TestNoiseBounded(t *testing.T) {
	for seed := uint64(1); seed < 6; seed++ {
		for i := 0; i < 2000; i++ {
			ft := float64(baseTime.Unix()) + float64(i)*7.3
			if v := sumOfSines(mix(seed, 1), slowComponents, ft); v < -1 || v > 1 {
				t.Fatalf("sumOfSines out of range: %v", v)
			}
			if v := sumOfSines(mix(seed, 2), periodicComponents, ft); v < -1 || v > 1 {
				t.Fatalf("periodic sumOfSines out of range: %v", v)
			}
		}
	}
	for _, x := range []float64{-0.1, 0, 0.02, 0.5, 0.98, 1, 1.1} {
		if v := plateau(x, 0.04); v < 0 || v > 1 {
			t.Errorf("plateau(%v) = %v", x, v)
		}
		if v := raisedCosine(x); v < 0 || v > 1 {
			t.Errorf("raisedCosine(%v) = %v", x, v)
		}
	}
	if plateau(0.5, 0.04) != 1 {
		t.Error("plateau centre should be 1")
	}
}

func TestRateVectorPeriodicAndNonNegative(t *testing.T) {
	s, _ := newSim(t, 21, baseTime)
	for _, name := range []string{"nas", "alice-iphone", "old-laptop", "tailwatch-hub"} {
		d := s.dev(t, name)
		p := float64(d.period())
		for i := 0; i < 500; i++ {
			u := float64(i) * 173.0
			for u >= p {
				u -= p
			}
			r := d.rateVector(u)
			for f, v := range r {
				if v < 0 || math.IsNaN(v) {
					t.Fatalf("%s flow %d rate(%v) = %v", name, f, u, v)
				}
			}
		}
		// Periodicity: the period boundary must be continuous in u.
		a, b := d.rateVector(0), d.rateVector(p)
		for f := range a {
			if math.Abs(a[f]-b[f]) > 1e-6*(1+math.Abs(a[f])) {
				t.Errorf("%s flow %d not periodic: rate(0)=%v rate(P)=%v", name, f, a[f], b[f])
			}
		}
	}
}

// TestMonotonicCounters walks the fake clock in 15s polls across midnight
// UTC (a 24h integration-period boundary) and checks that every cumulative
// counter and uptime only ever increases.
func TestMonotonicCounters(t *testing.T) {
	start := time.Date(2026, 9, 28, 23, 40, 0, 0, time.UTC)
	s, c := newSim(t, 8, start)
	ctx := context.Background()
	type prev struct {
		rx, tx int64
		ifs    map[string][2]uint64
		uptime uint64
		boot   time.Time
	}
	last := map[string]*prev{}
	agents := []string{"100.64.0.1", "100.64.0.2", "100.64.0.4", "100.64.0.15"}
	for step := 0; step < 200; step++ { // 50 minutes
		st, err := s.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range st.Peers {
			pv := last[p.DNSName]
			if pv == nil {
				pv = &prev{}
				last[p.DNSName] = pv
			}
			if p.RxBytes < pv.rx || p.TxBytes < pv.tx {
				t.Fatalf("step %d %s: counters went backwards rx %d->%d tx %d->%d", step, p.DNSName, pv.rx, p.RxBytes, pv.tx, p.TxBytes)
			}
			pv.rx, pv.tx = p.RxBytes, p.TxBytes
		}
		for _, ip := range agents {
			rep, err := s.Fetch(ctx, ip, 0)
			if err != nil {
				t.Fatalf("step %d fetch %s: %v", step, ip, err)
			}
			pv := last[ip]
			if pv == nil {
				pv = &prev{ifs: map[string][2]uint64{}}
				last[ip] = pv
			} else {
				if rep.Host.UptimeSeconds != pv.uptime+15 {
					t.Fatalf("step %d %s: uptime %d -> %d, want +15", step, ip, pv.uptime, rep.Host.UptimeSeconds)
				}
				if !rep.Host.BootTime.Equal(pv.boot) {
					t.Fatalf("step %d %s: boot time moved %v -> %v", step, ip, pv.boot, rep.Host.BootTime)
				}
			}
			pv.uptime, pv.boot = rep.Host.UptimeSeconds, rep.Host.BootTime
			for _, ifc := range rep.Net.Interfaces {
				old := pv.ifs[ifc.Name]
				if ifc.RxBytes < old[0] || ifc.TxBytes < old[1] {
					t.Fatalf("step %d %s %s: interface counters went backwards %v -> %d/%d", step, ip, ifc.Name, old, ifc.RxBytes, ifc.TxBytes)
				}
				if ifc.RxPackets > ifc.RxBytes || ifc.TxPackets > ifc.TxBytes {
					t.Fatalf("%s %s: more packets than bytes", ip, ifc.Name)
				}
				pv.ifs[ifc.Name] = [2]uint64{ifc.RxBytes, ifc.TxBytes}
			}
		}
		c.advance(15 * time.Second)
	}
	if c.now().Before(time.Date(2026, 9, 29, 0, 20, 0, 0, time.UTC)) {
		t.Fatal("test did not cross midnight")
	}
	// Counters must actually move for a busy, always-online peer.
	if pv := last["nas."+MagicDNSSuffix]; pv == nil || pv.rx == 0 {
		t.Fatal("nas counters never populated")
	}
}

// TestCountersAcrossLongPeriod exercises the 240h integration period of the
// block-pattern laptop at its period boundary, using the in-package instant
// so the device need not be online.
func TestCountersAcrossLongPeriod(t *testing.T) {
	s, _ := newSim(t, 8, baseTime)
	d := s.dev(t, "old-laptop")
	k := floorDiv(baseTime.Unix()-anchorUnix, blocksPeriod) + 1
	boundary := unix(anchorUnix + k*blocksPeriod)
	// Base time must precede the boundary so the counter epoch is sane.
	s.mu.Lock()
	s.tick()
	var prev [nFlows]float64
	for i := -40; i <= 40; i++ {
		tt := boundary.Add(time.Duration(i) * 15 * time.Second)
		in := s.instantAt(d, tt, false)
		for f := range in.counters {
			if in.counters[f] < prev[f] {
				t.Fatalf("flow %d decreased at %v: %v -> %v", f, tt, prev[f], in.counters[f])
			}
		}
		prev = in.counters
	}
	s.mu.Unlock()
}

// TestCountersHistoryIndependent checks the counter value at an instant does
// not depend on which instants were queried before it.
func TestCountersHistoryIndependent(t *testing.T) {
	a, ca := newSim(t, 3, baseTime)
	b, _ := newSim(t, 3, baseTime)
	da, db := a.dev(t, "homelab"), b.dev(t, "homelab")
	a.mu.Lock()
	b.mu.Lock()
	a.tick()
	b.tick()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	target := baseTime.Add(37*time.Minute + 11*time.Second + 500*time.Millisecond)
	// a walks forward, jumps back, then to the target; b goes straight there.
	for i := 0; i < 100; i++ {
		ca.advance(15 * time.Second)
		a.instantAt(da, ca.now(), false)
	}
	a.instantAt(da, baseTime.Add(-3*time.Hour), false)
	ia := a.instantAt(da, target, false)
	ib := b.instantAt(db, target, false)
	if ia.counters != ib.counters {
		t.Fatalf("counters differ: %v vs %v", ia.counters, ib.counters)
	}
	if ia.counters[flowPeerRx] <= 0 {
		t.Fatal("homelab counters should be positive")
	}
}

func TestPresencePatterns(t *testing.T) {
	s, _ := newSim(t, 13, baseTime)
	s.mu.Lock()
	s.tick()
	defer s.mu.Unlock()

	t.Run("iphone flaps ~4min per ~40min", func(t *testing.T) {
		d := s.dev(t, "alice-iphone")
		off, n, transitions := 0, 0, 0
		var prevOn bool
		var longestOff time.Duration
		var offStart time.Time
		for tt := baseTime; tt.Before(baseTime.Add(24 * time.Hour)); tt = tt.Add(15 * time.Second) {
			on, since := s.presence(d, tt)
			if !on {
				off++
				if since.After(tt) || tt.Sub(since) > 6*time.Minute {
					t.Fatalf("offline since %v at %v is implausible", since, tt)
				}
			}
			if n > 0 && on != prevOn {
				transitions++
				if on {
					if d := tt.Sub(offStart); d > longestOff {
						longestOff = d
					}
				} else {
					offStart = tt
				}
			}
			prevOn = on
			n++
		}
		frac := float64(off) / float64(n)
		if frac < 0.07 || frac > 0.13 {
			t.Errorf("offline fraction = %.3f, want ~0.10", frac)
		}
		if transitions < 60 || transitions > 80 {
			t.Errorf("transitions in 24h = %d, want ~72", transitions)
		}
		if longestOff > 5*time.Minute+15*time.Second {
			t.Errorf("longest offline window = %v", longestOff)
		}
	})

	t.Run("old laptop online ~10% in 6h blocks", func(t *testing.T) {
		d := s.dev(t, "old-laptop")
		on, n := 0, 0
		for tt := baseTime; tt.Before(baseTime.Add(240 * time.Hour)); tt = tt.Add(5 * time.Minute) {
			if o, _ := s.presence(d, tt); o {
				on++
			}
			n++
		}
		frac := float64(on) / float64(n)
		if frac < 0.08 || frac > 0.13 {
			t.Errorf("online fraction = %.3f, want ~0.10", frac)
		}
		// State only changes on 6-hour block boundaries.
		prevOn, _ := s.presence(d, baseTime)
		for tt := baseTime.Add(time.Minute); tt.Before(baseTime.Add(240 * time.Hour)); tt = tt.Add(time.Minute) {
			o, since := s.presence(d, tt)
			if o != prevOn && floorMod(tt.Unix()-anchorUnix, blockLen) != 0 {
				t.Fatalf("presence changed at %v, not a block boundary", tt)
			}
			if floorMod(since.Unix()-anchorUnix, blockLen) != 0 {
				t.Fatalf("since %v is not block aligned", since)
			}
			prevOn = o
		}
	})

	t.Run("ipad never online", func(t *testing.T) {
		d := s.dev(t, "alice-ipad")
		for tt := baseTime; tt.Before(baseTime.Add(48 * time.Hour)); tt = tt.Add(17 * time.Minute) {
			if on, since := s.presence(d, tt); on || tt.Sub(since) < 4*day {
				t.Fatalf("ipad online=%v since=%v at %v", on, since, tt)
			}
		}
	})

	t.Run("expired key goes offline", func(t *testing.T) {
		d := s.dev(t, "office-printer-gw")
		exp, disabled := s.keyExpiry(d)
		if disabled {
			t.Fatal("printer expiry disabled")
		}
		if on, _ := s.presence(d, exp.Add(-time.Second)); !on {
			t.Error("should be online just before expiry")
		}
		if on, since := s.presence(d, exp.Add(time.Hour)); on || !since.Equal(exp) {
			t.Errorf("after expiry: online=%v since=%v", on, since)
		}
		if !s.instantAt(d, exp.Add(time.Hour), false).expired {
			t.Error("expired flag not set")
		}
	})
}

func TestCPUSpikesAndSmoothness(t *testing.T) {
	s, _ := newSim(t, 17, baseTime)
	s.mu.Lock()
	s.tick()
	defer s.mu.Unlock()

	bob := s.dev(t, "bob-desktop")
	over, n := 0, 0
	var prevCPU float64
	maxJump := 0.0
	for tt := baseTime; tt.Before(baseTime.Add(6 * time.Hour)); tt = tt.Add(15 * time.Second) {
		in := s.instantAt(bob, tt, true)
		if in.cpu > 90 {
			over++
		}
		if n > 0 {
			if j := math.Abs(in.cpu - prevCPU); j > maxJump {
				maxJump = j
			}
		}
		if len(in.perCore) != bob.cores {
			t.Fatalf("perCore len = %d", len(in.perCore))
		}
		for _, c := range in.perCore {
			if c < 0 || c > 100 {
				t.Fatalf("perCore value %v", c)
			}
		}
		if in.load1 < 0 || in.mem < 0 || in.mem > 100 || in.disk < 0 || in.disk > 100 {
			t.Fatalf("out of range metrics: %+v", in)
		}
		prevCPU = in.cpu
		n++
	}
	// Three 12-minute plateaus in 6 hours: ~36 minutes at 15s = ~144 samples.
	minutes := float64(over) * 15 / 60
	if minutes < 28 || minutes > 40 {
		t.Errorf("bob-desktop >90%% for %.1f minutes in 6h, want ~36", minutes)
	}
	// Even the plateau edges ramp over ~30s, so no 15s step jumps by more than ~60 points.
	if maxJump > 60 {
		t.Errorf("max 15s CPU jump = %.1f", maxJump)
	}

	// A calm device stays well below 90% and moves smoothly.
	nas := s.dev(t, "nas")
	prevCPU, maxJump = 0, 0
	for i, tt := 0, baseTime; tt.Before(baseTime.Add(6 * time.Hour)); i, tt = i+1, tt.Add(15*time.Second) {
		in := s.instantAt(nas, tt, false)
		if in.cpu > 80 {
			t.Fatalf("nas cpu %v at %v", in.cpu, tt)
		}
		if i > 0 {
			if j := math.Abs(in.cpu - prevCPU); j > maxJump {
				maxJump = j
			}
		}
		prevCPU = in.cpu
	}
	// Bursts ramp over ~75s, so a 15s step never moves by more than ~20 points.
	if maxJump > 20 {
		t.Errorf("nas max 15s CPU jump = %.1f, want smooth", maxJump)
	}
}

func TestLatencyRanges(t *testing.T) {
	s, _ := newSim(t, 23, baseTime)
	s.mu.Lock()
	s.tick()
	defer s.mu.Unlock()
	for _, d := range s.devs {
		if d == s.self {
			continue
		}
		lo, hi := 1.5, 40.0
		if !d.direct {
			lo, hi = 35, 320
		}
		for tt := baseTime; tt.Before(baseTime.Add(2 * time.Hour)); tt = tt.Add(15 * time.Second) {
			in := s.instantAt(d, tt, false)
			if in.latency < lo || in.latency > hi {
				t.Errorf("%s latency %.1f outside [%v,%v] at %v", d.name, in.latency, lo, hi, tt)
				break
			}
		}
	}
}

func TestBootAndHandshake(t *testing.T) {
	s, _ := newSim(t, 2, baseTime)
	s.mu.Lock()
	s.tick()
	defer s.mu.Unlock()
	nas := s.dev(t, "nas")
	b0 := s.bootTime(nas, baseTime)
	if b0.After(baseTime) || baseTime.Sub(b0) > 90*day {
		t.Errorf("nas boot %v", b0)
	}
	if b1 := s.bootTime(nas, baseTime.Add(time.Hour)); !b1.Equal(b0) {
		t.Errorf("boot moved within the reboot period: %v -> %v", b0, b1)
	}
	if b2 := s.bootTime(nas, b0.Add(90*day)); !b2.Equal(b0.Add(90 * day)) {
		t.Errorf("reboot cycle: %v, want %v", b2, b0.Add(90*day))
	}
	var prev time.Time
	for i := 0; i < 100; i++ {
		tt := baseTime.Add(time.Duration(i) * 15 * time.Second)
		hs := nas.lastHandshake(tt)
		if hs.After(tt) || tt.Sub(hs) >= 2*time.Minute || hs.Before(prev) {
			t.Fatalf("handshake %v at %v (prev %v)", hs, tt, prev)
		}
		prev = hs
	}
}
