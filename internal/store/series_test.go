package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func TestPlanSeries(t *testing.T) {
	const day = 24 * time.Hour
	tests := []struct {
		rng    time.Duration
		source string
		step   int64
		bucket bool
	}{
		{15 * time.Minute, SourceRaw, 15, false},
		{time.Hour, SourceRaw, 15, false},
		{3 * time.Hour, SourceRaw, 15, false},
		{3*time.Hour + time.Second, SourceRaw, 60, true},
		{6 * time.Hour, SourceRaw, 60, true},
		{12 * time.Hour, SourceRaw, 60, true},
		{day, SourceRaw, 60, true},
		{day + time.Minute, SourceRaw, 300, true},
		{2 * day, SourceRaw, 300, true},
		{2*day + time.Second, SourceRollup, 300, true},
		{7 * day, SourceRollup, 300, true},
		{7*day + time.Second, SourceRollup, 1800, true},
		{14 * day, SourceRollup, 1800, true},
		{14*day + time.Second, SourceRollup, 3600, true},
		{30 * day, SourceRollup, 3600, true},
		{90 * day, SourceRollup, 3600, true},
	}
	for _, tc := range tests {
		got := planSeries(tc.rng)
		if got.source != tc.source || got.step != tc.step || got.bucket != tc.bucket {
			t.Errorf("planSeries(%v) = %+v, want source=%s step=%d bucket=%v", tc.rng, got, tc.source, tc.step, tc.bucket)
		}
	}
}

