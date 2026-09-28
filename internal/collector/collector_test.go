package collector

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

func equalAny(a, b any) bool { return reflect.DeepEqual(a, b) }

func TestOverviewCounts(t *testing.T) {
	cfg := defaultCfg()
	cfg.PingInterval = 30 * time.Second
	h := newHarness(t, cfg)
	h.api.configured = true
	soon := baseTime.Add(3 * 24 * time.Hour)
	far := baseTime.Add(30 * 24 * time.Hour)
	h.api.setDevices(
		source.APIDevice{NodeID: "unauth", Name: "pending.tail.ts.net", Authorized: false, LastSeen: baseTime, OS: "windows", User: "dave@example.com"},
		source.APIDevice{NodeID: "b", Name: "bravo.tail.ts.net", Authorized: true, UpdateAvailable: true, EnabledRoutes: []string{"192.168.1.0/24"}},
		source.APIDevice{NodeID: "f", Name: "foxtrot.tail.ts.net", Authorized: true, Expires: &soon, KeyExpiryDisabled: true},
	)
	h.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1")),
		peer("b", "bravo", "100.64.0.3", relay("nyc"), osName("macOS"), user("bob@example.com")),
		peer("c", "charlie", "100.64.0.4", offline(), osName("iOS")),
		peer("e", "echo", "100.64.0.6", keyExpiry(soon), tags("tag:server"), exitNodeOpt()),
		peer("f", "foxtrot", "100.64.0.7", keyExpiry(far), primaryRoutes("10.1.0.0/16")),
	))
	h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 10, Endpoint: "1.1.1.1:1"})
	h.local.setPing("100.64.0.3", source.PingReply{LatencyMs: 30, DERPRegionID: 1, DERPRegionCode: "nyc"})
	h.agents.setReport("100.64.0.2", report(baseTime, 1, 1))
	if err := h.st.OpenAlert(t.Context(), &model.Alert{RuleID: "disk_full", Severity: model.SeverityCritical, State: model.AlertOpen, DeviceID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.OpenAlert(t.Context(), &model.Alert{RuleID: "high_cpu", Severity: model.SeverityWarning, State: model.AlertOpen, DeviceID: "a"}); err != nil {
		t.Fatal(err)
	}
	resolved := &model.Alert{RuleID: "high_load", Severity: model.SeverityWarning, State: model.AlertResolved, DeviceID: "a"}
	if err := h.st.OpenAlert(t.Context(), resolved); err != nil {
		t.Fatal(err)
	}
	h.poll()

	ov := h.c.Snapshot().Overview
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Devices", ov.Devices, 7},
		// The unauthorized API-only device was seen just now, so it counts
		// as online: self + a + b + e + f + unauth; only c is offline.
		{"Online", ov.Online, 6},
		{"Offline", ov.Offline, 1},
		{"Direct", ov.Direct, 1},
		{"Relayed", ov.Relayed, 1},
		{"AgentsUp", ov.AgentsUp, 1},
		{"UpdatesPending", ov.UpdatesPending, 1},
		{"KeysExpiring", ov.KeysExpiring, 1},
		{"Unauthorized", ov.Unauthorized, 1},
		{"ExitNodes", ov.ExitNodes, 1},
		{"SubnetRouters", ov.SubnetRouters, 2},
		{"OpenAlerts", ov.OpenAlerts, 2},
		{"CriticalAlerts", ov.CriticalAlerts, 1},
		{"AvgLatency", floatVal(ov.AvgLatencyMs), 20.0},
		{"OS linux", ov.OSBreakdown["linux"], 4},
		{"OS macOS", ov.OSBreakdown["macOS"], 1},
		{"OS iOS", ov.OSBreakdown["iOS"], 1},
		{"OS windows", ov.OSBreakdown["windows"], 1},
		{"User alice", ov.UserBreakdown["alice@example.com"], 4},
		{"User bob", ov.UserBreakdown["bob@example.com"], 1},
		{"User dave", ov.UserBreakdown["dave@example.com"], 1},
		{"User tagged", ov.UserBreakdown["tagged"], 1},
		{"Hub self", ov.Hub.SelfID, model.DeviceID("self")},
	}
	for _, c := range checks {
		if !equalAny(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if len(ov.Sparklines.T) == 0 {
		t.Errorf("sparklines should have buckets after samples were written")
	}
}

func TestTopology(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"), bytes(100, 200)),
		peer("b", "bravo", "100.64.0.3", relay("nyc"), tags("tag:server"), exitNodeOpt()),
		peer("c", "charlie", "100.64.0.4", relay("zzz"), primaryRoutes("10.0.0.0/8")),
		peer("d", "delta", "100.64.0.5", offline()),
	))
	h.poll()
	top := h.c.Topology()
	if len(top.Nodes) != 5 || len(top.Edges) != 4 {
		t.Fatalf("nodes=%d edges=%d", len(top.Nodes), len(top.Edges))
	}
	for _, e := range top.Edges {
		if e.From != "self" || e.To == "self" {
			t.Errorf("edge endpoints: %+v", e)
		}
	}
	byID := map[model.DeviceID]model.TopologyNode{}
	for _, n := range top.Nodes {
		byID[n.ID] = n
	}
	if !byID["self"].IsSelf || !byID["b"].IsExit || !byID["c"].IsRouter || byID["d"].Online || byID["b"].Relay != "nyc" || byID["a"].Path != model.PathDirect {
		t.Errorf("nodes: %+v", byID)
	}
	if byID["b"].Tags == nil || byID["a"].Tags == nil {
		t.Errorf("tags must be non-nil")
	}
	want := map[string]string{"nyc": "New York City", "zzz": "zzz"}
	if !reflect.DeepEqual(top.DERPRegions, want) {
		t.Errorf("DERPRegions=%v, want %v", top.DERPRegions, want)
	}
	var edgeA model.TopologyEdge
	for _, e := range top.Edges {
		if e.To == "a" {
			edgeA = e
		}
	}
	if edgeA.Path != model.PathDirect || edgeA.RxRate != 0 {
		t.Errorf("edge a: %+v", edgeA)
	}
}

