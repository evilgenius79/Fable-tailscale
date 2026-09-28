package store

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

type rollupRow struct {
	bucket, step, samples int64
	online                float64
	cpu                   *float64
}

func readRollups(t *testing.T, s *Store, id string) []rollupRow {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), `SELECT bucket, step, samples, online_ratio, cpu_avg FROM rollups WHERE device_id = ? ORDER BY bucket`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rollupRow
	for rows.Next() {
		var r rollupRow
		var cpu *float64
		if err := rows.Scan(&r.bucket, &r.step, &r.samples, &r.online, &cpu); err != nil {
			t.Fatal(err)
		}
		r.cpu = cpu
		out = append(out, r)
	}
	return out
}

func TestRollupIdempotentAndWatermark(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	// Two full 5-minute buckets (20 samples each) plus a partial third.
	var samples []model.Sample
	for i := int64(0); i < 43; i++ {
		ts := i * 15
		samples = append(samples, model.Sample{DeviceID: "d1", TS: at(ts), Online: i%2 == 0, CPU: fp(float64(i))})
		if i < 5 {
			samples = append(samples, model.Sample{DeviceID: "d2", TS: at(ts), Online: true})
		}
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}

	n, err := s.Rollup(ctx, at(650))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if n != 4 { // d1: 3 buckets, d2: 1 bucket
		t.Errorf("Rollup wrote %d buckets, want 4", n)
	}
	got := readRollups(t, s, "d1")
	if len(got) != 3 || got[0].bucket != base || got[1].bucket != base+300 || got[2].bucket != base+600 {
		t.Fatalf("rollup buckets = %+v", got)
	}
	if got[0].samples != 20 || got[0].step != 300 || got[0].online != 0.5 || got[0].cpu == nil || *got[0].cpu != 9.5 {
		t.Errorf("bucket A = %+v", got[0])
	}
	if got[2].samples != 3 { // samples at +600, +615, +630
		t.Errorf("partial bucket C samples = %d, want 3", got[2].samples)
	}
	wm, ok, err := s.GetKV(ctx, kvRollupWatermark)
	if err != nil || !ok {
		t.Fatalf("watermark missing: %v %v", ok, err)
	}
	if want := time.Unix(base+600, 0).Unix(); wm != itoa(want) {
		t.Errorf("watermark = %s, want %d (start of partial bucket)", wm, want)
	}

	// Nothing new: only the partial bucket is recomputed (unchanged content).
	n, err = s.Rollup(ctx, at(650))
	if err != nil {
		t.Fatal(err)
	}
	if n > 1 {
		t.Errorf("re-run wrote %d buckets, want <= 1", n)
	}
	if again := readRollups(t, s, "d1"); len(again) != 3 || again[2].samples != 3 {
		t.Errorf("re-run changed rollups: %+v", again)
	}
	// A horizon behind the watermark (clock went backwards) triggers a
	// rescan that leaves existing rollups intact and moves the watermark
	// back to the horizon's bucket.
	if _, err := s.Rollup(ctx, at(100)); err != nil {
		t.Errorf("Rollup older than watermark: %v", err)
	}
	if again := readRollups(t, s, "d1"); len(again) != 3 || again[0].samples != 20 || again[2].samples != 3 {
		t.Errorf("rescan behind watermark changed rollups: %+v", again)
	}
	if wm, _, _ := s.GetKV(ctx, kvRollupWatermark); wm != itoa(base) {
		t.Errorf("watermark after rescan = %s, want %d", wm, base)
	}

	// More samples arrive for the partial bucket; it is completed later.
	var more []model.Sample
	for ts := int64(645); ts < 900; ts += 15 {
		more = append(more, model.Sample{DeviceID: "d1", TS: at(ts), Online: true})
	}
	if err := s.InsertSamples(ctx, more); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollup(ctx, at(900)); err != nil {
		t.Fatal(err)
	}
	got = readRollups(t, s, "d1")
	if len(got) != 3 || got[2].samples != 20 {
		t.Errorf("completed bucket C = %+v", got)
	}

	// Without the watermark a full re-scan yields identical rows.
	before := readRollups(t, s, "d1")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, kvRollupWatermark); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollup(ctx, at(900)); err != nil {
		t.Fatal(err)
	}
	after := readRollups(t, s, "d1")
	if len(before) != len(after) {
		t.Fatalf("rescan changed row count: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i].bucket != after[i].bucket || before[i].samples != after[i].samples || before[i].online != after[i].online {
			t.Errorf("rescan changed bucket %d: %+v -> %+v", i, before[i], after[i])
		}
	}

	// A bucket whose raw samples were partially pruned never degrades.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE device_id = 'd1' AND ts < ?`, base+150); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, kvRollupWatermark); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollup(ctx, at(900)); err != nil {
		t.Fatal(err)
	}
	got = readRollups(t, s, "d1")
	if got[0].samples != 20 || got[0].online != 0.5 {
		t.Errorf("degraded bucket A after partial prune: %+v", got[0])
	}

	// A malformed watermark is ignored rather than failing.
	if err := s.SetKV(ctx, kvRollupWatermark, "garbage"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollup(ctx, at(900)); err != nil {
		t.Errorf("Rollup with malformed watermark: %v", err)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestRollupWatermarkAheadOfHorizon: a watermark in the future (the hub ran
// with a wrong clock) must not stall rollups until real time catches up.
func TestRollupWatermarkAheadOfHorizon(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	var samples []model.Sample
	for ts := int64(0); ts < 645; ts += 15 {
		samples = append(samples, model.Sample{DeviceID: "d1", TS: at(ts), Online: true})
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKV(ctx, kvRollupWatermark, itoa(base+30*86_400)); err != nil {
		t.Fatal(err)
	}
	n, err := s.Rollup(ctx, at(650))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if n != 3 {
		t.Errorf("Rollup wrote %d buckets, want 3 (full rescan)", n)
	}
	got := readRollups(t, s, "d1")
	if len(got) != 3 || got[0].samples != 20 || got[1].samples != 20 || got[2].samples != 3 {
		t.Errorf("rollups after rescan = %+v", got)
	}
	if wm, ok, err := s.GetKV(ctx, kvRollupWatermark); err != nil || !ok || wm != itoa(base+600) {
		t.Errorf("watermark = %q %v %v, want %d", wm, ok, err, base+600)
	}
}

// TestPruneKeepsUnrolledSamples: raw samples at or after the rollup watermark
// belong to a bucket that Rollup still has to (re)compute, so Prune keeps
// them even when they are older than the raw retention.
func TestPruneKeepsUnrolledSamples(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	now := fixedNow
	if err := s.InsertSamples(ctx, []model.Sample{
		{DeviceID: "d1", TS: now.Add(-3 * time.Hour)},
		{DeviceID: "d1", TS: now.Add(-90 * time.Minute)}, // >= watermark: kept
		{DeviceID: "d1", TS: now.Add(-30 * time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKV(ctx, kvRollupWatermark, itoa(now.Add(-90*time.Minute).Unix())); err != nil {
		t.Fatal(err)
	}
	res, err := s.Prune(ctx, time.Hour, 0, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Samples != 1 {
		t.Errorf("pruned %d samples, want 1", res.Samples)
	}
	st, err := s.Stats(ctx)
	if err != nil || st.Samples != 2 {
		t.Errorf("samples left = %d, %v; want 2", st.Samples, err)
	}
	// A watermark older than the cutoff is the only thing that narrows the
	// deletion; a watermark newer than the cutoff leaves retention alone.
	if err := s.SetKV(ctx, kvRollupWatermark, itoa(now.Unix())); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Prune(ctx, time.Hour, 0, 0); err != nil || res.Samples != 1 {
		t.Errorf("Prune with newer watermark = %+v, %v; want 1 sample", res, err)
	}
}

// TestMaintainCycleKeepsBucketsComplete replays the collector's hourly
// Rollup(now-1h)+Prune(raw=1h) cycle: with the raw retention equal to the
// rollup lag, the partial bucket straddling the horizon must still be
// completed from all its samples on the next run.
func TestMaintainCycleKeepsBucketsComplete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	t0 := fixedNow
	var samples []model.Sample
	for ts := t0.Add(-4 * time.Hour); ts.Before(t0.Add(3 * time.Hour)); ts = ts.Add(15 * time.Second) {
		samples = append(samples, model.Sample{DeviceID: "d1", TS: ts, Online: true})
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{t0.Add(12 * time.Minute), t0.Add(72 * time.Minute), t0.Add(132 * time.Minute)} {
		s.now = func() time.Time { return now }
		if _, err := s.Rollup(ctx, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Prune(ctx, time.Hour, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Every bucket completed before the last horizon (t0+72m -> buckets up
	// to t0+65m) must be built from all 20 samples.
	horizon := t0.Add(70 * time.Minute).Unix()
	for _, r := range readRollups(t, s, "d1") {
		if r.bucket < horizon && r.samples != 20 {
			t.Errorf("bucket %s has %d samples, want 20", time.Unix(r.bucket, 0).UTC().Format("15:04"), r.samples)
		}
	}
	// The samples of the bucket at the watermark survived pruning.
	var left int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples WHERE ts >= ? AND ts < ?`, horizon, horizon+300).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 20 {
		t.Errorf("%d raw samples left in the watermark bucket, want 20", left)
	}
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	now := fixedNow
	h := func(hours float64) time.Time { return now.Add(-time.Duration(hours * float64(time.Hour))) }

	if err := s.InsertSamples(ctx, []model.Sample{
		{DeviceID: "d1", TS: h(3)}, {DeviceID: "d1", TS: h(1)}, {DeviceID: "d2", TS: h(2.5)},
	}); err != nil {
		t.Fatal(err)
	}
	for _, b := range []time.Time{h(240), h(24)} {
		insertRollupRow(t, s, "d1", b.Unix(), 1, 1, nil, nil, nil, nil)
	}
	for _, ts := range []time.Time{h(240), h(1)} {
		if err := s.InsertEvent(ctx, &model.Event{TS: ts, Type: model.EventDeviceOnline}); err != nil {
			t.Fatal(err)
		}
	}
	oldResolved := h(240)
	newResolved := h(1)
	alerts := []*model.Alert{
		{RuleID: "r", State: model.AlertResolved, OpenedAt: h(300), ResolvedAt: &oldResolved},
		{RuleID: "r", State: model.AlertResolved, OpenedAt: h(300), ResolvedAt: &newResolved},
		{RuleID: "r", State: model.AlertOpen, OpenedAt: h(300)}, // open alerts are never pruned
	}
	for _, a := range alerts {
		if err := s.OpenAlert(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	// Zero retention disables that category.
	res, err := s.Prune(ctx, 0, 0, 0)
	if err != nil || res != (PruneResult{}) {
		t.Errorf("Prune(0,0,0) = %+v, %v", res, err)
	}

	res, err = s.Prune(ctx, 2*time.Hour, 5*24*time.Hour, 5*24*time.Hour)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	want := PruneResult{Samples: 2, Rollups: 1, Events: 1, Alerts: 1}
	if res != want {
		t.Errorf("Prune = %+v, want %+v", res, want)
	}
	if res.Total() != 5 {
		t.Errorf("Total = %d, want 5", res.Total())
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Samples != 1 || st.Rollups != 1 || st.Events != 1 || st.Alerts != 2 {
		t.Errorf("after prune stats = %+v", st)
	}
	open, err := s.OpenAlertsByKey(ctx)
	if err != nil || len(open) != 1 {
		t.Errorf("open alerts after prune = %d, %v", len(open), err)
	}
}

func TestPruneVacuumThreshold(t *testing.T) {
	// Exercise the VACUUM path on a file database with a lowered threshold
	// by pruning more than the threshold's worth of rows.
	ctx := context.Background()
	s := newTestStore(t, t.TempDir()+"/v.db")
	var samples []model.Sample
	for i := 0; i < 1200; i++ {
		samples = append(samples, model.Sample{DeviceID: "d", TS: fixedNow.Add(-time.Duration(i+100) * time.Hour)})
	}
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	res, err := s.Prune(ctx, time.Hour, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Samples != 1200 {
		t.Errorf("pruned %d, want 1200", res.Samples)
	}
	s.vacuum(ctx) // must not fail or deadlock on a file DB
	if _, err := s.Stats(ctx); err != nil {
		t.Errorf("store unusable after vacuum: %v", err)
	}
}

func TestUptimeRatios(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")
	since := at(0)

	// d1: raw samples from +700 (3 of 4 online) plus rollups entirely before
	// the oldest raw sample (+0: 10 samples half online, +300: 10 samples all
	// online); the bucket at +600 overlaps the raw range and is excluded.
	if err := s.InsertSamples(ctx, []model.Sample{
		{DeviceID: "d1", TS: at(700), Online: true}, {DeviceID: "d1", TS: at(715), Online: true},
		{DeviceID: "d1", TS: at(730), Online: false}, {DeviceID: "d1", TS: at(745), Online: true},
		{DeviceID: "d1", TS: at(-100), Online: false}, // before since: ignored
		{DeviceID: "d3", TS: at(10), Online: false}, {DeviceID: "d3", TS: at(25), Online: false},
	}); err != nil {
		t.Fatal(err)
	}
	insertRollupRow(t, s, "d1", base, 10, 0.5, nil, nil, nil, nil)
	insertRollupRow(t, s, "d1", base+300, 10, 1.0, nil, nil, nil, nil)
	insertRollupRow(t, s, "d1", base+600, 10, 0.0, nil, nil, nil, nil)
	insertRollupRow(t, s, "d1", base-300, 10, 0.0, nil, nil, nil, nil) // before since
	insertRollupRow(t, s, "d2", base, 10, 1.0, nil, nil, nil, nil)
	insertRollupRow(t, s, "d2", base+300, 30, 0.9, nil, nil, nil, nil)

	got, err := s.UptimeRatios(ctx, since)
	if err != nil {
		t.Fatalf("UptimeRatios: %v", err)
	}
	want := map[model.DeviceID]float64{
		"d1": (3 + 5 + 10) / 24.0,
		"d2": (10 + 27) / 40.0,
		"d3": 0,
	}
	if len(got) != len(want) {
		t.Errorf("devices = %v, want %v", got, want)
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Errorf("%s missing", id)
			continue
		}
		if math.Abs(g-w) > 1e-9 {
			t.Errorf("%s = %v, want %v", id, g, w)
		}
	}
	if _, ok := got["d4"]; ok {
		t.Error("device without data present")
	}

	// A window that only raw samples cover ignores rollups entirely.
	got, err = s.UptimeRatios(ctx, at(700))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || math.Abs(got["d1"]-0.75) > 1e-9 {
		t.Errorf("raw-only window = %v, want d1=0.75", got)
	}

	// Empty store gives an empty, non-nil map.
	empty := newTestStore(t, "")
	got, err = empty.UptimeRatios(ctx, since)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty = %v, %v", got, err)
	}
}

func TestBuildTimeline(t *testing.T) {
	seg := func(from, to int64, online bool) model.TimelineSegment {
		return model.TimelineSegment{From: fromUnix(base + from), To: fromUnix(base + to), Online: online}
	}
	pt := func(t int64, online bool, interval int64) tlPoint {
		return tlPoint{t: base + t, online: online, interval: interval}
	}
	tests := []struct {
		name        string
		points      []tlPoint
		end         int64
		want        []model.TimelineSegment
		online, cov int64
	}{
		{name: "empty", end: 100, want: []model.TimelineSegment{}},
		{
			name:   "single point extends to end",
			points: []tlPoint{pt(0, true, 15)},
			end:    30,
			want:   []model.TimelineSegment{seg(0, 30, true)},
			online: 30, cov: 30,
		},
		{
			name:   "merge same state, count transitions",
			points: []tlPoint{pt(0, true, 15), pt(15, true, 15), pt(30, false, 15), pt(45, false, 15), pt(60, true, 15)},
			end:    75,
			want:   []model.TimelineSegment{seg(0, 30, true), seg(30, 60, false), seg(60, 75, true)},
			online: 45, cov: 75,
		},
		{
			name:   "gap longer than 3x interval is offline",
			points: []tlPoint{pt(0, true, 15), pt(15, true, 15), pt(300, true, 15)},
			end:    315,
			want:   []model.TimelineSegment{seg(0, 30, true), seg(30, 300, false), seg(300, 315, true)},
			online: 45, cov: 315,
		},
		{
			name:   "gap of exactly 3x interval is not a gap",
			points: []tlPoint{pt(0, true, 15), pt(45, true, 15)},
			end:    60,
			want:   []model.TimelineSegment{seg(0, 60, true)},
			online: 60, cov: 60,
		},
		{
			name:   "trailing gap to end is offline",
			points: []tlPoint{pt(0, true, 15)},
			end:    600,
			want:   []model.TimelineSegment{seg(0, 15, true), seg(15, 600, false)},
			online: 15, cov: 600,
		},
		{
			name:   "gap merges into adjacent offline",
			points: []tlPoint{pt(0, false, 15), pt(500, false, 15)},
			end:    515,
			want:   []model.TimelineSegment{seg(0, 515, false)},
			online: 0, cov: 515,
		},
		{
			name:   "rollup then raw with different intervals",
			points: []tlPoint{pt(0, true, 300), pt(300, false, 300), pt(900, true, 15), pt(915, true, 15)},
			end:    930,
			want:   []model.TimelineSegment{seg(0, 300, true), seg(300, 900, false), seg(900, 930, true)},
			online: 330, cov: 930,
		},
		{
			name:   "point at end contributes nothing",
			points: []tlPoint{pt(0, true, 15), pt(15, true, 15)},
			end:    15,
			want:   []model.TimelineSegment{seg(0, 15, true)},
			online: 15, cov: 15,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			segs, online, cov := buildTimeline(tc.points, base+tc.end)
			if segs == nil {
				t.Fatal("segments nil")
			}
			if len(segs) != len(tc.want) {
				t.Fatalf("segments = %+v, want %+v", segs, tc.want)
			}
			for i := range segs {
				if !segs[i].From.Equal(tc.want[i].From) || !segs[i].To.Equal(tc.want[i].To) || segs[i].Online != tc.want[i].Online {
					t.Errorf("segment %d = %v..%v %v, want %v..%v %v", i, segs[i].From.Unix()-base, segs[i].To.Unix()-base, segs[i].Online,
						tc.want[i].From.Unix()-base, tc.want[i].To.Unix()-base, tc.want[i].Online)
				}
			}
			if online != tc.online || cov != tc.cov {
				t.Errorf("online/covered = %d/%d, want %d/%d", online, cov, tc.online, tc.cov)
			}
		})
	}
}

func TestOnlineTimeline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	// d1: raw only, 10 samples: 5 online, 3 offline, 2 online.
	var samples []model.Sample
	for i := int64(0); i < 10; i++ {
		online := i < 5 || i >= 8
		samples = append(samples, model.Sample{DeviceID: "d1", TS: at(3600 + i*15), Online: online})
	}
	// d2: raw with a long gap.
	samples = append(samples,
		model.Sample{DeviceID: "d2", TS: at(3600), Online: true},
		model.Sample{DeviceID: "d2", TS: at(3615), Online: true},
		model.Sample{DeviceID: "d2", TS: at(3900), Online: true},
	)
	// d4: rollups before raw, all online.
	samples = append(samples,
		model.Sample{DeviceID: "d4", TS: at(900), Online: true},
		model.Sample{DeviceID: "d4", TS: at(915), Online: true},
	)
	if err := s.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	// d3: rollups only.
	insertRollupRow(t, s, "d3", base, 20, 1.0, nil, nil, nil, nil)
	insertRollupRow(t, s, "d3", base+300, 20, 0.4, nil, nil, nil, nil)
	insertRollupRow(t, s, "d3", base+600, 20, 0.6, nil, nil, nil, nil)
	// d4 rollups: +0, +300 usable; +600 ends exactly at first raw (usable);
	// +900 overlaps raw and must be ignored.
	for _, b := range []int64{0, 300, 600, 900} {
		insertRollupRow(t, s, "d4", base+b, 20, 1.0, nil, nil, nil, nil)
	}

	tests := []struct {
		name     string
		id       model.DeviceID
		from, to time.Time
		segments []model.TimelineSegment
		pct      float64
		outages  int
	}{
		{
			name: "raw continuous", id: "d1", from: at(3600), to: at(3750),
			segments: []model.TimelineSegment{
				{From: at(3600), To: at(3675), Online: true},
				{From: at(3675), To: at(3720), Online: false},
				{From: at(3720), To: at(3750), Online: true},
			},
			pct: 70, outages: 1,
		},
		{
			name: "raw with gap", id: "d2", from: at(3600), to: at(3915),
			segments: []model.TimelineSegment{
				{From: at(3600), To: at(3630), Online: true},
				{From: at(3630), To: at(3900), Online: false},
				{From: at(3900), To: at(3915), Online: true},
			},
			pct: 100 * 45.0 / 315.0, outages: 1,
		},
		{
			name: "rollups only", id: "d3", from: at(0), to: at(900),
			segments: []model.TimelineSegment{
				{From: at(0), To: at(300), Online: true},
				{From: at(300), To: at(600), Online: false},
				{From: at(600), To: at(900), Online: true},
			},
			pct: 100 * 600.0 / 900.0, outages: 1,
		},
		{
			name: "rollups then raw merge", id: "d4", from: at(0), to: at(930),
			segments: []model.TimelineSegment{{From: at(0), To: at(930), Online: true}},
			pct:      100, outages: 0,
		},
		{
			name: "no data", id: "nobody", from: at(0), to: at(900),
			segments: []model.TimelineSegment{}, pct: 0, outages: 0,
		},
		{
			name: "degenerate range", id: "d1", from: at(900), to: at(0),
			segments: []model.TimelineSegment{}, pct: 0, outages: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := s.OnlineTimeline(ctx, tc.id, tc.from, tc.to)
			if err != nil {
				t.Fatalf("OnlineTimeline: %v", err)
			}
			if rep.DeviceID != tc.id || !rep.From.Equal(tc.from) || !rep.To.Equal(tc.to) {
				t.Errorf("meta = %s %v..%v", rep.DeviceID, rep.From, rep.To)
			}
			if rep.Segments == nil {
				t.Fatal("Segments nil")
			}
			if len(rep.Segments) != len(tc.segments) {
				t.Fatalf("segments = %+v, want %+v", rep.Segments, tc.segments)
			}
			for i, sg := range rep.Segments {
				w := tc.segments[i]
				if !sg.From.Equal(w.From) || !sg.To.Equal(w.To) || sg.Online != w.Online {
					t.Errorf("segment %d = %v..%v %v, want %v..%v %v", i, sg.From, sg.To, sg.Online, w.From, w.To, w.Online)
				}
			}
			if math.Abs(rep.Pct-tc.pct) > 1e-9 {
				t.Errorf("Pct = %v, want %v", rep.Pct, tc.pct)
			}
			if rep.Outages != tc.outages {
				t.Errorf("Outages = %d, want %d", rep.Outages, tc.outages)
			}
		})
	}
}