func TestInsertAndLatestSample(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	if err := s.InsertSamples(ctx, nil); err != nil {
		t.Errorf("InsertSamples(nil) = %v", err)
	}
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: ""}}); err == nil {
		t.Error("InsertSamples with empty device id succeeded")
	}
	if _, err := s.LatestSample(ctx, "d1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestSample empty = %v, want ErrNotFound", err)
	}

	up := uint64(3600)
	full := model.Sample{
		DeviceID: "d1", TS: at(30).Add(700 * time.Millisecond), Online: true, LatencyMs: fp(12.5), Relay: "nyc", Direct: bp(false),
		TSRxBytes: 100, TSTxBytes: 200, TSRxRate: 1.5, TSTxRate: 2.5, AgentOK: true,
		CPU: fp(10), Mem: fp(20), Disk: fp(30), Load1: fp(0.5), NetRxRate: fp(1000), NetTxRate: fp(2000), TempC: fp(45), Uptime: &up,
	}
	older := model.Sample{DeviceID: "d1", TS: at(15), Online: false}
	if err := s.InsertSamples(ctx, []model.Sample{full, older}); err != nil {
		t.Fatalf("InsertSamples: %v", err)
	}
	got, err := s.LatestSample(ctx, "d1")
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if !got.TS.Equal(at(30)) {
		t.Errorf("TS = %v, want truncated %v", got.TS, at(30))
	}
	if !got.Online || got.LatencyMs == nil || *got.LatencyMs != 12.5 || got.Relay != "nyc" || got.Direct == nil || *got.Direct {
		t.Errorf("connectivity fields mismatch: %+v", got)
	}
	if got.TSRxBytes != 100 || got.TSTxBytes != 200 || got.TSRxRate != 1.5 || got.TSTxRate != 2.5 || !got.AgentOK {
		t.Errorf("byte/rate fields mismatch: %+v", got)
	}
	for name, pair := range map[string][2]*float64{
		"cpu": {got.CPU, full.CPU}, "mem": {got.Mem, full.Mem}, "disk": {got.Disk, full.Disk}, "load1": {got.Load1, full.Load1},
		"netrx": {got.NetRxRate, full.NetRxRate}, "nettx": {got.NetTxRate, full.NetTxRate}, "temp": {got.TempC, full.TempC},
	} {
		if pair[0] == nil || *pair[0] != *pair[1] {
			t.Errorf("%s = %v, want %v", name, pair[0], *pair[1])
		}
	}
	if got.Uptime == nil || *got.Uptime != up {
		t.Errorf("uptime = %v, want %d", got.Uptime, up)
	}

	// Nil pointers stay nil; same-second insert replaces.
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "d1", TS: at(30).Add(200 * time.Millisecond), Online: false, Relay: ""}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.LatestSample(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Online || got.LatencyMs != nil || got.Direct != nil || got.CPU != nil || got.Uptime != nil || got.Relay != "" {
		t.Errorf("replaced sample not clean: %+v", got)
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Samples != 2 {
		t.Errorf("samples = %d, want 2 (replace, not duplicate)", st.Samples)
	}

	// Zero TS defaults to now; huge uptime is clamped rather than failing.
	huge := uint64(math.MaxUint64)
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "d2", Uptime: &huge}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.LatestSample(ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	if !got.TS.Equal(fixedNow) || got.Uptime == nil || *got.Uptime != math.MaxInt64 {
		t.Errorf("defaults: ts=%v uptime=%v", got.TS, got.Uptime)
	}
}

func TestQuerySeriesRawUnbucketed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	samples := []model.Sample{
		{DeviceID: "d1", TS: at(0), Online: true, Direct: bp(true), LatencyMs: fp(5), CPU: fp(10), TSRxRate: 100},
		{DeviceID: "d1", TS: at(15), Online: false},
		{DeviceID: "d1", TS: at(30), Online: true, Direct: bp(false), CPU: fp(30), TSTxRate: 7},
		{DeviceID: "d1", TS: at(3600), Online: true}, // outside the range
		{DeviceID: "d2", TS: at(15), Online: true},   // other device
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	ser, err := s.QuerySeries(ctx, "d1", at(0), at(3599))
	if err != nil {
		t.Fatal(err)
	}
	if ser.Source != SourceRaw || ser.StepSec != 15 || ser.DeviceID != "d1" {
		t.Errorf("series meta = source=%s step=%d id=%s", ser.Source, ser.StepSec, ser.DeviceID)
	}
	if len(ser.Points) != 3 {
		t.Fatalf("points = %d, want 3: %+v", len(ser.Points), ser.Points)
	}
	p0, p1, p2 := ser.Points[0], ser.Points[1], ser.Points[2]
	if p0.T != base || p0.Online != 1 || p0.Direct == nil || *p0.Direct != 1 || p0.LatencyMs == nil || *p0.LatencyMs != 5 || p0.CPU == nil || *p0.CPU != 10 || p0.TSRxRate != 100 {
		t.Errorf("p0 = %+v", p0)
	}
	if p0.LatencyMax != nil || p0.CPUMax != nil {
		t.Errorf("raw points must not carry max fields: %+v", p0)
	}
	if p1.T != base+15 || p1.Online != 0 || p1.Direct != nil || p1.LatencyMs != nil || p1.CPU != nil {
		t.Errorf("p1 = %+v", p1)
	}
	if p2.T != base+30 || p2.Online != 1 || p2.Direct == nil || *p2.Direct != 0 || p2.TSTxRate != 7 {
		t.Errorf("p2 = %+v", p2)
	}

	// Inclusive end: a sample exactly at `to` is included.
	ser, err = s.QuerySeries(ctx, "d1", at(0), at(15))
	if err != nil {
		t.Fatal(err)
	}
	if len(ser.Points) != 2 {
		t.Errorf("inclusive end: points = %d, want 2", len(ser.Points))
	}

	// Empty and degenerate ranges give an empty, non-nil slice.
	for _, tc := range []struct {
		name     string
		id       model.DeviceID
		from, to time.Time
	}{
		{"no data", "nobody", at(0), at(100)},
		{"to before from", "d1", at(100), at(0)},
		{"empty id", "", at(0), at(100)},
	} {
		ser, err := s.QuerySeries(ctx, tc.id, tc.from, tc.to)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ser.Points == nil || len(ser.Points) != 0 {
			t.Errorf("%s: points = %v, want empty non-nil", tc.name, ser.Points)
		}
	}
}

func TestQuerySeriesRawBucketed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	samples := []model.Sample{
		// bucket base (60s)
		{DeviceID: "d1", TS: at(0), Online: true, Direct: bp(true), LatencyMs: fp(5), CPU: fp(10), TSRxRate: 100, Mem: fp(50), TempC: fp(40)},
		{DeviceID: "d1", TS: at(15), Online: true, CPU: fp(20), TSRxRate: 200},
		{DeviceID: "d1", TS: at(30), Online: false, Direct: bp(false), TSRxRate: 300},
		{DeviceID: "d1", TS: at(45), Online: true, Direct: bp(true), LatencyMs: fp(15), CPU: fp(30), TSRxRate: 400, Mem: fp(70)},
		// bucket base+60
		{DeviceID: "d1", TS: at(60), Online: false},
		{DeviceID: "d1", TS: at(75), Online: false},
		// bucket base+300 (only sample; also proves ordering)
		{DeviceID: "d1", TS: at(301), Online: true, Disk: fp(90), Load1: fp(1.25), NetRxRate: fp(10), NetTxRate: fp(20)},
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	ser, err := s.QuerySeries(ctx, "d1", at(0), at(6*3600))
	if err != nil {
		t.Fatal(err)
	}
	if ser.Source != SourceRaw || ser.StepSec != 60 {
		t.Fatalf("series meta = source=%s step=%d, want raw/60", ser.Source, ser.StepSec)
	}
	if len(ser.Points) != 3 {
		t.Fatalf("points = %d, want 3: %+v", len(ser.Points), ser.Points)
	}
	p0 := ser.Points[0]
	if p0.T != base {
		t.Errorf("p0.T = %d, want %d", p0.T, base)
	}
	approx := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s = nil, want %v", name, want)
			return
		}
		if math.Abs(*got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, *got, want)
		}
	}
	if math.Abs(p0.Online-0.75) > 1e-9 {
		t.Errorf("p0.Online = %v, want 0.75", p0.Online)
	}
	approx("p0.Direct", p0.Direct, 2.0/3.0)
	approx("p0.LatencyMs", p0.LatencyMs, 10)
	approx("p0.LatencyMax", p0.LatencyMax, 15)
	approx("p0.CPU", p0.CPU, 20)
	approx("p0.CPUMax", p0.CPUMax, 30)
	approx("p0.Mem", p0.Mem, 60)
	approx("p0.TempC", p0.TempC, 40)
	if p0.TSRxRate != 250 || p0.TSTxRate != 0 {
		t.Errorf("p0 rates = %v/%v, want 250/0", p0.TSRxRate, p0.TSTxRate)
	}
	if p0.Disk != nil || p0.Load1 != nil || p0.NetRxRate != nil {
		t.Errorf("all-null metrics must be nil: %+v", p0)
	}

	p1 := ser.Points[1]
	if p1.T != base+60 || p1.Online != 0 || p1.Direct != nil || p1.LatencyMs != nil || p1.LatencyMax != nil || p1.CPU != nil || p1.CPUMax != nil {
		t.Errorf("p1 = %+v", p1)
	}
	p2 := ser.Points[2]
	if p2.T != base+300 || p2.Online != 1 {
		t.Errorf("p2 = %+v", p2)
	}
	approx("p2.Disk", p2.Disk, 90)
	approx("p2.Load1", p2.Load1, 1.25)
	approx("p2.NetRxRate", p2.NetRxRate, 10)
	approx("p2.NetTxRate", p2.NetTxRate, 20)

	// 48h range buckets at 300s: the first three samples groups collapse.
	ser, err = s.QuerySeries(ctx, "d1", at(0), at(48*3600))
	if err != nil {
		t.Fatal(err)
	}
	if ser.StepSec != 300 || len(ser.Points) != 2 || ser.Points[0].T != base || ser.Points[1].T != base+300 {
		t.Errorf("48h series = step %d points %+v", ser.StepSec, ser.Points)
	}
	if math.Abs(ser.Points[0].Online-0.5) > 1e-9 { // 3 online of 6
		t.Errorf("300s bucket online = %v, want 0.5", ser.Points[0].Online)
	}
}

