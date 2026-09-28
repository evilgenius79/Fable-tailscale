package demo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestBackfill(t *testing.T) {
	s, _ := newSim(t, 81, baseTime)
	st := openStore(t)
	ctx := context.Background()

	const dur, step = time.Hour, 15 * time.Second
	started := time.Now()
	if err := s.Backfill(ctx, st, dur, step); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := int64(16) * int64(dur/step+1)
	if stats.Samples != wantRows {
		t.Fatalf("samples = %d, want %d", stats.Samples, wantRows)
	}
	if stats.Devices != 16 {
		t.Fatalf("devices = %d, want 16", stats.Devices)
	}
	if stats.OldestSample == nil || !stats.OldestSample.Equal(baseTime.Add(-dur)) {
		t.Fatalf("oldest sample = %v, want %v", stats.OldestSample, baseTime.Add(-dur))
	}
	t.Logf("backfilled %d rows in %v", stats.Samples, elapsed)

	// Devices carry FirstSeen = start of the range.
	devs, err := st.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		if !d.FirstSeen.Equal(baseTime.Add(-dur)) {
			t.Errorf("%s FirstSeen = %v", d.Name, d.FirstSeen)
		}
		if d.Name == "" || d.DNSName == "" || len(d.Addresses) != 2 {
			t.Errorf("incomplete device %+v", d)
		}
	}

	// Continuity: the last backfilled sample equals the live view at the
	// same instant.
	status, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range status.Peers {
		sm, err := st.LatestSample(ctx, p.ID)
		if err != nil {
			t.Fatalf("latest sample %s: %v", p.DNSName, err)
		}
		if !sm.TS.Equal(baseTime) {
			t.Fatalf("%s latest sample ts = %v, want %v", p.DNSName, sm.TS, baseTime)
		}
		if sm.Online != p.Online || sm.TSRxBytes != p.RxBytes || sm.TSTxBytes != p.TxBytes {
			t.Errorf("%s sample %+v != status online=%v rx=%d tx=%d", p.DNSName, sm, p.Online, p.RxBytes, p.TxBytes)
		}
		if p.Online {
			if sm.LatencyMs == nil || sm.Direct == nil || *sm.Direct != (p.CurAddr != "") {
				t.Errorf("%s path fields %+v vs curAddr %q", p.DNSName, sm, p.CurAddr)
			}
			if !*sm.Direct && sm.Relay != p.Relay {
				t.Errorf("%s relay %q vs %q", p.DNSName, sm.Relay, p.Relay)
			}
			pr, err := s.Ping(ctx, p.TailscaleIPs[0], 0)
			if err != nil || pr.LatencyMs != *sm.LatencyMs {
				t.Errorf("%s latency sample %v vs ping %v (%v)", p.DNSName, *sm.LatencyMs, pr, err)
			}
		} else if sm.LatencyMs != nil || sm.AgentOK {
			t.Errorf("%s offline sample has live fields %+v", p.DNSName, sm)
		}
		rep, ferr := s.Fetch(ctx, p.TailscaleIPs[0], 0)
		if (ferr == nil) != sm.AgentOK {
			t.Errorf("%s agentOk=%v but fetch err=%v", p.DNSName, sm.AgentOK, ferr)
		}
		if ferr == nil {
			if sm.CPU == nil || *sm.CPU != rep.CPU.Percent || sm.Mem == nil || *sm.Mem != rep.Memory.Percent {
				t.Errorf("%s cpu/mem sample %v/%v vs report %v/%v", p.DNSName, sm.CPU, sm.Mem, rep.CPU.Percent, rep.Memory.Percent)
			}
			if sm.Uptime == nil || *sm.Uptime != rep.Host.UptimeSeconds || sm.Disk == nil || sm.Load1 == nil || sm.NetRxRate == nil {
				t.Errorf("%s agent fields %+v", p.DNSName, sm)
			}
			if (sm.TempC != nil) != (len(rep.Temperatures) > 0) {
				t.Errorf("%s temp sample %v vs %v", p.DNSName, sm.TempC, rep.Temperatures)
			}
		}
	}
	// The hub's own row is online with agent metrics and no tailnet bytes.
	self, err := st.LatestSample(ctx, status.Self.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !self.Online || !self.AgentOK || self.TSRxBytes != 0 || self.LatencyMs != nil || self.CPU == nil {
		t.Errorf("self sample = %+v", self)
	}
	// Unauthorized workshop-pi: online per the API, no path, no agent.
	wp, err := st.LatestSample(ctx, "nDEMO13CNTRL")
	if err != nil {
		t.Fatal(err)
	}
	if !wp.Online || wp.Direct != nil || wp.AgentOK || wp.TSRxBytes != 0 {
		t.Errorf("workshop-pi sample = %+v", wp)
	}

	// Rates in history are non-negative and roughly match the analytic rate
	// for a busy always-online peer; the first sample must not be zero.
	series, err := st.QuerySeries(ctx, "nDEMO02CNTRL", baseTime.Add(-dur), baseTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) < 100 {
		t.Fatalf("series points = %d", len(series.Points))
	}
	for _, pt := range series.Points {
		if pt.TSRxRate < 0 || pt.TSTxRate < 0 || pt.CPU == nil || pt.Online != 1 {
			t.Fatalf("bad point %+v", pt)
		}
	}
	first, err := st.QuerySeries(ctx, "nDEMO02CNTRL", baseTime.Add(-dur), baseTime.Add(-dur+time.Minute))
	if err != nil || len(first.Points) == 0 || first.Points[0].TSRxRate <= 0 {
		t.Fatalf("first point rate should be analytic and positive: %+v (%v)", first, err)
	}

	// A second call is a no-op.
	if err := s.Backfill(ctx, st, dur, step); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.Stats(ctx); again.Samples != stats.Samples {
		t.Fatalf("second backfill changed samples %d -> %d", stats.Samples, again.Samples)
	}
}

