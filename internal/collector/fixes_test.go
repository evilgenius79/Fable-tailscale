package collector

import (
	"errors"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// A netmap peer is authorized by definition; a stale control API row (the
// cache is refreshed only every APIInterval) must not de-authorize it, and
// a refresh must re-query the API at once.
func TestNetmapPeerIsAuthorizedDespiteStaleAPIRow(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.api.configured = true
	h.api.setDevices(source.APIDevice{ID: "1", NodeID: "a", Name: "alpha.tail.ts.net", Authorized: false, LastSeen: baseTime})
	h.poll() // pending device, only the API knows it
	if d := h.device("a"); d.Authorized {
		t.Fatalf("api-only device must follow the API row")
	}
	if ov := h.c.Snapshot().Overview; ov.Unauthorized != 1 {
		t.Fatalf("Unauthorized=%d, want 1", ov.Unauthorized)
	}
	h.drainEvents()

	// The admin authorized it: it shows up in the netmap while the API
	// cache (not due for 60s) still says unauthorized.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2")))
	h.pollAfter(15 * time.Second)
	if h.api.callCount() != 1 {
		t.Fatalf("api calls=%d, the cache should not have been refreshed yet", h.api.callCount())
	}
	d := h.device("a")
	if !d.Authorized {
		t.Errorf("netmap peer reported unauthorized from the stale API row")
	}
	if ov := h.c.Snapshot().Overview; ov.Unauthorized != 0 {
		t.Errorf("Unauthorized=%d, want 0", ov.Unauthorized)
	}
	if evs := h.drainEvents(); countType(evs, model.EventDeviceAuthorized) != 1 {
		t.Errorf("device.authorized expected: %v", eventTypes(evs))
	}

	// RefreshNow forces the API to be re-queried on the next poll.
	h.api.setDevices(source.APIDevice{ID: "1", NodeID: "a", Name: "alpha.tail.ts.net", Authorized: true, ClientVersion: "1.90.0"})
	h.c.RefreshNow()
	h.pollAfter(15 * time.Second)
	if h.api.callCount() != 2 {
		t.Errorf("api calls=%d, want 2 after RefreshNow", h.api.callCount())
	}
	if d := h.device("a"); d.ClientVersion != "1.90.0" {
		t.Errorf("forced refresh did not apply the new API row: %+v", d)
	}
	h.pollAfter(15 * time.Second)
	if h.api.callCount() != 2 {
		t.Errorf("the forced refresh must be consumed by one poll: %d", h.api.callCount())
	}

	// Refresh is the synchronous form for callers that return the device.
	h.api.setDevices(source.APIDevice{ID: "1", NodeID: "a", Name: "alpha.tail.ts.net", Authorized: true, ClientVersion: "1.91.0"})
	h.advance(15 * time.Second)
	if err := h.c.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if h.api.callCount() != 3 {
		t.Errorf("api calls=%d, want 3 after Refresh", h.api.callCount())
	}
	if d := h.device("a"); d.ClientVersion != "1.91.0" {
		t.Errorf("Refresh must publish the refreshed device: %+v", d)
	}
}

// A measured latency describes the link only until the next scheduled ping
// fails or the device goes offline / away; LastPing stays for display.
func TestLatencyIsNotCarriedForever(t *testing.T) {
	cfg := defaultCfg()
	cfg.PingInterval = 15 * time.Second
	h := newHarness(t, defaultCfg()) // pings disabled: PingNow is the only source
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"))))
	h.poll()
	h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 400, Endpoint: "1.1.1.1:1"})
	if _, err := h.c.PingNow(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if d := h.device("a"); floatVal(d.Connectivity.LatencyMs) != 400 {
		t.Fatalf("PingNow latency: %v", d.Connectivity.LatencyMs)
	}
	h.pollAfter(15 * time.Second)
	if d := h.device("a"); floatVal(d.Connectivity.LatencyMs) != 400 || d.Connectivity.LastPing == nil {
		t.Errorf("latency should be carried while online with no ping evidence to the contrary: %+v", d.Connectivity)
	}

	// Offline: no latency; LastPing kept.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline())))
	h.pollAfter(15 * time.Second)
	d := h.device("a")
	if d.Connectivity.LatencyMs != nil || d.Connectivity.LastPing == nil {
		t.Errorf("offline device: %+v", d.Connectivity)
	}
	if top := h.c.Topology(); top.Nodes[0].LatencyMs != nil && top.Nodes[1].LatencyMs != nil {
		t.Errorf("topology still carries a latency for an offline node")
	}
	if ov := h.c.Snapshot().Overview; ov.AvgLatencyMs != nil {
		t.Errorf("AvgLatencyMs=%v, want nil", *ov.AvgLatencyMs)
	}

	t.Run("failed scheduled ping clears it", func(t *testing.T) {
		h := newHarness(t, cfg)
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"))))
		h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 400, Endpoint: "1.1.1.1:1"})
		h.poll()
		if d := h.device("a"); floatVal(d.Connectivity.LatencyMs) != 400 {
			t.Fatalf("latency after ping: %v", d.Connectivity.LatencyMs)
		}
		h.local.setPing("100.64.0.2", source.PingReply{Err: "timeout"})
		h.pollAfter(15 * time.Second)
		d := h.device("a")
		if d.Connectivity.LatencyMs != nil {
			t.Errorf("latency kept after a failed ping: %v", *d.Connectivity.LatencyMs)
		}
		if d.Connectivity.LastPing == nil || !d.Connectivity.LastPing.Equal(baseTime) {
			t.Errorf("LastPing must be kept for display: %v", d.Connectivity.LastPing)
		}
		if d.Connectivity.Path != model.PathDirect {
			t.Errorf("a failed ping must not touch the path: %v", d.Connectivity.Path)
		}
		h.local.setPingErr("100.64.0.2", errTest)
		h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 5, Endpoint: "1.1.1.1:1"})
		h.pollAfter(15 * time.Second)
		if d := h.device("a"); d.Connectivity.LatencyMs != nil {
			t.Errorf("latency after a transport error: %v", *d.Connectivity.LatencyMs)
		}
	})

	t.Run("removed and degraded polls clear it", func(t *testing.T) {
		h := newHarness(t, cfg)
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"))))
		h.local.setPing("100.64.0.2", source.PingReply{LatencyMs: 400, Endpoint: "1.1.1.1:1"})
		h.poll()
		h.local.setStatusErr(errTest)
		h.pollAfter(15 * time.Second)
		if d := h.device("a"); d.Connectivity.LatencyMs != nil || d.Connectivity.LastPing == nil {
			t.Errorf("degraded poll: %+v", d.Connectivity)
		}
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"))))
		h.pollAfter(15 * time.Second)
		if d := h.device("a"); floatVal(d.Connectivity.LatencyMs) != 400 {
			t.Fatalf("latency after recovery ping: %v", d.Connectivity.LatencyMs)
		}
		h.local.setStatus(hubStatus())
		for i := 0; i < removedAfterPolls; i++ {
			h.pollAfter(15 * time.Second)
		}
		if d := h.device("a"); d.Online || d.Connectivity.LatencyMs != nil || d.Connectivity.LastPing == nil {
			t.Errorf("removed device: %+v", d.Connectivity)
		}
	})
}

