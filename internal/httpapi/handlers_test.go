package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/tslocal"
)

func TestReadEndpoints(t *testing.T) {
	h := newHarness(t)

	t.Run("me", func(t *testing.T) {
		var id model.Identity
		w := h.do("GET", "/api/v1/me", nil, ipAdmin)
		expect(t, w, 200, "")
		decode(t, w, &id)
		if id.Login != "alice@example.com" || id.Role != model.RoleAdmin {
			t.Errorf("identity = %+v", id)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q", ct)
		}
	})

	t.Run("overview", func(t *testing.T) {
		var ov model.Overview
		w := h.do("GET", "/api/v1/overview", nil, ipViewer)
		expect(t, w, 200, "")
		decode(t, w, &ov)
		if ov.Devices != 3 || ov.Hub.SelfName == "" {
			t.Errorf("overview = %+v", ov)
		}
	})

	t.Run("settings has no secrets", func(t *testing.T) {
		h.cfg.APIKey = "tskey-api-SECRET"
		defer func() { h.cfg.APIKey = "" }()
		w := h.do("GET", "/api/v1/settings", nil, ipViewer)
		expect(t, w, 200, "")
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Errorf("settings leak: %s", w.Body.String())
		}
		var st model.Settings
		decode(t, w, &st)
		if st.AuthMode != "tailscale" || !st.ControlAPI || st.Store.Devices != 3 {
			t.Errorf("settings = %+v", st)
		}
	})

	t.Run("devices sorted by name", func(t *testing.T) {
		var devs []model.Device
		w := h.do("GET", "/api/v1/devices", nil, ipViewer)
		expect(t, w, 200, "")
		decode(t, w, &devs)
		if len(devs) != 3 || devs[0].Name != "hub" || devs[1].Name != "laptop" || devs[2].Name != "nas" {
			names := make([]string, 0, len(devs))
			for _, d := range devs {
				names = append(names, d.Name)
			}
			t.Errorf("devices = %v", names)
		}
	})

	t.Run("device by id and by name", func(t *testing.T) {
		for _, key := range []string{deviceLaptop, "laptop", "LAPTOP", "laptop.tail.ts.net"} {
			var det model.DeviceDetail
			w := h.do("GET", "/api/v1/devices/"+key, nil, ipViewer)
			expect(t, w, 200, "")
			decode(t, w, &det)
			if det.Device.ID != deviceLaptop || det.Uptime24h == nil || det.RecentEvents == nil || det.OpenAlerts == nil {
				t.Errorf("%s: detail = %+v", key, det)
			}
			if len(det.RecentEvents) == 0 {
				t.Errorf("%s: expected the device.new event", key)
			}
		}
		expect(t, h.do("GET", "/api/v1/devices/nope", nil, ipViewer), 404, codeNotFound)
		expect(t, h.do("GET", "/api/v1/devices/%20", nil, ipViewer), 400, codeBadRequest)
	})

	t.Run("topology", func(t *testing.T) {
		var top model.Topology
		w := h.do("GET", "/api/v1/network/topology", nil, ipViewer)
		expect(t, w, 200, "")
		decode(t, w, &top)
		if len(top.Nodes) != 3 || len(top.Edges) != 2 {
			t.Errorf("topology = %d nodes %d edges", len(top.Nodes), len(top.Edges))
		}
	})

	t.Run("rules", func(t *testing.T) {
		var rules []model.AlertRule
		w := h.do("GET", "/api/v1/alerts/rules", nil, ipViewer)
		expect(t, w, 200, "")
		decode(t, w, &rules)
		if len(rules) != 13 {
			t.Errorf("rules = %d", len(rules))
		}
	})

	t.Run("refresh is 202", func(t *testing.T) {
		expect(t, h.do("POST", "/api/v1/refresh", nil, ipAdmin), 202, "")
	})
}

func TestSeriesRangeParsing(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		rng     string
		status  int
		wantDur time.Duration
	}{
		{"", 200, 24 * time.Hour},
		{"15m", 200, 15 * time.Minute},
		{"1h", 200, time.Hour},
		{"24h", 200, 24 * time.Hour},
		{"7d", 200, 7 * 24 * time.Hour},
		{"30d", 200, 30 * 24 * time.Hour},
		{"2h30m", 200, 150 * time.Minute},
		{"90d", 200, 90 * 24 * time.Hour},
		{"91d", 400, 0},
		{"2161h", 400, 0},
		{"30s", 400, 0},
		{"0", 400, 0},
		{"-1h", 400, 0},
		{"abc", 400, 0},
		{"1%20hour", 400, 0},
	}
	for _, tc := range tests {
		t.Run("range="+tc.rng, func(t *testing.T) {
			w := h.do("GET", "/api/v1/devices/laptop/series?range="+tc.rng, nil, ipViewer)
			if tc.status == 400 {
				expect(t, w, 400, codeBadRequest)
				return
			}
			expect(t, w, 200, "")
			var s model.Series
			decode(t, w, &s)
			if s.DeviceID != deviceLaptop || !s.To.Equal(baseTime) || s.To.Sub(s.From) != tc.wantDur {
				t.Errorf("series window = %s..%s (%s), want %s", s.From, s.To, s.To.Sub(s.From), tc.wantDur)
			}
			if s.Points == nil {
				t.Error("points must not be null")
			}
			// Uptime uses the same parser.
			u := h.do("GET", "/api/v1/devices/laptop/uptime?range="+tc.rng, nil, ipViewer)
			expect(t, u, 200, "")
			var rep model.UptimeReport
			decode(t, u, &rep)
			if rep.To.Sub(rep.From) != tc.wantDur || rep.Segments == nil {
				t.Errorf("uptime window = %s", rep.To.Sub(rep.From))
			}
		})
	}
}