func TestBackfillArgs(t *testing.T) {
	s, _ := newSim(t, 1, baseTime)
	ctx := context.Background()
	st := openStore(t)
	if err := s.Backfill(ctx, nil, time.Hour, time.Second); err == nil {
		t.Error("nil store accepted")
	}
	if err := s.Backfill(ctx, st, time.Hour, 0); err == nil {
		t.Error("zero step accepted")
	}
	if err := s.Backfill(ctx, st, time.Hour, 500*time.Millisecond); err == nil {
		t.Error("sub-second step accepted")
	}
	if err := s.Backfill(ctx, st, 0, time.Second); err != nil {
		t.Errorf("zero duration should be a no-op: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Backfill(cctx, st, time.Hour, 15*time.Second); err == nil {
		t.Error("cancelled context accepted")
	}
	if stats, _ := st.Stats(ctx); stats.Samples != 0 {
		t.Errorf("samples written despite errors: %d", stats.Samples)
	}
	// Unaligned "now": samples land on the step grid at or before now.
	odd := baseTime.Add(7 * time.Second)
	s2, _ := newSim(t, 1, odd)
	if err := s2.Backfill(ctx, st, 10*time.Minute, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	sm, err := st.LatestSample(ctx, "nDEMO02CNTRL")
	if err != nil {
		t.Fatal(err)
	}
	if !sm.TS.Equal(baseTime) {
		t.Errorf("latest ts = %v, want %v", sm.TS, baseTime)
	}
	stats, _ := st.Stats(ctx)
	// 14:20:15 .. 14:30:00 inclusive on the 15s grid = 40 points per device.
	if stats.Samples != 16*40 {
		t.Errorf("samples = %d, want %d", stats.Samples, 16*40)
	}
}

func TestBackfillSkipsWhenStoreHasSamples(t *testing.T) {
	s, _ := newSim(t, 1, baseTime)
	ctx := context.Background()
	st := openStore(t)
	if err := st.InsertSamples(ctx, []model.Sample{{DeviceID: "x", TS: baseTime.Add(-time.Hour), Online: true}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Backfill(ctx, st, time.Hour, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if stats, _ := st.Stats(ctx); stats.Samples != 1 {
		t.Errorf("samples = %d, want 1 (untouched)", stats.Samples)
	}
}

// TestBackfillFullDay is the production shape: 24h at the default 15s poll
// interval for the whole fleet (~92k rows). It must comfortably finish
// within the hub's startup budget. The SQLite inserts take ~1.3s natively
// but ~25s under the race detector, so it only runs when
// TAILWATCH_DEMO_FULL_BACKFILL=1 is set.
func TestBackfillFullDay(t *testing.T) {
	if os.Getenv("TAILWATCH_DEMO_FULL_BACKFILL") == "" {
		t.Skip("set TAILWATCH_DEMO_FULL_BACKFILL=1 to run the full-day backfill")
	}
	s, _ := newSim(t, 1, baseTime)
	ctx := context.Background()
	st := openStore(t)
	started := time.Now()
	if err := s.Backfill(ctx, st, 24*time.Hour, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(16 * (24*3600/15 + 1))
	if stats.Samples != want {
		t.Fatalf("samples = %d, want %d", stats.Samples, want)
	}
	t.Logf("full-day backfill: %d rows in %v", stats.Samples, elapsed)
	if elapsed > 30*time.Second {
		t.Fatalf("backfill took %v, want well under 30s", elapsed)
	}
}