// Removed reports the devices kept only as a memory of a node that left.
func TestRemovedTracking(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h.poll()
	if h.c.Removed("a") || h.c.Removed("zzz") {
		t.Fatal("present devices are not removed")
	}
	h.local.setStatus(hubStatus(peer("b", "bravo", "100.64.0.3")))
	for i := 1; i < removedAfterPolls; i++ {
		h.pollAfter(15 * time.Second)
		if h.c.Removed("a") {
			t.Fatalf("poll %d absent: removed too early", i)
		}
	}
	h.pollAfter(15 * time.Second)
	if !h.c.Removed("a") || h.c.Removed("b") {
		t.Errorf("after %d absent polls: a=%v b=%v", removedAfterPolls, h.c.Removed("a"), h.c.Removed("b"))
	}
	if _, ok := h.c.Device("a"); !ok {
		t.Errorf("removed device must stay in the snapshot")
	}
	// The flag survives a degraded poll and a restart, and clears when the
	// device reappears or is forgotten.
	h.local.setStatusErr(errTest)
	h.pollAfter(15 * time.Second)
	if !h.c.Removed("a") {
		t.Errorf("degraded poll dropped the removed flag")
	}
	h2 := newHarnessWithStore(t, defaultCfg(), h.st)
	h2.now = h.now.Add(time.Hour)
	h2.local.setStatus(hubStatus(peer("b", "bravo", "100.64.0.3")))
	h2.poll()
	if !h2.c.Removed("a") || h2.c.Removed("b") {
		t.Errorf("after restart: a=%v b=%v", h2.c.Removed("a"), h2.c.Removed("b"))
	}
	h2.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h2.pollAfter(15 * time.Second)
	if h2.c.Removed("a") {
		t.Errorf("reappeared device still removed")
	}
	h2.local.setStatus(hubStatus(peer("b", "bravo", "100.64.0.3")))
	for i := 0; i < removedAfterPolls; i++ {
		h2.pollAfter(15 * time.Second)
	}
	if !h2.c.Removed("a") {
		t.Fatalf("not removed again")
	}
	if err := h2.c.Forget(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if h2.c.Removed("a") {
		t.Errorf("forgotten device still removed")
	}
}

// An agent that was reachable before the device went offline and fails
// right after it comes back emits agent.unreachable, so the event log
// matches the alert the engine raises.
func TestAgentUnreachableAfterReturnFromOffline(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2")))
	h.agents.setReport("100.64.0.2", report(baseTime, 1, 1))
	h.poll()
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline())))
	h.pollAfter(15 * time.Second)
	if d := h.device("a"); d.Agent.State != model.AgentUnknown || d.Agent.LastSuccess == nil {
		t.Fatalf("agent while offline: %+v", d.Agent)
	}
	h.drainEvents()

	h.agents.setErr("100.64.0.2", errTest)
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2")))
	h.pollAfter(3 * 24 * time.Hour)
	evs := h.drainEvents()
	if countType(evs, model.EventDeviceOnline) != 1 || countType(evs, model.EventAgentUnreachable) != 1 {
		t.Errorf("events after return: %v", eventTypes(evs))
	}
	if d := h.device("a"); d.Agent.State != model.AgentUnreachable || d.Agent.LastSuccess == nil || !d.Agent.LastSuccess.Equal(baseTime) {
		t.Errorf("agent after failed fetch: %+v", d.Agent)
	}
	h.pollAfter(time.Minute)
	if evs := h.drainEvents(); countType(evs, model.EventAgentUnreachable) != 0 {
		t.Errorf("agent.unreachable must not repeat: %v", eventTypes(evs))
	}
}

