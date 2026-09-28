package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

func TestEvents(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	if err := s.InsertEvent(ctx, nil); err == nil {
		t.Error("InsertEvent(nil) succeeded")
	}
	events := []*model.Event{
		{TS: at(100), Type: model.EventDeviceOffline, Severity: model.SeverityWarning, DeviceID: "d1", Device: "one", Title: "one offline", Data: map[string]any{"path": "relay", "n": float64(3)}},
		{TS: at(200), Type: model.EventDeviceOnline, DeviceID: "d1", Device: "one", Title: "one online"},
		{TS: at(300), Type: model.EventDeviceOffline, Severity: model.SeverityWarning, DeviceID: "d2", Device: "two", Title: "two offline"},
		{TS: at(300), Type: model.EventHubStarted, Title: "hub started"},
		{Type: model.EventAlertOpened, Severity: model.SeverityCritical, DeviceID: "d2", Title: "alert"}, // TS defaults to now
	}
	for i, e := range events {
		if err := s.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent %d: %v", i, err)
		}
		if e.ID != int64(i+1) {
			t.Errorf("event %d id = %d", i, e.ID)
		}
	}
	if events[1].Severity != model.SeverityInfo {
		t.Errorf("empty severity should default to info, got %q", events[1].Severity)
	}
	if !events[4].TS.Equal(fixedNow) {
		t.Errorf("zero TS should default to now, got %v", events[4].TS)
	}

	ids := func(evs []model.Event) []int64 {
		out := make([]int64, len(evs))
		for i, e := range evs {
			out[i] = e.ID
		}
		return out
	}
	eq := func(a, b []int64) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	tests := []struct {
		name string
		q    model.EventQuery
		want []int64
	}{
		{"all newest first", model.EventQuery{}, []int64{5, 4, 3, 2, 1}},
		{"limit", model.EventQuery{Limit: 2}, []int64{5, 4}},
		{"device", model.EventQuery{DeviceID: "d1"}, []int64{2, 1}},
		{"single type", model.EventQuery{Types: []model.EventType{model.EventDeviceOffline}}, []int64{3, 1}},
		{"multiple types", model.EventQuery{Types: []model.EventType{model.EventDeviceOffline, model.EventHubStarted}}, []int64{4, 3, 1}},
		{"since inclusive", model.EventQuery{Since: at(200)}, []int64{5, 4, 3, 2}},
		{"before exclusive", model.EventQuery{Before: at(300)}, []int64{2, 1}},
		{"since+before+device", model.EventQuery{Since: at(100), Before: at(300), DeviceID: "d1"}, []int64{2, 1}},
		{"type+device", model.EventQuery{Types: []model.EventType{model.EventDeviceOffline}, DeviceID: "d2"}, []int64{3}},
		{"no match", model.EventQuery{DeviceID: "nobody"}, []int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListEvents(ctx, tc.q)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if got == nil {
				t.Fatal("result nil")
			}
			if !eq(ids(got), tc.want) {
				t.Errorf("ids = %v, want %v", ids(got), tc.want)
			}
		})
	}

	got, err := s.ListEvents(ctx, model.EventQuery{DeviceID: "d1", Types: []model.EventType{model.EventDeviceOffline}})
	if err != nil || len(got) != 1 {
		t.Fatalf("ListEvents = %v, %v", got, err)
	}
	e := got[0]
	if e.Type != model.EventDeviceOffline || e.Severity != model.SeverityWarning || e.Device != "one" || e.Title != "one offline" || !e.TS.Equal(at(100)) {
		t.Errorf("event round trip = %+v", e)
	}
	if e.Data["path"] != "relay" || e.Data["n"] != float64(3) {
		t.Errorf("event data = %v", e.Data)
	}
	if got, _ := s.ListEvents(ctx, model.EventQuery{Types: []model.EventType{model.EventHubStarted}}); len(got) != 1 || got[0].Data != nil {
		t.Errorf("event without data should have nil Data: %+v", got)
	}

	// Corrupt JSON data is tolerated.
	if _, err := s.db.ExecContext(ctx, `UPDATE events SET data = '{bad' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListEvents(ctx, model.EventQuery{DeviceID: "d1"})
	if err != nil || len(got) != 2 || got[1].Data != nil {
		t.Errorf("corrupt data handling = %+v, %v", got, err)
	}
}

func TestAlertsLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	if err := s.OpenAlert(ctx, nil); err == nil {
		t.Error("OpenAlert(nil) succeeded")
	}
	if err := s.OpenAlert(ctx, &model.Alert{}); err == nil {
		t.Error("OpenAlert without rule id succeeded")
	}
	if _, err := s.GetAlert(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAlert missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateAlert(ctx, &model.Alert{ID: 42, RuleID: "x", State: model.AlertOpen}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAlert missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateAlert(ctx, &model.Alert{ID: 0}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAlert id 0 = %v, want ErrNotFound", err)
	}
	if err := s.UpdateAlert(ctx, &model.Alert{ID: 1, RuleID: "x", State: ""}); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAlert with empty state = %v, want validation error", err)
	}

	a1 := &model.Alert{RuleID: "high_cpu", RuleType: model.RuleHighCPU, DeviceID: "d1", DeviceName: "one", Severity: model.SeverityWarning,
		Title: "CPU high", Message: "cpu 95%", Value: fp(95), OpenedAt: at(100), Data: map[string]any{"threshold": float64(90)}}
	if err := s.OpenAlert(ctx, a1); err != nil {
		t.Fatalf("OpenAlert: %v", err)
	}
	if a1.ID != 1 || a1.State != model.AlertOpen || !a1.UpdatedAt.Equal(at(100)) {
		t.Errorf("defaults after open: %+v", a1)
	}
	a2 := &model.Alert{RuleID: "device_offline", RuleType: model.RuleDeviceOffline, DeviceID: "d2", Title: "offline", OpenedAt: at(200)}
	if err := s.OpenAlert(ctx, a2); err != nil {
		t.Fatal(err)
	}
	a3 := &model.Alert{RuleID: "device_offline", DeviceID: "d1", Title: "offline", OpenedAt: at(300)}
	if err := s.OpenAlert(ctx, a3); err != nil {
		t.Fatal(err)
	}
	if a2.Severity != model.SeverityInfo || a3.ID != 3 {
		t.Errorf("a2 severity %q, a3 id %d", a2.Severity, a3.ID)
	}

	got, err := s.GetAlert(ctx, a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleID != "high_cpu" || got.RuleType != model.RuleHighCPU || got.DeviceName != "one" || got.Value == nil || *got.Value != 95 ||
		got.ResolvedAt != nil || got.AckedAt != nil || got.Data["threshold"] != float64(90) || !got.OpenedAt.Equal(at(100)) {
		t.Errorf("GetAlert round trip = %+v", got)
	}

	open, err := s.OpenAlertsByKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 || open["high_cpu|d1"] == nil || open["device_offline|d2"] == nil || open["device_offline|d1"] == nil {
		t.Errorf("OpenAlertsByKey = %v", open)
	}

	// Acknowledge a1.
	acked := at(150)
	a1.AckedAt = &acked
	a1.AckedBy = "alice@example.com"
	a1.UpdatedAt = time.Time{}
	if err := s.UpdateAlert(ctx, a1); err != nil {
		t.Fatalf("UpdateAlert ack: %v", err)
	}
	if !a1.UpdatedAt.Equal(fixedNow) {
		t.Errorf("UpdateAlert should set zero UpdatedAt to now, got %v", a1.UpdatedAt)
	}
	got, _ = s.GetAlert(ctx, a1.ID)
	if got.AckedAt == nil || !got.AckedAt.Equal(acked) || got.AckedBy != "alice@example.com" || got.State != model.AlertOpen {
		t.Errorf("after ack = %+v", got)
	}

	// Resolve a1 with escalated severity and new value/message/data.
	resolved := at(400)
	a1.State = model.AlertResolved
	a1.ResolvedAt = &resolved
	a1.Severity = model.SeverityCritical
	a1.Message = "recovered"
	a1.Value = fp(12)
	a1.Data = nil
	a1.UpdatedAt = at(400)
	if err := s.UpdateAlert(ctx, a1); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAlert(ctx, a1.ID)
	if got.State != model.AlertResolved || got.ResolvedAt == nil || !got.ResolvedAt.Equal(resolved) || got.Severity != model.SeverityCritical ||
		got.Message != "recovered" || got.Value == nil || *got.Value != 12 || got.Data != nil || !got.UpdatedAt.Equal(at(400)) {
		t.Errorf("after resolve = %+v", got)
	}
	open, _ = s.OpenAlertsByKey(ctx)
	if len(open) != 2 || open["high_cpu|d1"] != nil {
		t.Errorf("OpenAlertsByKey after resolve = %v", open)
	}

	// Listing: newest first, filters, default limit.
	tests := []struct {
		name string
		q    model.AlertQuery
		want []int64
	}{
		{"all", model.AlertQuery{}, []int64{3, 2, 1}},
		{"open", model.AlertQuery{State: model.AlertOpen}, []int64{3, 2}},
		{"resolved", model.AlertQuery{State: model.AlertResolved}, []int64{1}},
		{"device", model.AlertQuery{DeviceID: "d1"}, []int64{3, 1}},
		{"device+state", model.AlertQuery{DeviceID: "d1", State: model.AlertOpen}, []int64{3}},
		{"limit", model.AlertQuery{Limit: 1}, []int64{3}},
		{"none", model.AlertQuery{DeviceID: "zzz"}, []int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListAlerts(ctx, tc.q)
			if err != nil {
				t.Fatalf("ListAlerts: %v", err)
			}
			if got == nil {
				t.Fatal("nil result")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].ID != tc.want[i] {
					t.Errorf("[%d] id = %d, want %d", i, got[i].ID, tc.want[i])
				}
			}
		})
	}
}

func TestRules(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	rules, err := s.ListRules(ctx)
	if err != nil || rules == nil || len(rules) != 0 {
		t.Errorf("empty ListRules = %v, %v", rules, err)
	}
	if err := s.SaveRule(ctx, nil); err == nil {
		t.Error("SaveRule(nil) succeeded")
	}
	if err := s.SaveRule(ctx, &model.AlertRule{ID: " "}); err == nil {
		t.Error("SaveRule with blank id succeeded")
	}

	r1 := &model.AlertRule{ID: "high_cpu", Type: model.RuleHighCPU, Name: "High CPU", Enabled: true, Severity: model.SeverityWarning,
		Threshold: 90, ForSeconds: 600, IncludeTags: []string{"tag:server"}, ExcludeDevice: []model.DeviceID{"d9"}, Notify: true}
	r2 := &model.AlertRule{ID: "device_offline", Type: model.RuleDeviceOffline, Name: "Offline", Enabled: false, Threshold: 0, ForSeconds: 300}
	for _, r := range []*model.AlertRule{r1, r2} {
		if err := s.SaveRule(ctx, r); err != nil {
			t.Fatalf("SaveRule %s: %v", r.ID, err)
		}
		if !r.UpdatedAt.Equal(fixedNow) {
			t.Errorf("SaveRule should set UpdatedAt, got %v", r.UpdatedAt)
		}
	}
	rules, err = s.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].ID != "device_offline" || rules[1].ID != "high_cpu" {
		t.Fatalf("ListRules = %+v", rules)
	}
	got := rules[1]
	if got.Type != model.RuleHighCPU || got.Threshold != 90 || got.ForSeconds != 600 || !got.Enabled || !got.Notify ||
		len(got.IncludeTags) != 1 || len(got.ExcludeDevice) != 1 || !got.UpdatedAt.Equal(fixedNow) {
		t.Errorf("rule round trip = %+v", got)
	}

	// Saving again replaces.
	r1.Threshold = 80
	r1.Enabled = false
	if err := s.SaveRule(ctx, r1); err != nil {
		t.Fatal(err)
	}
	rules, _ = s.ListRules(ctx)
	if len(rules) != 2 || rules[1].Threshold != 80 || rules[1].Enabled {
		t.Errorf("after replace = %+v", rules)
	}

	// Corrupt rows are skipped.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO alert_rules(id, data, updated_at) VALUES ('bad', 'nope', 0)`); err != nil {
		t.Fatal(err)
	}
	rules, err = s.ListRules(ctx)
	if err != nil || len(rules) != 2 {
		t.Errorf("with corrupt row = %d rules, %v", len(rules), err)
	}
}