func insertRollupRow(t *testing.T, s *Store, id string, bucket, samples int64, online float64, direct, cpu, cpuMax, lat *float64) {
	t.Helper()
	_, err := s.db.ExecContext(context.Background(), `INSERT OR REPLACE INTO rollups(device_id, bucket, step, samples, online_ratio, direct_ratio,
		latency_avg, latency_max, ts_rx_rate_avg, ts_tx_rate_avg, cpu_avg, cpu_max) VALUES (?, ?, 300, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, bucket, samples, online, nullFloat(direct), nullFloat(lat), nullFloat(lat), float64(samples), 1.0, nullFloat(cpu), nullFloat(cpuMax))
	if err != nil {
		t.Fatal(err)
	}
}

func TestQuerySeriesRollups(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	// Raw samples in range must be ignored for > 48h ranges.
	if err := s.InsertSamples(ctx, []model.Sample{{DeviceID: "d1", TS: at(0), Online: true, CPU: fp(99)}}); err != nil {
		t.Fatal(err)
	}
	insertRollupRow(t, s, "d1", base, 10, 1.0, fp(1.0), fp(10), fp(12), fp(100))
	insertRollupRow(t, s, "d1", base+300, 30, 0.5, nil, nil, nil, nil)
	insertRollupRow(t, s, "d1", base+600, 20, 0.0, fp(0.0), fp(40), fp(50), fp(20))
	insertRollupRow(t, s, "d1", base+1800, 5, 1.0, nil, nil, nil, nil)
	insertRollupRow(t, s, "d2", base, 5, 1.0, nil, nil, nil, nil)

	const day = 24 * 3600
	// 7d: rollups at native 300s step, each row is one point.
	ser, err := s.QuerySeries(ctx, "d1", at(0), at(7*day))
	if err != nil {
		t.Fatal(err)
	}
	if ser.Source != SourceRollup || ser.StepSec != 300 || len(ser.Points) != 4 {
		t.Fatalf("7d series = %s/%d with %d points", ser.Source, ser.StepSec, len(ser.Points))
	}
	if p := ser.Points[0]; p.T != base || p.Online != 1 || p.CPU == nil || *p.CPU != 10 || p.CPUMax == nil || *p.CPUMax != 12 || p.LatencyMax == nil || *p.LatencyMax != 100 || p.TSRxRate != 10 || p.TSTxRate != 1 {
		t.Errorf("7d p0 = %+v", p)
	}
	if p := ser.Points[1]; p.T != base+300 || p.Online != 0.5 || p.CPU != nil || p.Direct != nil {
		t.Errorf("7d p1 = %+v", p)
	}

	// 10d: re-bucketed to 1800s, weighted by sample counts.
	ser, err = s.QuerySeries(ctx, "d1", at(0), at(10*day))
	if err != nil {
		t.Fatal(err)
	}
	if ser.StepSec != 1800 || len(ser.Points) != 2 {
		t.Fatalf("10d series = step %d points %+v", ser.StepSec, ser.Points)
	}
	p := ser.Points[0]
	if p.T != base {
		t.Errorf("p.T = %d, want %d", p.T, base)
	}
	if want := 25.0 / 60.0; math.Abs(p.Online-want) > 1e-9 {
		t.Errorf("weighted online = %v, want %v", p.Online, want)
	}
	if p.CPU == nil || math.Abs(*p.CPU-30) > 1e-9 { // (10*10 + 40*20) / 30
		t.Errorf("weighted cpu = %v, want 30", p.CPU)
	}
	if p.CPUMax == nil || *p.CPUMax != 50 {
		t.Errorf("cpu max = %v, want 50", p.CPUMax)
	}
	if p.Direct == nil || math.Abs(*p.Direct-(10.0/30.0)) > 1e-9 { // (1*10 + 0*20) / 30
		t.Errorf("weighted direct = %v, want 1/3", p.Direct)
	}
	if p.LatencyMs == nil || math.Abs(*p.LatencyMs-(100*10+20*20)/30.0) > 1e-9 {
		t.Errorf("weighted latency = %v", p.LatencyMs)
	}
	if p.LatencyMax == nil || *p.LatencyMax != 100 {
		t.Errorf("latency max = %v, want 100", p.LatencyMax)
	}
	if want := (10*10 + 30*30 + 20*20) / 60.0; math.Abs(p.TSRxRate-want) > 1e-9 {
		t.Errorf("weighted rx = %v, want %v", p.TSRxRate, want)
	}
	if p2 := ser.Points[1]; p2.T != base+1800 || p2.Online != 1 || p2.CPU != nil {
		t.Errorf("p2 = %+v", p2)
	}

	// 30d: 3600s buckets, everything collapses into one point.
	ser, err = s.QuerySeries(ctx, "d1", at(0), at(30*day))
	if err != nil {
		t.Fatal(err)
	}
	if ser.StepSec != 3600 || len(ser.Points) != 1 || ser.Points[0].T != base {
		t.Errorf("30d series = step %d points %+v", ser.StepSec, ser.Points)
	}
	if want := 30.0 / 65.0; math.Abs(ser.Points[0].Online-want) > 1e-9 {
		t.Errorf("30d online = %v, want %v", ser.Points[0].Online, want)
	}
}

func TestNetworkSparklines(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	samples := []model.Sample{
		{DeviceID: "d1", TS: at(0), Online: true, LatencyMs: fp(10), TSRxRate: 100, TSTxRate: 10},
		{DeviceID: "d1", TS: at(30), Online: true, LatencyMs: fp(20), TSRxRate: 200, TSTxRate: 30},
		{DeviceID: "d2", TS: at(10), Online: true, LatencyMs: fp(30), TSRxRate: 50, TSTxRate: 5},
		{DeviceID: "d3", TS: at(20), Online: false, LatencyMs: fp(999), TSRxRate: 0}, // offline: latency ignored
		{DeviceID: "d2", TS: at(70), Online: false, TSRxRate: 0},
		{DeviceID: "d1", TS: at(130), Online: true}, // online, no latency
		{DeviceID: "d1", TS: at(180), Online: true}, // == to, excluded
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	sp, err := s.NetworkSparklines(ctx, at(0), at(180), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wantT := []int64{base, base + 60, base + 120}
	if len(sp.T) != 3 || len(sp.Online) != 3 || len(sp.RxRate) != 3 || len(sp.TxRate) != 3 || len(sp.Latency) != 3 {
		t.Fatalf("array lengths = %d/%d/%d/%d/%d, want 3", len(sp.T), len(sp.Online), len(sp.RxRate), len(sp.TxRate), len(sp.Latency))
	}
	for i := range wantT {
		if sp.T[i] != wantT[i] {
			t.Errorf("T[%d] = %d, want %d", i, sp.T[i], wantT[i])
		}
	}
	if sp.Online[0] != 2 || sp.Online[1] != 0 || sp.Online[2] != 1 {
		t.Errorf("Online = %v, want [2 0 1]", sp.Online)
	}
	if sp.RxRate[0] != 200 || sp.TxRate[0] != 25 { // d1 avg 150 + d2 50; d1 avg 20 + d2 5
		t.Errorf("rates[0] = %v/%v, want 200/25", sp.RxRate[0], sp.TxRate[0])
	}
	if sp.RxRate[1] != 0 || sp.RxRate[2] != 0 {
		t.Errorf("rates = %v", sp.RxRate)
	}
	if sp.Latency[0] != 20 || sp.Latency[1] != 0 || sp.Latency[2] != 0 {
		t.Errorf("Latency = %v, want [20 0 0]", sp.Latency)
	}

	// Unaligned from: buckets start at the floor and the arrays still align.
	sp, err = s.NetworkSparklines(ctx, at(30), at(150), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.T) != 3 || sp.T[0] != base || sp.T[2] != base+120 {
		t.Errorf("unaligned T = %v", sp.T)
	}
	if sp.Online[0] != 1 || sp.Latency[0] != 20 || sp.RxRate[0] != 200 { // only d1@+30 is >= from
		t.Errorf("unaligned bucket0 = online %v latency %v rx %v", sp.Online[0], sp.Latency[0], sp.RxRate[0])
	}

	// No data / degenerate ranges: empty non-nil arrays or zeros.
	sp, err = s.NetworkSparklines(ctx, at(100), at(0), time.Minute)
	if err != nil || sp.T == nil || len(sp.T) != 0 || sp.Online == nil {
		t.Errorf("degenerate = %+v, %v", sp, err)
	}
	sp, err = s.NetworkSparklines(ctx, at(7200), at(7320), 0) // zero step -> 60s
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.T) != 2 || sp.Online[0] != 0 || sp.Latency[1] != 0 {
		t.Errorf("no-data window = %+v", sp)
	}
	if _, err := s.NetworkSparklines(ctx, at(0), at(int64(maxSparklineBuckets)*60+60), time.Second); err == nil {
		t.Error("oversized bucket count accepted")
	}
}