func TestDERPRegionName(t *testing.T) {
	cases := map[string]string{"nyc": "New York City", "FRA": "Frankfurt", " sao ": "São Paulo", "xyz": "xyz", "": ""}
	for code, want := range cases {
		if got := DERPRegionName(code); got != want {
			t.Errorf("DERPRegionName(%q)=%q, want %q", code, got, want)
		}
	}
}

func TestPingNow(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(
		peer("a", "Alpha", "100.64.0.2", relay("nyc")),
		peer("b", "bravo", "100.64.0.3"),
		peer("n", "noip", "", ips()),
	))
	h.poll()
	h.drainEvents()
	ctx := t.Context()

	h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 7.25, Endpoint: "8.8.8.8:41641"})
	res, err := h.c.PingNow(ctx, "alpha") // by name, case-insensitive
	if err != nil {
		t.Fatalf("PingNow: %v", err)
	}
	if res.DeviceID != "a" || res.IP != "100.64.0.2" || res.LatencyMs != 7.25 || res.Path != model.PathDirect || res.Endpoint != "8.8.8.8:41641" || res.Err != "" || !res.At.Equal(baseTime) {
		t.Errorf("result: %+v", res)
	}
	d := h.device("a")
	if floatVal(d.Connectivity.LatencyMs) != 7.25 || d.Connectivity.Path != model.PathDirect || d.Connectivity.Relay != "" || d.Connectivity.LastPing == nil {
		t.Errorf("in-memory connectivity not updated: %+v", d.Connectivity)
	}
	if evs := h.drainEvents(); countType(evs, model.EventPathChanged) != 1 {
		t.Errorf("relay→direct via PingNow should emit path_changed: %v", eventTypes(evs))
	}

	h.local.setPing("100.64.0.3", source.PingReply{Err: "no matching peer"})
	res, err = h.c.PingNow(ctx, "b")
	if err != nil || res.Err != "no matching peer" || res.LatencyMs != 0 {
		t.Errorf("ping failure must be reported in Err: %+v, %v", res, err)
	}
	if d := h.device("b"); d.Connectivity.LatencyMs != nil {
		t.Errorf("failed ping must not touch connectivity")
	}

	h.local.setPingErr("100.64.0.3", errTest)
	if _, err := h.c.PingNow(ctx, "b"); err == nil || !errors.Is(err, errTest) {
		t.Errorf("LocalAPI failure must be returned as error: %v", err)
	}
	h.local.setPingErr("100.64.0.3", context.DeadlineExceeded)
	if res, err := h.c.PingNow(ctx, "b"); err != nil || !strings.Contains(res.Err, "timed out") {
		t.Errorf("timeout should be reported in Err: %+v %v", res, err)
	}

	if _, err := h.c.PingNow(ctx, "nope"); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("unknown device: %v", err)
	}
	if res, err := h.c.PingNow(ctx, "self"); err != nil || res.Err == "" {
		t.Errorf("self: %+v %v", res, err)
	}
	if res, err := h.c.PingNow(ctx, "n"); err != nil || res.Err == "" {
		t.Errorf("device without ip: %+v %v", res, err)
	}
}

