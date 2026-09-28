package collector

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentclient"
	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

var errTest = errors.New("boom")

func TestAgentMetricsMapping(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
	h.agents.setReport("100.64.0.2", report(baseTime, 1000, 500))
	h.poll()

	d := h.device("p1")
	if d.Agent.State != model.AgentReachable || d.Agent.Version != "0.3.0" || d.Agent.URL != "http://100.64.0.2:41820" ||
		d.Agent.LastSuccess == nil || !d.Agent.LastSuccess.Equal(baseTime) || d.Agent.LastError != "" {
		t.Fatalf("agent status: %+v", d.Agent)
	}
	m := d.Metrics
	if m == nil {
		t.Fatal("metrics missing")
	}
	if m.CPUPercent != 37.5 || m.CPUCount != 2 || m.MemPercent != 50 || m.Load1 != 0.7 || m.UptimeSeconds != 172800 ||
		m.Platform != "ubuntu" || m.PlatformVersion != "24.04" || m.Kernel != "6.8" || m.Arch != "amd64" ||
		m.BootTime == nil || m.TailscaleIf != "tailscale0" || m.TailscaleVer != "1.86.0" || m.Processes != 123 ||
		len(m.PerCore) != 2 || m.MemTotal != 8<<30 || m.SwapTotal != 1<<30 {
		t.Errorf("metrics: %+v", m)
	}
	if m.DiskPercent != 42 || len(m.Disks) != 2 || m.Disks[0].Mount != "/data" {
		t.Errorf("disk: pct=%v disks=%+v", m.DiskPercent, m.Disks)
	}
	if m.NetRxRate != 0 || m.NetTxRate != 0 || len(m.Interfaces) != 2 || m.Interfaces[0].RxBytes != 1000 {
		t.Errorf("first report rates must be 0: %+v", m.Interfaces)
	}
	if len(m.Temperatures) != 2 || m.Temperatures[1].Critical != 90 {
		t.Errorf("temperatures: %+v", m.Temperatures)
	}
	evs := h.drainEvents()
	if e, ok := findEvent(evs, model.EventAgentReachable, "p1"); !ok || !strings.Contains(e.Message, "0.3.0") {
		t.Errorf("agent.reachable event missing: %v", eventTypes(evs))
	}

	// Second report 10s later: eth0 +3000/+1500 → 300/150 B/s; tailscale0 is
	// not physical and must not count towards the totals.
	h.agents.setReport("100.64.0.2", report(baseTime.Add(10*time.Second), 4000, 2000))
	h.pollAfter(10 * time.Second)
	m = h.device("p1").Metrics
	if m.NetRxRate != 300 || m.NetTxRate != 150 {
		t.Errorf("net rates: rx=%v tx=%v, want 300/150", m.NetRxRate, m.NetTxRate)
	}
	if m.Interfaces[0].RxRate != 300 || m.Interfaces[0].TxRate != 150 || m.Interfaces[1].RxRate != 3000 {
		t.Errorf("per-interface rates: %+v", m.Interfaces)
	}
	sm, err := h.st.LatestSample(t.Context(), "p1")
	if err != nil {
		t.Fatalf("latest sample: %v", err)
	}
	if !sm.AgentOK || floatVal(sm.CPU) != 37.5 || floatVal(sm.Mem) != 50 || floatVal(sm.Disk) != 42 ||
		floatVal(sm.Load1) != 0.7 || floatVal(sm.NetRxRate) != 300 || floatVal(sm.NetTxRate) != 150 ||
		floatVal(sm.TempC) != 65 || sm.Uptime == nil || *sm.Uptime != 172800 {
		t.Errorf("sample metrics: %+v", sm)
	}
	if h.drainEvents() != nil {
		t.Errorf("no agent events expected while it stays reachable")
	}

	// Counter reset (reboot) → 0, never negative.
	h.agents.setReport("100.64.0.2", report(baseTime.Add(20*time.Second), 100, 50))
	h.pollAfter(10 * time.Second)
	if m := h.device("p1").Metrics; m.NetRxRate != 0 || m.NetTxRate != 0 {
		t.Errorf("rates after counter reset: %v/%v", m.NetRxRate, m.NetTxRate)
	}
}

