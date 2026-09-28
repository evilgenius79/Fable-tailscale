package alerts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// removalSource is a fakeSource that also reports removed devices.
type removalSource struct {
	*fakeSource
	removed map[model.DeviceID]bool
}

func (r *removalSource) Removed(id model.DeviceID) bool { return r.removed[id] }

// A device the collector keeps only as a memory of a node that left the
// tailnet must not accumulate a permanent device_offline alert; whatever is
// open for it resolves.
func TestRemovedDeviceResolvesInsteadOfAlerting(t *testing.T) {
	h := newHarness(t)
	src := &removalSource{fakeSource: h.src, removed: map[model.DeviceID]bool{}}
	h.e.src = src

	gone := dev("old", "decommissioned", false)
	gone.Uptime.LastChange = tp(base.Add(-time.Hour))
	h.tick(gone)
	a := h.onlyOpen()
	if a.RuleID != "device_offline" {
		t.Fatalf("open alert = %+v", a)
	}

	// The collector now reports it removed: the alert resolves and, no
	// matter how long the row lingers, nothing reopens.
	src.removed["old"] = true
	h.clk.Advance(time.Minute)
	h.tick(gone)
	if open := h.openAlerts(); len(open) != 0 {
		t.Fatalf("alerts still open for a removed device: %+v", open)
	}
	if all := h.allAlerts(); len(all) != 1 || all[0].Message != "Device removed from the tailnet" {
		t.Errorf("resolved alert = %+v", all)
	}
	for i := 0; i < 30; i++ {
		h.clk.Advance(24 * time.Hour)
		h.tick(gone)
	}
	if open := h.openAlerts(); len(open) != 0 {
		t.Errorf("removed device raised alerts: %+v", open)
	}
	if got := h.nf.sent(); len(got) != 2 || got[1].Type != model.EventAlertResolved {
		t.Errorf("notifications = %+v", got)
	}

	// Back in the tailnet and offline again: alerts work as before.
	src.removed["old"] = false
	gone.Uptime.LastChange = tp(h.clk.Now().Add(-time.Hour))
	h.tick(gone)
	if a := h.onlyOpen(); a.RuleID != "device_offline" {
		t.Errorf("reappeared device: %+v", a)
	}

	t.Run("source without removal support keeps alerting", func(t *testing.T) {
		h := newHarness(t)
		plain := dev("old", "decommissioned", false)
		plain.Uptime.LastChange = tp(base.Add(-time.Hour))
		h.tick(plain)
		h.onlyOpen()
	})
}

// The agent_unreachable grace period counts from the moment the device came
// back online, not from the last successful fetch before it went offline.
func TestAgentGraceAfterReturnFromOffline(t *testing.T) {
	h := newHarness(t)
	d := dev("vm", "vm", true)
	d.Agent = model.AgentStatus{State: model.AgentReachable, Version: "1.0.0", LastSuccess: tp(base)}
	d.Uptime.LastChange = tp(base.Add(-time.Hour))
	h.tick(d)

	// Offline for three days: the collector carries LastSuccess through.
	off := d
	off.Online = false
	off.Agent.State = model.AgentUnknown
	off.Uptime.LastChange = tp(h.clk.Advance(15 * time.Second))
	h.tick(off)
	if len(h.openAlerts()) != 0 {
		t.Fatal("offline device raised an agent alert")
	}

	// Back online, agent not yet up: within the 300s grace nothing fires.
	back := d
	back.Agent.State = model.AgentUnreachable
	back.Agent.LastError = "dial tcp: connection refused"
	back.Uptime.LastChange = tp(h.clk.Advance(3 * 24 * time.Hour))
	h.tick(back)
	if open := h.openAlerts(); len(open) != 0 {
		t.Fatalf("agent_unreachable fired on the first failed fetch after a return: %+v", open)
	}
	h.clk.Advance(2 * time.Minute)
	h.tick(back)
	if open := h.openAlerts(); len(open) != 0 {
		t.Fatalf("agent_unreachable fired inside the grace period: %+v", open)
	}
	h.clk.Advance(3*time.Minute + time.Second)
	h.tick(back)
	a := h.onlyOpen()
	if a.RuleID != "agent_unreachable" || !strings.HasPrefix(a.Message, "Agent unreachable for 5m (last success Mar 1 12:00)") {
		t.Errorf("alert = %+v", a)
	}

	// A later successful fetch still resolves it.
	h.clk.Advance(time.Minute)
	ok := back
	ok.Agent = model.AgentStatus{State: model.AgentReachable, LastSuccess: tp(h.clk.Now())}
	h.tick(ok)
	if len(h.openAlerts()) != 0 {
		t.Error("agent alert did not resolve")
	}
}

// The tick subscription is taken when the engine is built, so a snapshot
// published before Run starts (the collector's first poll) is evaluated.
func TestTickPublishedBeforeRunIsEvaluated(t *testing.T) {
	h := newHarness(t)
	h.setRule("device_offline", func(r *model.AlertRule) { r.ForSeconds = 0 })
	if h.src.ticks.Len() != 1 {
		t.Fatalf("subscriptions after NewEngine = %d, want 1", h.src.ticks.Len())
	}
	if n := h.src.ticks.Publish(model.Snapshot{Overview: model.Overview{Hub: h.src.hub}, Devices: []model.Device{dev("nas", "nas", false)}}); n != 1 {
		t.Fatalf("tick delivered to %d subscribers before Run, want 1", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.e.Run(ctx) }()
	waitFor(t, "early tick processed", func() bool { return h.e.processed.Load() == 1 })
	waitFor(t, "alert from the early tick", func() bool { return len(h.openAlerts()) == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v", err)
	}
	if h.src.ticks.Len() != 0 {
		t.Error("Run left its tick subscription behind")
	}

	// A second Run subscribes afresh.
	ctx, cancel = context.WithCancel(context.Background())
	go func() { done <- h.e.Run(ctx) }()
	waitFor(t, "resubscribed", func() bool { return h.src.ticks.Len() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("second Run = %v", err)
	}
}