func TestAudit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "")

	if err := s.InsertAudit(ctx, nil); err == nil {
		t.Error("InsertAudit(nil) succeeded")
	}
	list, err := s.ListAudit(ctx, 0)
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("empty ListAudit = %v, %v", list, err)
	}
	entries := []*model.AuditEntry{
		{TS: at(100), Actor: "alice@example.com", ActorNode: "laptop", Action: "device.authorize", Target: "d1",
			Details: map[string]any{"authorized": true}, OK: true, RemoteIP: "100.64.0.2"},
		{TS: at(200), Actor: "bob@example.com", Action: "device.delete", Target: "d2", OK: false, Error: "upstream: 403"},
		{Actor: "alice@example.com", Action: "rule.update", Target: "high_cpu", OK: true}, // TS defaults
	}
	for i, e := range entries {
		if err := s.InsertAudit(ctx, e); err != nil {
			t.Fatalf("InsertAudit %d: %v", i, err)
		}
		if e.ID != int64(i+1) {
			t.Errorf("audit %d id = %d", i, e.ID)
		}
	}
	if !entries[2].TS.Equal(fixedNow) {
		t.Errorf("zero TS should default to now, got %v", entries[2].TS)
	}
	list, err = s.ListAudit(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != 3 || list[1].ID != 2 || list[2].ID != 1 {
		t.Fatalf("ListAudit order = %+v", list)
	}
	e := list[2]
	if e.Actor != "alice@example.com" || e.ActorNode != "laptop" || e.Action != "device.authorize" || e.Target != "d1" ||
		!e.OK || e.RemoteIP != "100.64.0.2" || e.Details["authorized"] != true || !e.TS.Equal(at(100)) {
		t.Errorf("audit round trip = %+v", e)
	}
	if list[1].OK || list[1].Error != "upstream: 403" || list[1].Details != nil {
		t.Errorf("failed entry = %+v", list[1])
	}
	list, _ = s.ListAudit(ctx, 2)
	if len(list) != 2 || list[0].ID != 3 {
		t.Errorf("ListAudit(2) = %+v", list)
	}
}
