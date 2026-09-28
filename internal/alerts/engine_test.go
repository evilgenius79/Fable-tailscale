package alerts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

func TestNewEngineMergesStoredOverrides(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	override := model.AlertRule{
		ID: "high_cpu", Type: model.RuleHighCPU, Name: "Custom CPU", Enabled: false,
		Severity: model.SeverityCritical, Threshold: 50, ForSeconds: 30, Notify: false,
		IncludeTags: []string{"tag:server"},
	}
	if err := st.SaveRule(ctx, &override); err != nil {
		t.Fatal(err)
	}
	bogus := model.AlertRule{ID: "bogus", Type: "bogus_type", Name: "Bogus", Severity: model.SeverityInfo}
	if err := st.SaveRule(ctx, &bogus); err != nil {
		t.Fatal(err)
	}
	// A stored row missing its type/name/severity is repaired from the default.
	broken := model.AlertRule{ID: "disk_full", Enabled: true, Threshold: 80}
	if err := st.SaveRule(ctx, &broken); err != nil {
		t.Fatal(err)
	}

	h := newHarnessOn(t, st, &fakeClock{t: base})
	rules := h.e.Rules()
	if len(rules) != len(DefaultRules()) {
		t.Fatalf("Rules() = %d rules, want %d", len(rules), len(DefaultRules()))
	}
	byID := map[string]model.AlertRule{}
	for _, r := range rules {
		byID[r.ID] = r
	}
	cpu := byID["high_cpu"]
	if cpu.Name != "Custom CPU" || cpu.Enabled || cpu.Severity != model.SeverityCritical ||
		cpu.Threshold != 50 || cpu.ForSeconds != 30 || cpu.Notify || len(cpu.IncludeTags) != 1 {
		t.Errorf("stored override not honoured: %+v", cpu)
	}
	disk := byID["disk_full"]
	if disk.Type != model.RuleDiskFull || disk.Name != "Disk almost full" || disk.Severity != model.SeverityWarning || disk.Threshold != 80 {
		t.Errorf("broken row not repaired: %+v", disk)
	}
	if _, ok := byID["bogus"]; ok {
		t.Errorf("rule of unknown type must be ignored")
	}
	for i := 1; i < len(rules); i++ {
		if strings.ToLower(rules[i-1].Name) > strings.ToLower(rules[i].Name) {
			t.Errorf("Rules() not sorted by name: %q before %q", rules[i-1].Name, rules[i].Name)
		}
	}
	stored, err := st.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(DefaultRules())+1 {
		t.Errorf("stored rules = %d, want defaults persisted plus the bogus row (%d)", len(stored), len(DefaultRules())+1)
	}
	// Returned rules are copies.
	rules[0].IncludeTags = append(rules[0].IncludeTags, "tag:mutated")
	for _, r := range h.e.Rules() {
		for _, tag := range r.IncludeTags {
			if tag == "tag:mutated" {
				t.Fatal("Rules() must return copies")
			}
		}
	}
}