func TestSeriesReturnsStoredSamples(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cpu := 42.0
	samples := []model.Sample{}
	for i := 0; i < 4; i++ {
		samples = append(samples, model.Sample{DeviceID: deviceLaptop, TS: baseTime.Add(-time.Duration(i+1) * 15 * time.Second), Online: true, CPU: &cpu})
	}
	if err := h.st.InsertSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	var s model.Series
	w := h.do("GET", "/api/v1/devices/laptop/series?range=1h", nil, ipViewer)
	expect(t, w, 200, "")
	decode(t, w, &s)
	if s.Source != "raw" || s.StepSec != 15 || len(s.Points) != 4 || s.Points[0].CPU == nil || *s.Points[0].CPU != 42 {
		t.Errorf("series = %+v", s)
	}
}

func TestEventsAndAlertsQueries(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ev := &model.Event{TS: baseTime.Add(-time.Duration(i) * time.Minute), Type: model.EventDeviceOffline, Severity: model.SeverityWarning, DeviceID: deviceNAS, Device: "nas", Title: "offline"}
		if err := h.st.InsertEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	a := &model.Alert{RuleID: "device_offline", RuleType: model.RuleDeviceOffline, DeviceID: deviceNAS, DeviceName: "nas", State: model.AlertOpen, Severity: model.SeverityWarning, Title: "nas offline", OpenedAt: baseTime}
	if err := h.st.OpenAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	r := &model.Alert{RuleID: "high_cpu", RuleType: model.RuleHighCPU, DeviceID: deviceLaptop, DeviceName: "laptop", State: model.AlertResolved, Severity: model.SeverityInfo, Title: "cpu", OpenedAt: baseTime.Add(-time.Hour)}
	if err := h.st.OpenAlert(ctx, r); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path   string
		status int
		count  int
	}{
		{"/api/v1/events", 200, -1},
		{"/api/v1/events?type=device.offline", 200, 5},
		{"/api/v1/events?type=device.offline&limit=2", 200, 2},
		{"/api/v1/events?type=device.offline,device.online", 200, 5},
		{"/api/v1/events?device=nas&type=device.offline", 200, 5},
		{"/api/v1/events?device=" + deviceNAS + "&type=device.offline", 200, 5},
		{"/api/v1/events?device=laptop&type=device.offline", 200, 0},
		{"/api/v1/events?type=device.offline&since=" + baseTime.Add(-90*time.Second).Format(time.RFC3339), 200, 2},
		{"/api/v1/events?since=yesterday", 400, 0},
		{"/api/v1/events?before=yesterday", 400, 0},
		{"/api/v1/events?limit=abc", 400, 0},
		{"/api/v1/events?limit=-1", 400, 0},
		{"/api/v1/events?limit=100000&type=device.offline", 200, 5},
		{"/api/v1/alerts", 200, 2},
		{"/api/v1/alerts?state=all", 200, 2},
		{"/api/v1/alerts?state=open", 200, 1},
		{"/api/v1/alerts?state=resolved", 200, 1},
		{"/api/v1/alerts?state=OPEN", 200, 1},
		{"/api/v1/alerts?state=bogus", 400, 0},
		{"/api/v1/alerts?device=nas", 200, 1},
		{"/api/v1/alerts?device=laptop&state=open", 200, 0},
		{"/api/v1/alerts?limit=1", 200, 1},
		{"/api/v1/alerts?limit=x", 400, 0},
		{"/api/v1/devices/laptop/events?limit=zz", 400, 0},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			w := h.do("GET", tc.path, nil, ipViewer)
			if tc.status == 400 {
				expect(t, w, 400, codeBadRequest)
				return
			}
			expect(t, w, 200, "")
			var list []map[string]any
			decode(t, w, &list)
			if tc.count >= 0 && len(list) != tc.count {
				t.Errorf("got %d items, want %d", len(list), tc.count)
			}
		})
	}
}

