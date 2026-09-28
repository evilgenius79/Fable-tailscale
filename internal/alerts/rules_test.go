package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func TestDefaultRules(t *testing.T) {
	want := []struct {
		typ       model.AlertRuleType
		severity  model.Severity
		threshold float64
		forSec    int
	}{
		{model.RuleDeviceOffline, model.SeverityWarning, 0, 300},
		{model.RuleHighCPU, model.SeverityWarning, 90, 600},
		{model.RuleHighMemory, model.SeverityWarning, 90, 600},
		{model.RuleDiskFull, model.SeverityWarning, 90, 0},
		{model.RuleHighLatency, model.SeverityInfo, 250, 300},
		{model.RuleRelayOnly, model.SeverityInfo, 0, 900},
		{model.RuleKeyExpiring, model.SeverityWarning, 7, 0},
		{model.RuleUpdateAvailable, model.SeverityInfo, 0, 0},
		{model.RuleAgentUnreachable, model.SeverityWarning, 0, 300},
		{model.RuleNewDevice, model.SeverityInfo, 0, 86400},
		{model.RuleUnauthorized, model.SeverityWarning, 0, 0},
		{model.RuleHighTemp, model.SeverityWarning, 85, 300},
		{model.RuleHighLoad, model.SeverityInfo, 2.0, 600},
	}
	rules := DefaultRules()
	if len(rules) != len(want) || len(rules) != len(ruleSpecs) {
		t.Fatalf("DefaultRules() = %d rules, want %d (one per type)", len(rules), len(want))
	}
	byType := map[model.AlertRuleType]model.AlertRule{}
	for _, r := range rules {
		byType[r.Type] = r
	}
	for _, w := range want {
		r, ok := byType[w.typ]
		if !ok {
			t.Errorf("missing default for %s", w.typ)
			continue
		}
		if r.ID != string(w.typ) {
			t.Errorf("%s: ID = %q, want the type", w.typ, r.ID)
		}
		if r.Severity != w.severity || r.Threshold != w.threshold || r.ForSeconds != w.forSec {
			t.Errorf("%s: got sev=%s thr=%v for=%d, want sev=%s thr=%v for=%d",
				w.typ, r.Severity, r.Threshold, r.ForSeconds, w.severity, w.threshold, w.forSec)
		}
		if !r.Enabled || !r.Notify || r.Name == "" || r.Description == "" {
			t.Errorf("%s: must be enabled, notifying and described: %+v", w.typ, r)
		}
	}
	// Each call returns an independent copy.
	rules[0].Name = "mutated"
	if DefaultRules()[0].Name == "mutated" {
		t.Error("DefaultRules() must return a fresh slice")
	}
}