func TestBuildMetricsTable(t *testing.T) {
	base := report(baseTime, 1000, 1000)
	noPrimary := report(baseTime, 1000, 1000)
	for i := range noPrimary.Disks {
		noPrimary.Disks[i].Primary = false
	}
	cases := []struct {
		name      string
		rep, prev *agentproto.Report
		wantDisk  float64
		wantRx    float64
	}{
		{"primary disk wins", report(baseTime.Add(time.Second), 2000, 1000), base, 42, 1000},
		{"no primary → max", noPrimary, nil, 80, 0},
		{"no disks → 0", &agentproto.Report{SampledAt: baseTime}, nil, 0, 0},
		{"identical sampledAt → 0 rate", report(baseTime, 5000, 1000), base, 42, 0},
		{"gap over 15 minutes resets baseline", report(baseTime.Add(20*time.Minute), 5000, 1000), base, 42, 0},
		{"nil report", nil, nil, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildMetrics(tc.rep, tc.prev)
			if tc.rep == nil {
				if m != nil {
					t.Fatalf("nil report must give nil metrics")
				}
				return
			}
			if m.DiskPercent != tc.wantDisk {
				t.Errorf("disk=%v, want %v", m.DiskPercent, tc.wantDisk)
			}
			if m.NetRxRate != tc.wantRx {
				t.Errorf("rx=%v, want %v", m.NetRxRate, tc.wantRx)
			}
			if m.Disks == nil || m.Interfaces == nil {
				t.Errorf("slices must be non-nil for JSON")
			}
		})
	}
}

func TestAgentBackoff(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
	h.agents.setErr("100.64.0.2", errTest)
	h.poll()
	d := h.device("p1")
	if d.Agent.State != model.AgentUnreachable || !strings.Contains(d.Agent.LastError, "boom") {
		t.Fatalf("agent after failure: %+v", d.Agent)
	}
	if h.agents.callCount("100.64.0.2") != 1 {
		t.Fatalf("calls=%d", h.agents.callCount("100.64.0.2"))
	}
	if evs := h.drainEvents(); countType(evs, model.EventAgentUnreachable) != 0 {
		t.Errorf("device that never had an agent must not emit agent.unreachable: %v", eventTypes(evs))
	}

	// Backoff 30s: polls at +15s skip, +30s retries; then 60s.
	steps := []struct {
		after time.Duration
		calls int
	}{
		{15 * time.Second, 1}, {15 * time.Second, 2}, // retried at 30s → failure #2, next in 60s
		{15 * time.Second, 2}, {15 * time.Second, 2}, {15 * time.Second, 2}, {15 * time.Second, 3}, // 90s → failure #3, next in 120s
	}
	for i, s := range steps {
		h.pollAfter(s.after)
		if got := h.agents.callCount("100.64.0.2"); got != s.calls {
			t.Fatalf("step %d (t=%s): calls=%d, want %d", i, h.now.Sub(baseTime), got, s.calls)
		}
	}
	if d := h.device("p1"); d.Agent.State != model.AgentUnreachable {
		t.Errorf("state during backoff: %v", d.Agent.State)
	}

	// Recovery resets the schedule: the next poll fetches again immediately.
	h.agents.setReport("100.64.0.2", report(h.now.Add(2*time.Minute), 10, 10))
	h.pollAfter(2 * time.Minute)
	if d := h.device("p1"); d.Agent.State != model.AgentReachable {
		t.Fatalf("agent after recovery: %+v", d.Agent)
	}
	if evs := h.drainEvents(); countType(evs, model.EventAgentReachable) != 1 {
		t.Errorf("agent.reachable expected once: %v", eventTypes(evs))
	}
	before := h.agents.callCount("100.64.0.2")
	h.pollAfter(15 * time.Second)
	if h.agents.callCount("100.64.0.2") != before+1 {
		t.Errorf("no backoff expected after success")
	}

	// reachable → failure emits agent.unreachable with the error text; the
	// stale metrics are kept while the device is online.
	h.agents.setErr("100.64.0.2", fmt.Errorf("fetch http://100.64.0.2:41820/v1/metrics: %w", errTest))
	h.pollAfter(15 * time.Second)
	evs := h.drainEvents()
	e, ok := findEvent(evs, model.EventAgentUnreachable, "p1")
	if !ok || !strings.Contains(e.Message, "boom") || e.Severity != model.SeverityInfo {
		t.Errorf("agent.unreachable event: %+v (%v)", e, eventTypes(evs))
	}
	if d := h.device("p1"); d.Metrics == nil || d.Agent.LastSuccess == nil {
		t.Errorf("last metrics/success should be retained while online")
	}
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); countType(evs, model.EventAgentUnreachable) != 0 {
		t.Errorf("agent.unreachable must not repeat: %v", eventTypes(evs))
	}
}