func TestDeviceLookup(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "Alpha", "100.64.0.2")))
	h.poll()
	cases := []struct {
		id   string
		want bool
	}{
		{"a", true}, {"alpha", true}, {"ALPHA", true}, {"alpha.tail.ts.net", true}, {"Alpha.tail.ts.net.", true},
		{"hub", true}, {"self", true}, {"", false}, {"zzz", false}, {"alp", false},
	}
	for _, tc := range cases {
		if _, ok := h.c.Device(model.DeviceID(tc.id)); ok != tc.want {
			t.Errorf("Device(%q)=%v, want %v", tc.id, ok, tc.want)
		}
	}
}

func TestHubStatusError(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"), bytes(10, 10)), peer("b", "bravo", "100.64.0.3", offline())))
	h.poll()
	h.drainEvents()
	h.drainTicks()
	before, err := h.st.LatestSample(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}

	h.local.setStatusErr(errors.New("tailscaled: connect: no such file"))
	h.pollAfter(15 * time.Second)
	hub := h.c.Hub()
	if !strings.Contains(hub.LastError, "no such file") || !hub.LastPoll.Equal(h.now) {
		t.Errorf("hub after failure: %+v", hub)
	}
	snap := h.c.Snapshot()
	if len(snap.Devices) != 3 {
		t.Fatalf("devices must be kept: %d", len(snap.Devices))
	}
	for _, d := range snap.Devices {
		if d.Connectivity.Path != model.PathUnknown {
			t.Errorf("%s path=%v, want unknown", d.Name, d.Connectivity.Path)
		}
	}
	if d := h.device("a"); !d.Online || d.Connectivity.RxBytes != 10 {
		t.Errorf("other fields must be kept: %+v", d.Connectivity)
	}
	if ticks := h.drainTicks(); len(ticks) != 1 || ticks[0].Overview.Hub.LastError == "" {
		t.Errorf("degraded poll must publish a tick with the error: %d", len(ticks))
	}
	evs := h.drainEvents()
	if countType(evs, model.EventHubError) != 1 || len(evs) != 1 {
		t.Fatalf("hub.error events: %v", eventTypes(evs))
	}
	if after, err := h.st.LatestSample(t.Context(), "a"); err != nil || !after.TS.Equal(before.TS) {
		t.Errorf("degraded poll must not write samples: %v %v", after, err)
	}

	// Same error within 10 minutes → suppressed; different error → new event.
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("duplicate hub.error within 10 minutes: %v", eventTypes(evs))
	}
	h.local.setStatusErr(errors.New("tailscaled: permission denied"))
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); countType(evs, model.EventHubError) != 1 {
		t.Errorf("distinct message must emit: %v", eventTypes(evs))
	}
	h.local.setStatusErr(errors.New("tailscaled: connect: no such file"))
	h.pollAfter(10 * time.Minute)
	if evs := h.drainEvents(); countType(evs, model.EventHubError) != 1 {
		t.Errorf("same message after 10 minutes must emit again: %v", eventTypes(evs))
	}

	// Recovery: paths restored, no error, no transition flood, rates sane.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"), bytes(10, 10)), peer("b", "bravo", "100.64.0.3", offline())))
	h.pollAfter(15 * time.Second)
	if hub := h.c.Hub(); hub.LastError != "" {
		t.Errorf("LastError after recovery: %q", hub.LastError)
	}
	if d := h.device("a"); d.Connectivity.Path != model.PathDirect || d.Connectivity.RxRate != 0 {
		t.Errorf("after recovery: %+v", d.Connectivity)
	}
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("recovery emitted: %v", eventTypes(evs))
	}

	t.Run("failure on very first poll keeps stored devices", func(t *testing.T) {
		h2 := newHarnessWithStore(t, defaultCfg(), h.st)
		h2.local.setStatusErr(errTest)
		h2.poll()
		snap := h2.c.Snapshot()
		if len(snap.Devices) != 3 || snap.Devices[0].Connectivity.Path != model.PathUnknown {
			t.Errorf("stored devices on failed first poll: %d", len(snap.Devices))
		}
		if evs := h2.drainEvents(); countType(evs, model.EventHubError) != 1 {
			t.Errorf("events: %v", eventTypes(evs))
		}
	})
}