func TestRuleAppliesTo(t *testing.T) {
	d := &model.Device{ID: "abc", Name: "Laptop", Tags: []string{"tag:server", "tag:eu"}}
	tests := []struct {
		name string
		rule model.AlertRule
		want bool
	}{
		{"empty scope", model.AlertRule{}, true},
		{"include tag hit", model.AlertRule{IncludeTags: []string{"tag:eu"}}, true},
		{"include tag miss", model.AlertRule{IncludeTags: []string{"tag:us"}}, false},
		{"exclude tag hit", model.AlertRule{ExcludeTags: []string{"tag:eu"}}, false},
		{"include device id", model.AlertRule{IncludeDevice: []model.DeviceID{"abc"}}, true},
		{"include device name case-insensitive", model.AlertRule{IncludeDevice: []model.DeviceID{"laptop"}}, true},
		{"include device miss", model.AlertRule{IncludeDevice: []model.DeviceID{"zzz"}}, false},
		{"exclude device", model.AlertRule{ExcludeDevice: []model.DeviceID{"abc"}}, false},
		{"include tag but excluded", model.AlertRule{IncludeTags: []string{"tag:eu"}, ExcludeDevice: []model.DeviceID{"abc"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleAppliesTo(&tc.rule, d); got != tc.want {
				t.Errorf("ruleAppliesTo = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestConditions exercises each rule's condition function directly.
func TestConditions(t *testing.T) {
	now := base
	rule := func(typ model.AlertRuleType, thr float64, forSec int) *model.AlertRule {
		return &model.AlertRule{ID: string(typ), Type: typ, Severity: model.SeverityWarning, Threshold: thr, ForSeconds: forSec}
	}
	tests := []struct {
		name       string
		rule       *model.AlertRule
		device     func() model.Device
		ec         evalContext
		wantActive bool
		wantSev    model.Severity
		wantValue  *float64
		wantMsg    string // substring of message (active) or resolved text (inactive)
	}{
		{
			name: "unauthorized device",
			rule: rule(model.RuleUnauthorized, 0, 0),
			device: func() model.Device {
				d := dev("x", "phone", false)
				d.Authorized = false
				d.Created = base.Add(-2 * time.Hour)
				return d
			},
			wantActive: true, wantSev: model.SeverityWarning,
			wantMsg: "Waiting for authorization since Mar 1 10:00 (alice@example.com)",
		},
		{
			name:       "authorized device is inactive",
			rule:       rule(model.RuleUnauthorized, 0, 0),
			device:     func() model.Device { return dev("x", "phone", true) },
			wantActive: false, wantMsg: "Device authorized",
		},
		{
			name: "relay only",
			rule: rule(model.RuleRelayOnly, 0, 900),
			device: func() model.Device {
				d := dev("x", "phone", true)
				d.Connectivity.Path = model.PathRelay
				d.Connectivity.Relay = "nyc"
				return d
			},
			ec:         evalContext{since: base.Add(-15 * time.Minute)},
			wantActive: true, wantSev: model.SeverityWarning,
			wantMsg: "Relayed via DERP nyc for 15m (no direct path)",
		},
		{
			name:       "direct path is inactive",
			rule:       rule(model.RuleRelayOnly, 0, 900),
			device:     func() model.Device { return dev("x", "phone", true) },
			wantActive: false, wantMsg: "Direct path restored",
		},
		{
			name: "agent unreachable after a previous success",
			rule: rule(model.RuleAgentUnreachable, 0, 300),
			device: func() model.Device {
				d := dev("x", "pi", true)
				d.Agent = model.AgentStatus{State: model.AgentUnreachable, LastSuccess: tp(base.Add(-7 * time.Minute)), LastError: "dial tcp 100.64.0.10:41820: i/o timeout"}
				return d
			},
			wantActive: true, wantSev: model.SeverityWarning,
			wantMsg: "Agent unreachable for 7m (last success 11:53): dial tcp 100.64.0.10:41820: i/o timeout",
		},
		{
			name: "agent never reachable is ignored",
			rule: rule(model.RuleAgentUnreachable, 0, 300),
			device: func() model.Device {
				d := dev("x", "pi", true)
				d.Agent = model.AgentStatus{State: model.AgentUnreachable, LastError: "connection refused"}
				return d
			},
			wantActive: false,
		},
		{
			name: "high latency",
			rule: rule(model.RuleHighLatency, 250, 300),
			device: func() model.Device {
				d := dev("x", "vm", true)
				d.Connectivity.LatencyMs = fp(312.4)
				return d
			},
			ec:         evalContext{since: base.Add(-5 * time.Minute)},
			wantActive: true, wantSev: model.SeverityWarning, wantValue: fp(312.4),
			wantMsg: "Latency at 312ms for 5m (threshold 250ms)",
		},
		{
			name: "latency of an offline device is inactive",
			rule: rule(model.RuleHighLatency, 250, 300),
			device: func() model.Device {
				d := dev("x", "vm", false)
				d.Connectivity.LatencyMs = fp(900)
				return d
			},
			wantActive: false,
		},
		{
			name: "high temperature uses the hottest sensor",
			rule: rule(model.RuleHighTemp, 85, 300),
			device: func() model.Device {
				return withMetrics(dev("x", "pi", true), now, model.MetricsSnapshot{
					Temperatures: []model.Temperature{{Sensor: "nvme", Celsius: 60}, {Sensor: "cpu_thermal", Celsius: 91.2}},
				})
			},
			wantActive: true, wantSev: model.SeverityWarning, wantValue: fp(91.2),
			wantMsg: "cpu_thermal at 91°C for 0s (threshold 85°C)",
		},
		{
			name: "no temperature sensors is inactive",
			rule: rule(model.RuleHighTemp, 85, 300),
			device: func() model.Device {
				return withMetrics(dev("x", "pi", true), now, model.MetricsSnapshot{CPUPercent: 10})
			},
			wantActive: false,
		},
		{
			name: "high load per core",
			rule: rule(model.RuleHighLoad, 2, 600),
			device: func() model.Device {
				return withMetrics(dev("x", "pi", true), now, model.MetricsSnapshot{Load1: 8.4, CPUCount: 2})
			},
			ec:         evalContext{since: base.Add(-10 * time.Minute)},
			wantActive: true, wantSev: model.SeverityWarning, wantValue: fp(4.2),
			wantMsg: "Load 8.40 on 2 cores (4.20 per core) for 10m (threshold 2.00)",
		},
		{
			name: "load below threshold",
			rule: rule(model.RuleHighLoad, 2, 600),
			device: func() model.Device {
				return withMetrics(dev("x", "pi", true), now, model.MetricsSnapshot{Load1: 1.0, CPUCount: 4})
			},
			wantActive: false, wantValue: fp(0.25), wantMsg: "Load back to 0.25 (threshold 2.00)",
		},
		{
			name: "memory high",
			rule: rule(model.RuleHighMemory, 90, 600),
			device: func() model.Device {
				return withMetrics(dev("x", "nas", true), now, model.MetricsSnapshot{MemPercent: 93.6})
			},
			wantActive: true, wantSev: model.SeverityWarning, wantValue: fp(93.6),
			wantMsg: "Memory at 94% for 0s (threshold 90%)",
		},
		{
			name: "key expiry disabled is skipped",
			rule: rule(model.RuleKeyExpiring, 7, 0),
			device: func() model.Device {
				d := dev("x", "vm", true)
				d.KeyExpiry = tp(base.Add(24 * time.Hour))
				d.KeyExpiryDisabled = true
				return d
			},
			wantActive: false,
		},
		{
			name: "expired key is skipped",
			rule: rule(model.RuleKeyExpiring, 7, 0),
			device: func() model.Device {
				d := dev("x", "vm", true)
				d.KeyExpiry = tp(base.Add(-time.Hour))
				d.Expired = true
				return d
			},
			wantActive: false,
		},
		{
			name: "key expiring within a day is critical",
			rule: rule(model.RuleKeyExpiring, 7, 0),
			device: func() model.Device {
				d := dev("x", "vm", true)
				d.KeyExpiry = tp(base.Add(30 * time.Hour))
				return d
			},
			wantActive: true, wantSev: model.SeverityCritical, wantValue: fp(1.3),
			wantMsg: "Node key expires in 1d 6h (Mar 2 18:00)",
		},
		{
			name: "update available without version",
			rule: rule(model.RuleUpdateAvailable, 0, 0),
			device: func() model.Device {
				d := dev("x", "vm", true)
				d.UpdateAvailable = true
				return d
			},
			wantActive: true, wantSev: model.SeverityWarning,
			wantMsg: "A newer Tailscale client is available",
		},
		{
			name:       "new device only when flagged new",
			rule:       rule(model.RuleNewDevice, 0, 86400),
			device:     func() model.Device { return dev("x", "vm", true) },
			ec:         evalContext{isNew: false},
			wantActive: false,
		},
		{
			name: "cpu with metrics from a disabled agent is skipped",
			rule: rule(model.RuleHighCPU, 90, 0),
			device: func() model.Device {
				d := withMetrics(dev("x", "vm", true), now, model.MetricsSnapshot{CPUPercent: 99})
				d.Agent.State = model.AgentDisabled
				return d
			},
			wantActive: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.device()
			ec := tc.ec
			ec.now = now
			obs := observe(tc.rule, &d, ec)
			if obs.active != tc.wantActive {
				t.Fatalf("active = %v, want %v (obs %+v)", obs.active, tc.wantActive, obs)
			}
			if tc.wantActive {
				if obs.severity != tc.wantSev {
					t.Errorf("severity = %s, want %s", obs.severity, tc.wantSev)
				}
				if obs.title == "" || obs.since.IsZero() {
					t.Errorf("active observation needs a title and start: %+v", obs)
				}
				if tc.wantMsg != "" && obs.message != tc.wantMsg {
					t.Errorf("message = %q, want %q", obs.message, tc.wantMsg)
				}
			} else if tc.wantMsg != "" && obs.resolved != tc.wantMsg {
				t.Errorf("resolved = %q, want %q", obs.resolved, tc.wantMsg)
			}
			switch {
			case tc.wantValue == nil && obs.value != nil:
				t.Errorf("value = %v, want nil", *obs.value)
			case tc.wantValue != nil && (obs.value == nil || *obs.value != *tc.wantValue):
				t.Errorf("value = %v, want %v", obs.value, *tc.wantValue)
			}
		})
	}
}

func TestFormatting(t *testing.T) {
	durations := map[time.Duration]string{
		0:                            "0s",
		45 * time.Second:             "45s",
		6*time.Minute + time.Second:  "6m",
		2*time.Hour + 13*time.Minute: "2h 13m",
		3 * time.Hour:                "3h",
		3*24*time.Hour + 4*time.Hour: "3d 4h",
		48 * time.Hour:               "2d",
		-5 * time.Second:             "0s",
	}
	for d, want := range durations {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := clockTime(base.Add(-2*time.Hour), base); got != "10:00" {
		t.Errorf("clockTime same day = %q", got)
	}
	if got := clockTime(base.Add(-30*time.Hour), base); got != "Feb 28 06:00" {
		t.Errorf("clockTime other day = %q", got)
	}
	if got := truncate("héllo wörld", 6); !strings.HasPrefix(got, "héll") || !strings.HasSuffix(got, "…") {
		t.Errorf("truncate = %q", got)
	}
	if got := newDeviceWindow(15); got != time.Minute {
		t.Errorf("newDeviceWindow(15s) = %v, want 1m", got)
	}
	if got := newDeviceWindow(60); got != 3*time.Minute {
		t.Errorf("newDeviceWindow(60s) = %v, want 3m", got)
	}
	if got := newDeviceWindow(0); got != time.Minute {
		t.Errorf("newDeviceWindow(0) = %v, want 1m", got)
	}
}