func TestAgentBackoffUnauthorizedAndOffline(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
	h.agents.setErr("100.64.0.2", fmt.Errorf("agentclient: %w", &agentclient.StatusError{Code: 403}))
	h.poll()
	if h.agents.callCount("100.64.0.2") != 1 {
		t.Fatal("expected one attempt")
	}
	h.pollAfter(30 * time.Second)
	h.pollAfter(5 * time.Minute)
	if h.agents.callCount("100.64.0.2") != 1 {
		t.Errorf("unauthorized must back off 10 minutes at once: calls=%d", h.agents.callCount("100.64.0.2"))
	}
	h.pollAfter(5 * time.Minute)
	if h.agents.callCount("100.64.0.2") != 2 {
		t.Errorf("expected retry after 10 minutes: calls=%d", h.agents.callCount("100.64.0.2"))
	}

	// Going offline clears the agent state; coming back resets the backoff.
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2", offline())))
	h.pollAfter(15 * time.Second)
	if d := h.device("p1"); d.Agent.State != model.AgentUnknown || d.Metrics != nil {
		t.Errorf("offline device agent: %+v metrics=%v", d.Agent, d.Metrics)
	}
	h.agents.setReport("100.64.0.2", report(h.now, 1, 1))
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
	h.pollAfter(15 * time.Second)
	if d := h.device("p1"); d.Agent.State != model.AgentReachable {
		t.Errorf("backoff must reset when the device comes back online: %+v", d.Agent)
	}
}

func TestAgentDisabledAndConcurrency(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		cfg := defaultCfg()
		cfg.AgentEnabled = false
		h := newHarness(t, cfg)
		h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
		h.agents.setReport("100.64.0.2", report(baseTime, 1, 1))
		h.poll()
		if d := h.device("p1"); d.Agent.State != model.AgentDisabled || d.Metrics != nil {
			t.Errorf("agent must be disabled: %+v", d.Agent)
		}
		if h.agents.callCount("100.64.0.2") != 0 {
			t.Errorf("agent client must not be used when disabled")
		}
	})
	t.Run("semaphore", func(t *testing.T) {
		cfg := defaultCfg()
		cfg.AgentConcurrency = 3
		h := newHarness(t, cfg)
		h.agents.delay = 5 * time.Millisecond
		var peers = []model.DeviceID{}
		st := hubStatus()
		for i := 2; i < 12; i++ {
			ip := fmt.Sprintf("100.64.0.%d", i)
			st.Peers = append(st.Peers, peer(fmt.Sprintf("p%d", i), fmt.Sprintf("dev%d", i), ip))
			h.agents.setReport(ip, report(baseTime, 1, 1))
			peers = append(peers, model.DeviceID(fmt.Sprintf("p%d", i)))
		}
		h.local.setStatus(st)
		h.poll()
		if h.agents.maxFlight > 3 {
			t.Errorf("max concurrent fetches %d exceeds semaphore 3", h.agents.maxFlight)
		}
		for _, id := range peers {
			if d := h.device(string(id)); d.Agent.State != model.AgentReachable {
				t.Errorf("%s: %v", id, d.Agent.State)
			}
		}
		if snap := h.c.Snapshot(); snap.Overview.AgentsUp != 10 {
			t.Errorf("AgentsUp=%d, want 10", snap.Overview.AgentsUp)
		}
	})
	t.Run("self is fetched", func(t *testing.T) {
		h := newHarness(t, defaultCfg())
		h.agents.setReport("100.64.0.1", report(baseTime, 1, 1))
		h.poll()
		if d := h.device("self"); d.Agent.State != model.AgentReachable {
			t.Errorf("self agent: %+v", d.Agent)
		}
	})
}

func TestBackoffDelay(t *testing.T) {
	c := &Collector{jitter: func() float64 { return 0.5 }}
	cases := []struct {
		failures     int
		unauthorized bool
		want         time.Duration
	}{
		{1, false, 30 * time.Second}, {2, false, time.Minute}, {3, false, 2 * time.Minute},
		{5, false, 8 * time.Minute}, {6, false, 10 * time.Minute}, {50, false, 10 * time.Minute},
		{1, true, 10 * time.Minute},
	}
	for _, tc := range cases {
		if got := c.backoffDelay(tc.failures, tc.unauthorized); got != tc.want {
			t.Errorf("backoffDelay(%d,%v)=%v, want %v", tc.failures, tc.unauthorized, got, tc.want)
		}
	}
	// Jitter stays within +/-15%.
	for _, j := range []float64{0, 1} {
		c.jitter = func() float64 { return j }
		got := c.backoffDelay(1, false)
		if got < 25*time.Second || got > 35*time.Second {
			t.Errorf("jittered delay %v out of range", got)
		}
	}
}

func TestIsUnauthorized(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errTest, false},
		{agentclient.ErrUnauthorized, true},
		{fmt.Errorf("wrap: %w", &agentclient.StatusError{Code: 401}), true},
		{fmt.Errorf("wrap: %w", &agentclient.StatusError{Code: 500}), false},
		{errors.New("agent returned HTTP 403: forbidden"), true},
		{errors.New("request unauthorized by policy"), true},
	}
	for _, tc := range cases {
		if got := isUnauthorized(tc.err); got != tc.want {
			t.Errorf("isUnauthorized(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}