func TestForTiming(t *testing.T) {
	cpu := func(h *harness, pct float64) model.Device {
		return withMetrics(dev("pi", "pi-hole", true), h.clk.Now(), model.MetricsSnapshot{CPUPercent: pct})
	}
	t.Run("fires once the condition has held for ForSeconds", func(t *testing.T) {
		h := newHarness(t)
		h.tick(cpu(h, 95))
		if n := len(h.openAlerts()); n != 0 {
			t.Fatalf("alert opened immediately, want pending")
		}
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 95))
		if n := len(h.openAlerts()); n != 0 {
			t.Fatalf("alert opened after 300s, want pending until 600s")
		}
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 95))
		a := h.onlyOpen()
		if a.RuleID != "high_cpu" || a.DeviceID != "pi" || a.Severity != model.SeverityWarning {
			t.Errorf("unexpected alert %+v", a)
		}
		if a.Title != "CPU high on pi-hole" {
			t.Errorf("title = %q", a.Title)
		}
		if a.Message != "CPU at 95% for 10m (threshold 90%)" {
			t.Errorf("message = %q", a.Message)
		}
		if a.Value == nil || *a.Value != 95 {
			t.Errorf("value = %v, want 95", a.Value)
		}
		if a.Data["threshold"] != float64(90) || a.Data["value"] != float64(95) {
			t.Errorf("data = %v", a.Data)
		}
		if got := h.nf.sent(); len(got) != 1 || got[0].Type != model.EventAlertOpened || got[0].Alert.ID != a.ID || got[0].Hub.SelfName != "hub" {
			t.Errorf("notifications = %+v", got)
		}
		if evs := h.src.eventsOfType(model.EventAlertOpened); len(evs) != 1 || evs[0].Severity != model.SeverityWarning || evs[0].DeviceID != "pi" {
			t.Errorf("alert.opened events = %+v", evs)
		}
	})
	t.Run("a dip resets the pending timer", func(t *testing.T) {
		h := newHarness(t)
		h.tick(cpu(h, 95))
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 50))
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 95))
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 95))
		if n := len(h.openAlerts()); n != 0 {
			t.Fatalf("alert opened 300s after the dip; timer was not reset")
		}
		h.clk.Advance(300 * time.Second)
		h.tick(cpu(h, 95))
		h.onlyOpen()
	})
	t.Run("ForSeconds 0 fires immediately", func(t *testing.T) {
		h := newHarness(t)
		h.setRule("high_cpu", func(r *model.AlertRule) { r.ForSeconds = 0 })
		h.tick(cpu(h, 95))
		if a := h.onlyOpen(); a.Message != "CPU at 95% (threshold 90%)" {
			t.Errorf("message = %q", a.Message)
		}
	})
}

func TestOfflineUsesLastTransition(t *testing.T) {
	h := newHarness(t)
	d := dev("nas", "nas", false)
	d.Uptime.LastChange = tp(base.Add(-6 * time.Minute))
	d.LastSeen = base.Add(-6 * time.Minute)
	h.tick(d)
	a := h.onlyOpen()
	if a.Title != "nas is offline" {
		t.Errorf("title = %q", a.Title)
	}
	if a.Message != "Offline for 6m (last seen 11:54)" {
		t.Errorf("message = %q", a.Message)
	}
	// Data round-trips through JSON, so numbers come back as float64.
	if a.Data["offlineSeconds"] != float64(360) || a.Data["lastSeen"] != "2026-03-01T11:54:00Z" {
		t.Errorf("data = %v", a.Data)
	}

	// Without a known transition the engine counts from its first observation.
	h2 := newHarness(t)
	h2.tick(dev("nas", "nas", false))
	if len(h2.openAlerts()) != 0 {
		t.Fatal("offline alert fired before ForSeconds elapsed")
	}
	h2.clk.Advance(301 * time.Second)
	h2.tick(dev("nas", "nas", false))
	if a := h2.onlyOpen(); a.Message != "Offline for 5m (last seen 11:53)" {
		t.Errorf("message = %q", a.Message)
	}
}

func TestHysteresis(t *testing.T) {
	h := newHarness(t)
	h.setRule("high_cpu", func(r *model.AlertRule) { r.ForSeconds = 0 })
	cpu := func(pct float64) model.Device {
		return withMetrics(dev("pi", "pi-hole", true), h.clk.Now(), model.MetricsSnapshot{CPUPercent: pct})
	}
	h.tick(cpu(95))
	opened := h.onlyOpen()

	h.clk.Advance(time.Minute)
	h.tick(cpu(88)) // below threshold but above threshold-5: stays open
	a := h.onlyOpen()
	if a.ID != opened.ID {
		t.Fatalf("alert re-opened instead of kept")
	}
	if a.Value == nil || *a.Value != 88 || a.Message != "CPU at 88% (threshold 90%)" {
		t.Errorf("open alert not refreshed: value=%v message=%q", a.Value, a.Message)
	}
	if n := len(h.nf.sent()); n != 1 {
		t.Errorf("refresh must not notify; got %d notifications", n)
	}

	h.clk.Advance(time.Minute)
	h.tick(cpu(84)) // below threshold-5: clears
	if n := len(h.openAlerts()); n != 0 {
		t.Fatalf("alert still open at 84%%")
	}
	all := h.allAlerts()
	if len(all) != 1 || all[0].State != model.AlertResolved || all[0].ResolvedAt == nil {
		t.Fatalf("resolved alert not recorded: %+v", all)
	}
	if all[0].Message != "CPU back to 84% (threshold 90%)" {
		t.Errorf("resolved message = %q", all[0].Message)
	}
	if all[0].Data["resolvedValue"] != float64(84) || all[0].Data["durationSeconds"] != float64(120) {
		t.Errorf("resolved data = %v", all[0].Data)
	}
	got := h.nf.sent()
	if len(got) != 2 || got[1].Type != model.EventAlertResolved || got[1].Alert.State != model.AlertResolved {
		t.Errorf("notifications = %+v", got)
	}
	if evs := h.src.eventsOfType(model.EventAlertResolved); len(evs) != 1 || !strings.HasPrefix(evs[0].Title, "Resolved: ") {
		t.Errorf("alert.resolved events = %+v", evs)
	}
	// Re-firing below the original threshold is not possible.
	h.tick(cpu(88))
	if len(h.openAlerts()) != 0 {
		t.Error("alert re-opened at 88% (below the firing threshold)")
	}
}