func TestAck(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := &model.Alert{RuleID: "device_offline", RuleType: model.RuleDeviceOffline, DeviceID: deviceNAS, DeviceName: "nas", State: model.AlertOpen, Severity: model.SeverityWarning, Title: "nas offline", OpenedAt: baseTime}
	if err := h.st.OpenAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	changes, unsub := h.eng.Changes().Subscribe()
	defer unsub()

	w := h.do("POST", "/api/v1/alerts/1/ack", nil, ipAdmin)
	expect(t, w, 200, "")
	var got model.Alert
	decode(t, w, &got)
	if got.AckedBy != "alice@example.com" || got.AckedAt == nil {
		t.Errorf("ack = %+v", got)
	}
	select {
	case c := <-changes:
		if c.ID != 1 {
			t.Errorf("change id = %d", c.ID)
		}
	default:
		t.Error("ack not published on Changes()")
	}
	expect(t, h.do("POST", "/api/v1/alerts/999/ack", nil, ipAdmin), 404, codeNotFound)
	expect(t, h.do("POST", "/api/v1/alerts/x/ack", nil, ipAdmin), 400, codeBadRequest)
	expect(t, h.do("POST", "/api/v1/alerts/0/ack", nil, ipAdmin), 400, codeBadRequest)

	// Newest first: the failed ack of #999, then the successful ack of #1.
	// Malformed ids never reach the engine and are not audited.
	entries := h.auditEntries()
	if len(entries) != 2 || entries[0].OK || entries[0].Target != "999" ||
		entries[1].Action != "alert.ack" || !entries[1].OK || entries[1].Actor != "alice@example.com" || entries[1].Target != "1" {
		t.Errorf("audit = %+v", entries)
	}
}

func TestSaveRule(t *testing.T) {
	h := newHarness(t)
	body := model.AlertRule{ID: "ignored", Type: model.RuleHighCPU, Name: "CPU hot", Enabled: true, Severity: model.SeverityCritical, Threshold: 95, ForSeconds: 120, Notify: false}
	w := h.do("PUT", "/api/v1/alerts/rules/high_cpu", body, ipAdmin)
	expect(t, w, 200, "")
	var saved model.AlertRule
	decode(t, w, &saved)
	if saved.ID != "high_cpu" || saved.Threshold != 95 || saved.Severity != model.SeverityCritical || saved.Name != "CPU hot" || saved.Notify {
		t.Errorf("saved = %+v", saved)
	}
	// Persisted and visible through the engine.
	found := false
	for _, r := range h.eng.Rules() {
		if r.ID == "high_cpu" && r.Threshold == 95 {
			found = true
		}
	}
	if !found {
		t.Error("rule not persisted")
	}
	entries := h.auditEntries()
	if len(entries) != 1 || entries[0].Action != "rule.update" || entries[0].Target != "high_cpu" || !entries[0].OK || entries[0].Details["threshold"] != 95.0 {
		t.Errorf("audit = %+v", entries)
	}
	evs := h.adminEvents()
	if len(evs) != 1 || evs[0].Severity != model.SeverityInfo || !strings.Contains(evs[0].Title, "update rule") {
		t.Errorf("events = %+v", evs)
	}

	tests := []struct {
		name   string
		id     string
		body   any
		status int
		code   string
	}{
		{"unknown rule", "nope", body, 404, codeNotFound},
		{"invalid severity", "high_cpu", model.AlertRule{Type: model.RuleHighCPU, Severity: "loud", Threshold: 1}, 400, codeBadRequest},
		{"type change", "high_cpu", model.AlertRule{Type: model.RuleDiskFull, Severity: model.SeverityInfo}, 400, codeBadRequest},
		{"negative threshold", "high_cpu", model.AlertRule{Type: model.RuleHighCPU, Severity: model.SeverityInfo, Threshold: -1}, 400, codeBadRequest},
		{"malformed json", "high_cpu", "{not json", 400, codeBadRequest},
		{"empty body", "high_cpu", "", 400, codeBadRequest},
		{"trailing data", "high_cpu", `{"type":"high_cpu","severity":"info"} {}`, 400, codeBadRequest},
		{"wrong content type", "high_cpu", "x", 400, codeBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := []reqOpt{}
			if tc.name == "wrong content type" {
				opts = append(opts, hdr("Content-Type", "text/plain"))
			}
			w := h.do("PUT", "/api/v1/alerts/rules/"+tc.id, tc.body, ipAdmin, opts...)
			expect(t, w, tc.status, tc.code)
		})
	}
	// Failed attempts are audited as failures.
	var failed int
	for _, e := range h.auditEntries() {
		if e.Action == "rule.update" && !e.OK {
			failed++
		}
	}
	if failed != len(tests) {
		t.Errorf("failed rule audits = %d, want %d", failed, len(tests))
	}
}