func TestUptimeRatios(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2")))
	h.poll()
	if d := h.device("a"); d.Uptime.Pct24h != nil {
		t.Errorf("no history yet: %v", d.Uptime.Pct24h)
	}
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline())))
	h.pollAfter(time.Minute)
	h.pollAfter(time.Minute) // still using the cached (empty) ratios
	if d := h.device("a"); d.Uptime.Pct24h != nil {
		t.Errorf("ratios refreshed too early")
	}
	h.pollAfter(5 * time.Minute) // refresh: samples so far = online, offline, offline → 1/3
	d := h.device("a")
	if d.Uptime.Pct24h == nil || d.Uptime.Pct7d == nil || d.Uptime.Pct30d == nil {
		t.Fatalf("ratios missing: %+v", d.Uptime)
	}
	if got := *d.Uptime.Pct24h; got < 33.3 || got > 33.4 {
		t.Errorf("Pct24h=%v, want ~33.3", got)
	}
}

func TestSnapshotIsolationAndEmit(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2")))
	h.poll()
	snap := h.c.Snapshot()
	snap.Devices[0].Name = "mutated"
	snap.Overview.OSBreakdown["linux"] = 99
	again := h.c.Snapshot()
	if again.Devices[0].Name == "mutated" || again.Overview.OSBreakdown["linux"] == 99 {
		t.Errorf("Snapshot must return copies")
	}
	hub := h.c.Hub()
	hub.Health = append(hub.Health, "x")
	if len(h.c.Hub().Health) != 0 {
		t.Errorf("Hub must return a copy")
	}

	h.drainEvents()
	h.c.Emit(t.Context(), model.Event{Type: model.EventAdminAction, Title: "t", DeviceID: "a"})
	evs := h.drainEvents()
	if len(evs) != 1 || evs[0].ID == 0 || evs[0].Severity != model.SeverityInfo || !evs[0].TS.Equal(baseTime) {
		t.Errorf("Emit: %+v", evs)
	}
	stored, err := h.st.ListEvents(t.Context(), model.EventQuery{Types: []model.EventType{model.EventAdminAction}})
	if err != nil || len(stored) != 1 {
		t.Errorf("Emit must persist: %d %v", len(stored), err)
	}
}

func TestRunLoopAndRefreshNow(t *testing.T) {
	st, err := store.Open(context.Background(), ":memory:", quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	local := &fakeLocal{status: hubStatus(peer("a", "alpha", "100.64.0.2"))}
	cfg := defaultCfg()
	cfg.PollInterval = 20 * time.Millisecond
	cfg.RawRetention = time.Hour
	c := New(cfg, st, local, nil, &fakeAgents{}, quietLogger())
	ticks, cancelTicks := c.Ticks().Subscribe()
	defer cancelTicks()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	deadline := time.After(3 * time.Second)
	for got := 0; got < 3; {
		select {
		case <-ticks:
			got++
		case <-deadline:
			t.Fatalf("only %d ticks before deadline", got)
		}
	}
	c.RefreshNow()
	c.RefreshNow() // coalesced, never blocks
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("no tick after RefreshNow")
	}
	if d, ok := c.Device("alpha"); !ok || !d.Online {
		t.Errorf("device after Run polls: %+v %v", d, ok)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if err := c.PollOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("PollOnce on cancelled ctx: %v", err)
	}
}