func TestEscalation(t *testing.T) {
	t.Run("disk_full escalates to critical at 97%", func(t *testing.T) {
		h := newHarness(t)
		disk := func(pct float64) model.Device {
			return withMetrics(dev("nas", "nas", true), h.clk.Now(), model.MetricsSnapshot{
				DiskPercent: pct,
				Disks:       []model.DiskUsage{{Mount: "/", Percent: pct}},
			})
		}
		h.tick(disk(92))
		a := h.onlyOpen()
		if a.Severity != model.SeverityWarning || a.Message != "Disk at 92% on / (threshold 90%)" {
			t.Fatalf("initial alert %+v", a)
		}
		h.clk.Advance(time.Minute)
		h.tick(disk(98))
		b := h.onlyOpen()
		if b.ID != a.ID || b.Severity != model.SeverityCritical {
			t.Fatalf("expected escalation of alert %d, got %+v", a.ID, b)
		}
		if b.Value == nil || *b.Value != 98 || b.Data["escalatedAt"] == nil {
			t.Errorf("escalated alert not updated: %+v", b)
		}
		got := h.nf.sent()
		if len(got) != 2 || got[1].Type != model.EventAlertOpened || got[1].Alert.Severity != model.SeverityCritical {
			t.Errorf("escalation must re-notify: %+v", got)
		}
		if evs := h.src.eventsOfType(model.EventAlertOpened); len(evs) != 2 || evs[1].Severity != model.SeverityCritical || evs[1].Data["escalated"] != true {
			t.Errorf("escalation event = %+v", evs)
		}
		// Severity never goes back down while open.
		h.clk.Advance(time.Minute)
		h.tick(disk(93))
		if c := h.onlyOpen(); c.Severity != model.SeverityCritical {
			t.Errorf("severity de-escalated to %s", c.Severity)
		}
		if n := len(h.nf.sent()); n != 2 {
			t.Errorf("de-escalation must not notify; got %d", n)
		}
	})
	t.Run("key_expiring escalates below 2 days", func(t *testing.T) {
		h := newHarness(t)
		d := dev("lap", "laptop", true)
		d.KeyExpiry = tp(base.Add(5 * 24 * time.Hour))
		h.tick(d)
		a := h.onlyOpen()
		if a.Severity != model.SeverityWarning || a.Value == nil || *a.Value != 5 {
			t.Fatalf("initial alert %+v", a)
		}
		if a.Message != "Node key expires in 5d (Mar 6 12:00)" {
			t.Errorf("message = %q", a.Message)
		}
		h.clk.Advance(3*24*time.Hour + 12*time.Hour) // 1.5 days left
		h.tick(d)
		b := h.onlyOpen()
		if b.ID != a.ID || b.Severity != model.SeverityCritical || b.Value == nil || *b.Value != 1.5 {
			t.Fatalf("expected critical escalation, got %+v", b)
		}
		if b.Message != "Node key expires in 1d 12h (Mar 6 12:00)" {
			t.Errorf("message = %q", b.Message)
		}
		// Renewal resolves it.
		h.clk.Advance(time.Hour)
		d.KeyExpiry = tp(h.clk.Now().Add(180 * 24 * time.Hour))
		h.tick(d)
		if len(h.openAlerts()) != 0 {
			t.Error("renewed key did not resolve the alert")
		}
	})
}