func TestAlertTest(t *testing.T) {
	h := newHarness(t)
	w := h.do("POST", "/api/v1/alerts/test", map[string]string{"message": "hello"}, ipAdmin)
	expect(t, w, 200, "")
	var res struct {
		Sent   []string          `json:"sent"`
		Errors map[string]string `json:"errors"`
	}
	decode(t, w, &res)
	if len(res.Sent) != 1 || res.Sent[0] != "fake" || len(res.Errors) != 0 {
		t.Errorf("result = %+v", res)
	}
	h.notif.mu.Lock()
	if len(h.notif.sent) != 1 || h.notif.sent[0].Alert.Message != "hello" {
		t.Errorf("notifier got %+v", h.notif.sent)
	}
	h.notif.mu.Unlock()
	// Empty body uses the default message.
	w = h.do("POST", "/api/v1/alerts/test", nil, ipAdmin)
	expect(t, w, 200, "")
	// Notifier failure is reported, not an HTTP error.
	h.notif.mu.Lock()
	h.notif.err = context.DeadlineExceeded
	h.notif.mu.Unlock()
	w = h.do("POST", "/api/v1/alerts/test", nil, ipAdmin)
	expect(t, w, 200, "")
	decode(t, w, &res)
	if len(res.Sent) != 0 || res.Errors["fake"] == "" {
		t.Errorf("result = %+v", res)
	}
	entries := h.auditEntries()
	if len(entries) != 3 || entries[0].OK || !entries[1].OK || entries[1].Action != "alerts.test" {
		t.Errorf("audit = %+v", entries)
	}
}

func TestAuditList(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/v1/alerts/test", nil, ipAdmin)
	w := h.do("GET", "/api/v1/audit?limit=10", nil, ipAdmin)
	expect(t, w, 200, "")
	var entries []model.AuditEntry
	decode(t, w, &entries)
	if len(entries) != 1 || entries[0].RemoteIP != ipAdmin || entries[0].ActorNode != "alice-mbp.tail.ts.net" {
		t.Errorf("audit = %+v", entries)
	}
	expect(t, h.do("GET", "/api/v1/audit?limit=q", nil, ipAdmin), 400, codeBadRequest)
}

func TestPing(t *testing.T) {
	h := newHarness(t)
	h.local.mu.Lock()
	h.local.pings = map[string]*source.PingReply{"100.64.0.2": {LatencyMs: 12.5, Endpoint: "203.0.113.1:41641", NodeName: "laptop"}}
	h.local.mu.Unlock()

	w := h.do("POST", "/api/v1/devices/laptop/ping", nil, ipViewer)
	expect(t, w, 200, "")
	var res model.PingResult
	decode(t, w, &res)
	if res.DeviceID != deviceLaptop || res.LatencyMs != 12.5 || res.Path != model.PathDirect || res.Err != "" {
		t.Errorf("ping = %+v", res)
	}
	// Daemon-reported failure is a 200 with Err set.
	w = h.do("POST", "/api/v1/devices/nas/ping", nil, ipViewer)
	expect(t, w, 200, "")
	decode(t, w, &res)
	if res.Err == "" {
		t.Errorf("expected ping error, got %+v", res)
	}
	expect(t, h.do("POST", "/api/v1/devices/ghost/ping", nil, ipViewer), 404, codeNotFound)
	// A LocalAPI failure (not a ping timeout) is an upstream error whose
	// operator-facing details (socket path, hints) never reach the caller.
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("tslocal: ping 100.64.0.2: %w at /var/run/tailscale/tailscaled.sock (hint: run as root): %w", tslocal.ErrDaemonUnavailable, errors.New("dial unix: no such file")), "tailscaled is unavailable on the hub"},
		{fmt.Errorf("tslocal: ping 100.64.0.2: %w (hint: add the user to the socket group): %w", tslocal.ErrPermissionDenied, errors.New("permission denied")), "tailscaled is unavailable on the hub"},
		{errors.New("localapi: connection refused to /run/secret.sock"), "upstream request failed"},
	} {
		h.local.mu.Lock()
		h.local.pingErr = tc.err
		h.local.mu.Unlock()
		w := h.do("POST", "/api/v1/devices/laptop/ping", nil, ipViewer)
		expect(t, w, http.StatusBadGateway, codeUpstream)
		var eb errorBody
		decode(t, w, &eb)
		if eb.Error.Message != tc.want {
			t.Errorf("ping error %v: message %q, want %q", tc.err, eb.Error.Message, tc.want)
		}
		for _, leak := range []string{".sock", "hint", "permission"} {
			if strings.Contains(w.Body.String(), leak) {
				t.Errorf("ping error body leaks %q: %s", leak, w.Body.String())
			}
		}
	}
}