func TestMaintenance(t *testing.T) {
	cfg := defaultCfg()
	cfg.RawRetention = 2 * time.Hour
	cfg.RollupRetention = 24 * time.Hour
	cfg.EventRetention = 24 * time.Hour
	h := newHarness(t, cfg)
	ctx := t.Context()
	var samples []model.Sample
	for i := 0; i < 20; i++ {
		samples = append(samples, model.Sample{DeviceID: "old", TS: baseTime.Add(-3*time.Hour + time.Duration(i)*15*time.Second), Online: true})
	}
	if err := h.st.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	h.c.maintain(ctx)
	stats, err := h.st.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rollups == 0 {
		t.Errorf("rollup did not run: %+v", stats)
	}
	// Prune uses the store's own clock (real time); samples dated 2026-09-28
	// are only pruned once that is more than 2h in the past, so assert on
	// the rollup side only and that maintain tolerates a cancelled context.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	h.c.maintain(cctx) // must not panic or block
}

func TestConcurrentAccessRaceFree(t *testing.T) {
	cfg := defaultCfg()
	cfg.PingInterval = 15 * time.Second
	h := newHarness(t, cfg)
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", relay("nyc")), peer("b", "bravo", "100.64.0.3")))
	h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 1, Endpoint: "1.1.1.1:1"})
	h.agents.setReport("100.64.0.2", report(baseTime, 1, 1))
	h.poll()
	var mu sync.Mutex
	now := h.now
	h.c.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = h.c.Snapshot()
			_, _ = h.c.Device("alpha")
			_ = h.c.Topology()
			_ = h.c.Hub()
			_, _ = h.c.PingNow(t.Context(), "a")
		}
	}()
	for i := 0; i < 20; i++ {
		mu.Lock()
		now = now.Add(15 * time.Second)
		mu.Unlock()
		h.poll()
	}
	close(stop)
	wg.Wait()
}

func TestForget(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h.poll()
	h.drainEvents()
	ctx := t.Context()

	if err := h.c.Forget(ctx, "zzz"); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("unknown device: %v", err)
	}
	if err := h.c.Forget(ctx, "alpha"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, ok := h.c.Device("a"); ok {
		t.Errorf("forgotten device still in snapshot")
	}
	if _, err := h.st.GetDevice(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("forgotten device still stored: %v", err)
	}
	if _, ok := h.c.Device("b"); !ok {
		t.Errorf("other devices must be kept")
	}

	// Gone from the sources too: it stays gone, no events, no row.
	h.local.setStatus(hubStatus(peer("b", "bravo", "100.64.0.3")))
	h.pollAfter(15 * time.Second)
	if _, ok := h.c.Device("a"); ok {
		t.Errorf("forgotten device resurrected by poll")
	}
	if _, err := h.st.GetDevice(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("forgotten device row resurrected: %v", err)
	}
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("events after forget: %v", eventTypes(evs))
	}

	// Still reported by a source: comes back as a new device.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h.pollAfter(15 * time.Second)
	if _, ok := h.c.Device("a"); !ok {
		t.Errorf("device still in netmap must reappear")
	}
	if evs := h.drainEvents(); countType(evs, model.EventDeviceNew) != 1 {
		t.Errorf("reappearance must be device.new: %v", eventTypes(evs))
	}

	// Forget while a poll is in flight: the in-flight poll must not write
	// the device back.
	release := make(chan struct{})
	h.agents.setReport("100.64.0.2", report(h.now, 1, 1))
	h.agents.delay = 30 * time.Millisecond
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-release
		h.pollAfter(15 * time.Second)
	}()
	close(release)
	time.Sleep(10 * time.Millisecond) // poll is inside the (delayed) agent fetch
	if err := h.c.Forget(ctx, "a"); err != nil {
		t.Fatalf("Forget during poll: %v", err)
	}
	<-done
	if _, ok := h.c.Device("a"); ok {
		t.Errorf("in-flight poll wrote a forgotten device back")
	}
	if _, err := h.st.GetDevice(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("in-flight poll resurrected the row: %v", err)
	}
}

func TestSampleFieldsAndRemovedDevicesPersist(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", relay("nyc"))))
	h.poll()
	sm, err := h.st.LatestSample(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if sm.AgentOK || sm.CPU != nil || sm.Relay != "nyc" || sm.Direct == nil || *sm.Direct || !sm.Online {
		t.Errorf("sample: %+v", sm)
	}
	if _, err := h.st.LatestSample(t.Context(), "self"); err != nil {
		t.Errorf("self must be sampled too: %v", err)
	}
	devs, err := h.st.ListDevices(t.Context())
	if err != nil || len(devs) != 2 {
		t.Errorf("devices persisted: %d %v", len(devs), err)
	}
}