// Rates use the time since the device's counters were last read, not the
// time since the last poll, so a device carried through absent polls does
// not report its whole delta against one interval.
func TestRateBaselineFollowsCounterReads(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(0, 0))))
	h.poll()
	h.local.setStatus(hubStatus())
	h.pollAfter(15 * time.Second)
	h.pollAfter(15 * time.Second)
	if d := h.device("a"); d.Connectivity.RxRate != 0 {
		t.Fatalf("rate while absent: %v", d.Connectivity.RxRate)
	}
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(4500, 900))))
	h.pollAfter(15 * time.Second)
	if d := h.device("a"); d.Connectivity.RxRate != 100 || d.Connectivity.TxRate != 20 {
		t.Errorf("rate over 45s: rx=%v tx=%v, want 100/20", d.Connectivity.RxRate, d.Connectivity.TxRate)
	}

	t.Run("api-only to netmap transition has no rate", func(t *testing.T) {
		h := newHarness(t, defaultCfg())
		h.api.configured = true
		h.api.setDevices(source.APIDevice{NodeID: "a", Name: "alpha.tail.ts.net", Authorized: true, LastSeen: baseTime})
		h.poll()
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(1<<30, 1<<30))))
		h.pollAfter(15 * time.Second)
		if d := h.device("a"); d.Connectivity.RxRate != 0 || d.Connectivity.TxRate != 0 {
			t.Errorf("counter spike on first netmap read: rx=%v tx=%v", d.Connectivity.RxRate, d.Connectivity.TxRate)
		}
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(1<<30+150, 1<<30))))
		h.pollAfter(15 * time.Second)
		if d := h.device("a"); d.Connectivity.RxRate != 10 {
			t.Errorf("rate after second read: %v, want 10", d.Connectivity.RxRate)
		}
	})

	t.Run("removed device restarts its baseline", func(t *testing.T) {
		h := newHarness(t, defaultCfg())
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(0, 0))))
		h.poll()
		h.local.setStatus(hubStatus())
		for i := 0; i < removedAfterPolls; i++ {
			h.pollAfter(15 * time.Second)
		}
		h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", bytes(1<<20, 0))))
		h.pollAfter(time.Hour)
		if d := h.device("a"); d.Connectivity.RxRate != 0 {
			t.Errorf("rate computed across a removal: %v", d.Connectivity.RxRate)
		}
	})
}