func TestResolveNotifiesUnlessAcked(t *testing.T) {
	h := newHarness(t)
	h.setRule("device_offline", func(r *model.AlertRule) { r.ForSeconds = 0 })
	h.tick(dev("nas", "nas", false), dev("pi", "pi", false))
	open := h.openAlerts()
	if len(open) != 2 {
		t.Fatalf("open = %d, want 2", len(open))
	}
	var nasID int64
	for _, a := range open {
		if a.DeviceID == "nas" {
			nasID = a.ID
		}
	}

	h.clk.Advance(time.Minute)
	acked, err := h.e.Ack(h.ctx, nasID, "alice@example.com")
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if acked.AckedAt == nil || !acked.AckedAt.Equal(h.clk.Now()) || acked.AckedBy != "alice@example.com" || acked.State != model.AlertOpen {
		t.Errorf("acked alert = %+v", acked)
	}
	stored, err := h.st.GetAlert(h.ctx, nasID)
	if err != nil || stored.AckedAt == nil || stored.AckedBy != "alice@example.com" {
		t.Errorf("ack not persisted: %+v (%v)", stored, err)
	}
	if evs := h.src.eventsOfType(model.EventAlertAcked); len(evs) != 1 || evs[0].Data["ackedBy"] != "alice@example.com" {
		t.Errorf("alert.acked events = %+v", evs)
	}

	h.clk.Advance(2 * time.Minute)
	h.tick(dev("nas", "nas", true), dev("pi", "pi", true))
	if len(h.openAlerts()) != 0 {
		t.Fatal("alerts still open after devices came back")
	}
	for _, a := range h.allAlerts() {
		if a.State != model.AlertResolved || a.ResolvedAt == nil || !a.ResolvedAt.Equal(h.clk.Now()) {
			t.Errorf("alert %d not resolved: %+v", a.ID, a)
		}
		if a.Message != "Back online after 3m" {
			t.Errorf("resolved message = %q", a.Message)
		}
		if a.DeviceID == "nas" && (a.AckedAt == nil || a.AckedBy != "alice@example.com") {
			t.Errorf("ack lost on resolve: %+v", a)
		}
	}
	got := h.nf.sent()
	var resolvedFor []model.DeviceID
	for _, n := range got {
		if n.Type == model.EventAlertResolved {
			resolvedFor = append(resolvedFor, n.Alert.DeviceID)
		}
	}
	if len(resolvedFor) != 1 || resolvedFor[0] != "pi" {
		t.Errorf("resolved notifications for %v, want only pi (nas was acked)", resolvedFor)
	}
	if evs := h.src.eventsOfType(model.EventAlertResolved); len(evs) != 2 {
		t.Errorf("resolution events = %d, want 2 (ack suppresses notifications, not events)", len(evs))
	}
}