// A Forget that lands between the poll consuming the forgotten set and its
// UpsertDevices writes the row back; the poll must delete it again and the
// published snapshot must never show a forgotten device.
func TestForgetLateInPoll(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.api.configured = true
	h.api.setDevices(source.APIDevice{NodeID: "a", Name: "alpha.tail.ts.net", Authorized: true, ClientVersion: "1.80.0"})
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h.poll()
	ctx := t.Context()
	a := h.device("a")

	// Forget, then an upsert that raced with it (the poll's persistence).
	if err := h.c.Forget(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpsertDevices(ctx, []model.Device{a}); err != nil {
		t.Fatal(err)
	}
	// Gone from the netmap, but still in the cached API rows (the admin
	// deleted it upstream; the cache is not due for another 45s).
	h.local.setStatus(hubStatus(peer("b", "bravo", "100.64.0.3")))
	h.pollAfter(15 * time.Second)
	if _, ok := h.c.Device("a"); ok {
		t.Errorf("forgotten device resurrected from the cached API row")
	}
	if _, err := h.st.GetDevice(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("resurrected row not deleted again: %v", err)
	}
	h.pollAfter(15 * time.Second)
	if _, ok := h.c.Device("a"); ok {
		t.Errorf("forgotten device came back on a later poll")
	}
	if _, err := h.st.GetDevice(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("row after later poll: %v", err)
	}
	if h.api.callCount() != 1 {
		t.Fatalf("api calls=%d (test assumes the cache was not refreshed)", h.api.callCount())
	}

	// publish drops devices forgotten after the poll's last check.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2"), peer("b", "bravo", "100.64.0.3")))
	h.api.setDevices()
	h.pollAfter(15 * time.Second)
	snap := h.c.Snapshot()
	h.c.mu.Lock()
	h.c.forgotten["b"] = true
	h.c.mu.Unlock()
	h.c.publish(snap, h.c.Hub(), map[model.DeviceID]bool{"b": true})
	if _, ok := h.c.Device("b"); ok {
		t.Errorf("publish kept a device forgotten after the poll's check")
	}
	if h.c.Removed("b") {
		t.Errorf("publish kept the removed flag of a forgotten device")
	}
	if _, ok := h.c.Device("a"); !ok {
		t.Errorf("publish dropped an unrelated device")
	}
	h.drainEvents()
	h.pollAfter(15 * time.Second) // consumes the late Forget: b is skipped once
	if _, ok := h.c.Device("b"); ok {
		t.Errorf("poll consuming the Forget wrote the device back")
	}
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("events for a forgotten device: %v", eventTypes(evs))
	}
	h.pollAfter(15 * time.Second) // still in the netmap: back as a new device
	if evs := h.drainEvents(); countType(evs, model.EventDeviceNew) != 1 {
		t.Errorf("device still in netmap returns as device.new: %v", eventTypes(evs))
	}
}