func TestAckErrors(t *testing.T) {
	h := newHarness(t)
	h.setRule("device_offline", func(r *model.AlertRule) { r.ForSeconds = 0 })
	h.tick(dev("nas", "nas", false))
	a := h.onlyOpen()

	if _, err := h.e.Ack(h.ctx, 9999, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Ack(unknown) = %v, want store.ErrNotFound", err)
	}
	if _, err := h.e.Ack(h.ctx, 0, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Ack(0) = %v, want store.ErrNotFound", err)
	}
	first, err := h.e.Ack(h.ctx, a.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	again, err := h.e.Ack(h.ctx, a.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if again.AckedBy != "alice" || !again.AckedAt.Equal(*first.AckedAt) {
		t.Errorf("second ack changed the record: %+v", again)
	}
	if n := len(h.src.eventsOfType(model.EventAlertAcked)); n != 1 {
		t.Errorf("acked events = %d, want 1", n)
	}
	h.tick(dev("nas", "nas", true))
	if _, err := h.e.Ack(h.ctx, a.ID, "x"); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Ack(resolved) = %v, want ErrNotOpen", err)
	}
}

func TestScoping(t *testing.T) {
	tests := []struct {
		name  string
		scope func(r *model.AlertRule)
		tags  []string
		want  bool
	}{
		{"no scope matches everything", func(r *model.AlertRule) {}, nil, true},
		{"includeTags without the tag", func(r *model.AlertRule) { r.IncludeTags = []string{"tag:server"} }, nil, false},
		{"includeTags with the tag", func(r *model.AlertRule) { r.IncludeTags = []string{"tag:server"} }, []string{"tag:server"}, true},
		{"excludeTags with the tag", func(r *model.AlertRule) { r.ExcludeTags = []string{"tag:server"} }, []string{"tag:server", "tag:x"}, false},
		{"excludeTags without the tag", func(r *model.AlertRule) { r.ExcludeTags = []string{"tag:server"} }, []string{"tag:x"}, true},
		{"includeDevices by id", func(r *model.AlertRule) { r.IncludeDevice = []model.DeviceID{"vm"} }, nil, true},
		{"includeDevices by name", func(r *model.AlertRule) { r.IncludeDevice = []model.DeviceID{"Cloud-VM"} }, nil, true},
		{"includeDevices other device", func(r *model.AlertRule) { r.IncludeDevice = []model.DeviceID{"other"} }, nil, false},
		{"excludeDevices by id", func(r *model.AlertRule) { r.ExcludeDevice = []model.DeviceID{"vm"} }, nil, false},
		{"include tag but excluded device", func(r *model.AlertRule) {
			r.IncludeTags = []string{"tag:server"}
			r.ExcludeDevice = []model.DeviceID{"vm"}
		}, []string{"tag:server"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.setRule("update_available", tc.scope)
			d := dev("vm", "cloud-vm", true)
			d.Tags = tc.tags
			d.UpdateAvailable = true
			d.ClientVersion = "1.80.0"
			h.tick(d)
			got := len(h.openAlerts()) == 1
			if got != tc.want {
				t.Errorf("alert fired = %v, want %v", got, tc.want)
			}
			if got {
				a := h.onlyOpen()
				if a.Title != "Update available for cloud-vm" || a.Message != "Running Tailscale 1.80.0; a newer version is available" {
					t.Errorf("alert text = %q / %q", a.Title, a.Message)
				}
			}
		})
	}
}

func TestNewDeviceAutoResolve(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	known := dev("old", "old-box", true)
	if err := st.UpsertDevices(ctx, []model.Device{known}); err != nil {
		t.Fatal(err)
	}
	h := newHarnessOn(t, st, &fakeClock{t: base})

	newbie := dev("new", "newbie", true)
	newbie.FirstSeen = base.Add(-10 * time.Second)
	newbie.Tags = []string{"tag:server"}
	newbie.User = ""
	stale := dev("stale", "stale-box", true) // unknown to the store but not recently first seen
	h.tick(known, newbie, stale)
	a := h.onlyOpen()
	if a.RuleID != "new_device" || a.DeviceID != "new" || a.Severity != model.SeverityInfo {
		t.Fatalf("alert = %+v", a)
	}
	if a.Title != "New device: newbie" || a.Message != "newbie joined the tailnet (linux, tag:server) at 11:59" {
		t.Errorf("alert text = %q / %q", a.Title, a.Message)
	}

	// Subsequent ticks neither duplicate nor resolve it early.
	h.clk.Advance(time.Hour)
	h.tick(known, newbie, stale)
	if b := h.onlyOpen(); b.ID != a.ID {
		t.Fatalf("new_device alert duplicated")
	}
	if n := len(h.allAlerts()); n != 1 {
		t.Fatalf("alerts = %d, want 1", n)
	}

	h.clk.Advance(23 * time.Hour)
	h.tick(known, newbie, stale)
	if len(h.openAlerts()) != 0 {
		t.Fatal("new_device alert did not auto-resolve after 24h")
	}
	all := h.allAlerts()
	if all[0].Message != "Auto-resolved after 1d" {
		t.Errorf("resolved message = %q", all[0].Message)
	}
	if got := h.nf.sent(); len(got) != 2 || got[1].Type != model.EventAlertResolved {
		t.Errorf("notifications = %+v", got)
	}
}

func TestRestartSeedsOpenAlerts(t *testing.T) {
	st := newStore(t)
	clk := &fakeClock{t: base}
	h1 := newHarnessOn(t, st, clk)
	h1.setRule("device_offline", func(r *model.AlertRule) { r.ForSeconds = 0 })
	h1.tick(dev("nas", "nas", false))
	a := h1.onlyOpen()

	// "Restart": a fresh engine on the same store.
	clk.Advance(10 * time.Minute)
	h2 := newHarnessOn(t, st, clk)
	h2.e.mu.RLock()
	seeded := len(h2.e.open)
	h2.e.mu.RUnlock()
	if seeded != 1 {
		t.Fatalf("seeded open alerts = %d, want 1", seeded)
	}
	// Still offline: no duplicate alert.
	h2.tick(dev("nas", "nas", false))
	if n := len(h2.allAlerts()); n != 1 {
		t.Fatalf("alerts after restart tick = %d, want 1 (no duplicate)", n)
	}
	// Back online: the original alert resolves.
	clk.Advance(time.Minute)
	h2.tick(dev("nas", "nas", true))
	all := h2.allAlerts()
	if len(all) != 1 || all[0].ID != a.ID || all[0].State != model.AlertResolved {
		t.Fatalf("expected alert %d resolved, got %+v", a.ID, all)
	}
	if all[0].Message != "Back online after 11m" {
		t.Errorf("resolved message = %q", all[0].Message)
	}
	if got := h2.nf.sent(); len(got) != 1 || got[0].Type != model.EventAlertResolved {
		t.Errorf("notifications after restart = %+v", got)
	}
}

func TestMetricsAvailability(t *testing.T) {
	h := newHarness(t)
	h.setRule("high_cpu", func(r *model.AlertRule) { r.ForSeconds = 0 })

	// No metrics at all: metric rules are skipped.
	h.tick(dev("pi", "pi", true))
	if len(h.openAlerts()) != 0 {
		t.Fatal("metric rule fired without metrics")
	}

	hot := withMetrics(dev("pi", "pi", true), h.clk.Now(), model.MetricsSnapshot{CPUPercent: 99})
	h.tick(hot)
	h.onlyOpen()

	// Agent becomes unreachable: the carried-over metrics stay valid for a while.
	h.clk.Advance(2 * time.Minute)
	stale := hot
	stale.Agent.State = model.AgentUnreachable
	stale.Agent.LastError = "dial tcp: i/o timeout"
	h.tick(stale)
	if len(h.openAlerts()) != 1 {
		t.Fatal("alert resolved while metrics were still fresh")
	}

	// ...but not forever: the CPU alert resolves, and by now the agent has
	// been unreachable long enough for agent_unreachable to fire instead.
	h.clk.Advance(metricsStaleAfter)
	h.tick(stale)
	open := h.openAlerts()
	if len(open) != 1 || open[0].RuleID != "agent_unreachable" {
		t.Fatalf("open alerts = %+v, want only agent_unreachable", open)
	}
	if open[0].Message != "Agent unreachable for 7m (last success 12:00): dial tcp: i/o timeout" {
		t.Errorf("agent_unreachable message = %q", open[0].Message)
	}
	for _, a := range h.allAlerts() {
		if a.RuleID == "high_cpu" && (a.State != model.AlertResolved || a.Message != "Agent metrics no longer available") {
			t.Errorf("high_cpu alert on stale metrics = %+v", a)
		}
	}
}

func TestRuleChangesResolveOpenAlerts(t *testing.T) {
	t.Run("disabled rule", func(t *testing.T) {
		h := newHarness(t)
		d := dev("vm", "vm", true)
		d.UpdateAvailable = true
		h.tick(d)
		h.onlyOpen()
		h.setRule("update_available", func(r *model.AlertRule) { r.Enabled = false })
		h.clk.Advance(time.Minute)
		h.tick(d)
		if len(h.openAlerts()) != 0 {
			t.Fatal("alert of a disabled rule stayed open")
		}
		if all := h.allAlerts(); all[0].Message != "Rule no longer applies to this device" {
			t.Errorf("message = %q", all[0].Message)
		}
	})
	t.Run("device gone", func(t *testing.T) {
		h := newHarness(t)
		d := dev("vm", "vm", true)
		d.UpdateAvailable = true
		h.tick(d, dev("other", "other", true))
		h.onlyOpen()
		h.tick() // empty snapshot: collector hiccup, nothing changes
		h.onlyOpen()
		h.tick(dev("other", "other", true))
		if len(h.openAlerts()) != 0 {
			t.Fatal("alert for a vanished device stayed open")
		}
		if all := h.allAlerts(); all[0].Message != "Device no longer present" {
			t.Errorf("message = %q", all[0].Message)
		}
	})
}

func TestPendingTimerCleanup(t *testing.T) {
	h := newHarness(t)
	hot := withMetrics(dev("pi", "pi", true), h.clk.Now(), model.MetricsSnapshot{CPUPercent: 99})
	h.tick(hot)
	h.e.mu.RLock()
	_, pending := h.e.since[alertKey("high_cpu", "pi")]
	h.e.mu.RUnlock()
	if !pending {
		t.Fatal("expected a pending timer for high_cpu|pi")
	}
	// The device disappears: its pending timer must not linger.
	h.tick(dev("other", "other", true))
	h.e.mu.RLock()
	n := len(h.e.since)
	h.e.mu.RUnlock()
	if n != 0 {
		t.Errorf("since map has %d stale entries", n)
	}
	// And when it comes back, the timer restarts from scratch.
	h.clk.Advance(700 * time.Second)
	h.tick(withMetrics(dev("pi", "pi", true), h.clk.Now(), model.MetricsSnapshot{CPUPercent: 99}))
	if len(h.openAlerts()) != 0 {
		t.Error("high_cpu fired on a stale pending timer")
	}
}

func TestSaveRuleValidation(t *testing.T) {
	valid := func(r *model.AlertRule) {}
	tests := []struct {
		name    string
		id      string
		mutate  func(r *model.AlertRule)
		wantErr error
	}{
		{"unknown id", "nope", valid, ErrUnknownRule},
		{"empty id", "", valid, ErrUnknownRule},
		{"bad severity", "high_cpu", func(r *model.AlertRule) { r.Severity = "fatal" }, ErrInvalidRule},
		{"empty severity", "high_cpu", func(r *model.AlertRule) { r.Severity = "" }, ErrInvalidRule},
		{"negative threshold", "high_cpu", func(r *model.AlertRule) { r.Threshold = -1 }, ErrInvalidRule},
		{"negative forSeconds", "high_cpu", func(r *model.AlertRule) { r.ForSeconds = -1 }, ErrInvalidRule},
		{"type change", "high_cpu", func(r *model.AlertRule) { r.Type = model.RuleDiskFull }, ErrInvalidRule},
		{"unknown type", "high_cpu", func(r *model.AlertRule) { r.Type = "bogus" }, ErrInvalidRule},
		{"name too long", "high_cpu", func(r *model.AlertRule) { r.Name = strings.Repeat("x", maxRuleNameLen+1) }, ErrInvalidRule},
		{"valid", "high_cpu", func(r *model.AlertRule) {
			r.Threshold = 75
			r.ForSeconds = 120
			r.Severity = model.SeverityCritical
			r.Name = "  CPU hot  "
			r.IncludeTags = []string{" tag:server ", "", "tag:server"}
			r.ExcludeDevice = []model.DeviceID{"abc", ""}
		}, nil},
		{"empty type is filled in", "high_cpu", func(r *model.AlertRule) { r.Type = "" }, nil},
		{"empty name keeps the old one", "high_cpu", func(r *model.AlertRule) { r.Name = "" }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			var in model.AlertRule
			for _, r := range h.e.Rules() {
				if r.ID == "high_cpu" {
					in = r
				}
			}
			in.ID = tc.id
			tc.mutate(&in)
			out, err := h.e.SaveRule(h.ctx, in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SaveRule err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SaveRule: %v", err)
			}
			if out.ID != "high_cpu" || out.Type != model.RuleHighCPU || out.Name == "" || out.UpdatedAt.IsZero() {
				t.Errorf("saved rule = %+v", out)
			}
			if tc.name == "valid" {
				if out.Name != "CPU hot" || out.Threshold != 75 || out.ForSeconds != 120 || out.Severity != model.SeverityCritical {
					t.Errorf("saved rule = %+v", out)
				}
				if len(out.IncludeTags) != 1 || out.IncludeTags[0] != "tag:server" || len(out.ExcludeDevice) != 1 {
					t.Errorf("scope lists not cleaned: %+v", out)
				}
			}
			// Persisted and visible through Rules().
			stored, err := h.st.ListRules(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range stored {
				if r.ID == "high_cpu" && r.Threshold != out.Threshold {
					t.Errorf("stored threshold %v != returned %v", r.Threshold, out.Threshold)
				}
			}
			for _, r := range h.e.Rules() {
				if r.ID == "high_cpu" && (r.Threshold != out.Threshold || r.Name != out.Name) {
					t.Errorf("in-memory rule not reloaded: %+v", r)
				}
			}
		})
	}
}

func TestTestNotification(t *testing.T) {
	st := newStore(t)
	src := newFakeSource()
	good := &fakeNotifier{name: "webhook"}
	bad := &fakeNotifier{name: "ntfy", err: errors.New("ntfy: https://ntfy.sh responded with status 403")}
	e := newEngine(st, src, []Notifier{good, nil, bad}, testLogger(), (&fakeClock{t: base}).Now)

	sent, errs := e.Test(context.Background(), "  hello  ")
	if len(sent) != 1 || sent[0] != "webhook" {
		t.Errorf("sent = %v", sent)
	}
	if len(errs) != 1 || !strings.Contains(errs["ntfy"], "status 403") {
		t.Errorf("errs = %v", errs)
	}
	for _, nf := range []*fakeNotifier{good, bad} {
		got := nf.sent()
		if len(got) != 1 {
			t.Fatalf("%s got %d notifications", nf.name, len(got))
		}
		n := got[0]
		if n.Type != model.EventAlertOpened || n.Alert.Title != "Test notification" || n.Alert.Message != "hello" ||
			n.Alert.Severity != model.SeverityInfo || n.Hub.SelfName != "hub" {
			t.Errorf("%s notification = %+v", nf.name, n)
		}
	}
	sent, errs = (&Engine{now: time.Now}).Test(context.Background(), "")
	if sent == nil || errs == nil || len(sent) != 0 || len(errs) != 0 {
		t.Errorf("no notifiers: sent=%v errs=%v", sent, errs)
	}
}

func TestQueueFullDropsWithoutBlocking(t *testing.T) {
	h := newHarness(t)
	a := model.Alert{ID: 1, RuleID: "x"}
	for i := 0; i < notifyQueueSize+5; i++ {
		h.e.enqueue(model.EventAlertOpened, a) // must never block
	}
	if len(h.e.queue) != notifyQueueSize {
		t.Errorf("queue length = %d, want %d", len(h.e.queue), notifyQueueSize)
	}
}

func TestRunEndToEnd(t *testing.T) {
	h := newHarness(t)
	h.setRule("device_offline", func(r *model.AlertRule) { r.ForSeconds = 0 })
	changes, unsub := h.e.Changes().Subscribe()
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.e.Run(ctx) }()

	// A second Run is refused while the first is active.
	waitFor(t, "engine running", func() bool { return h.e.running.Load() })
	if err := h.e.Run(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Run = %v, want ErrAlreadyRunning", err)
	}

	waitFor(t, "tick subscription", func() bool { return h.src.ticks.Len() == 1 })
	h.src.ticks.Publish(model.Snapshot{Overview: model.Overview{Hub: h.src.hub}, Devices: []model.Device{dev("nas", "nas", false)}})
	waitFor(t, "tick processed", func() bool { return h.e.processed.Load() == 1 })

	select {
	case a := <-changes:
		if a.State != model.AlertOpen || a.DeviceID != "nas" {
			t.Errorf("published alert = %+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no alert published on Changes()")
	}
	waitFor(t, "async notification", func() bool { return len(h.nf.sent()) == 1 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v after cancel, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if h.src.ticks.Len() != 0 {
		t.Error("Run left its tick subscription behind")
	}
}

func TestRunWithoutStoreOrSource(t *testing.T) {
	e := NewEngine(nil, nil, nil, testLogger())
	if err := e.Run(context.Background()); err == nil {
		t.Error("Run without a source must fail")
	}
	if _, err := e.SaveRule(context.Background(), model.AlertRule{ID: "high_cpu"}); err == nil {
		t.Error("SaveRule without a store must fail")
	}
	if _, err := e.Ack(context.Background(), 1, "x"); err == nil {
		t.Error("Ack without a store must fail")
	}
	if n := len(e.Rules()); n != len(DefaultRules()) {
		t.Errorf("Rules() without a store = %d, want built-in defaults", n)
	}
	e2 := NewEngine(newStore(t), nil, nil, nil) // nil logger falls back to slog.Default
	if err := e2.Run(context.Background()); err == nil {
		t.Error("Run without a source must fail")
	}
}
