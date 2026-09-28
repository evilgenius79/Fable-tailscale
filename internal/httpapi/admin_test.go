package httpapi

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestAdminActionsNotConfigured(t *testing.T) {
	tests := []struct {
		name string
		opt  harnessOpt
	}{
		{"flag off", func(h *harness) { h.api.configured = true }},
		{"api unconfigured", func(h *harness) { h.cfg.EnableAdminActions = true }},
		{"both off", func(*harness) {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.opt)
			w := h.do("POST", "/api/v1/devices/laptop/authorize", map[string]bool{"authorized": true}, ipAdmin)
			expect(t, w, http.StatusNotImplemented, codeNotConfigured)
			if len(h.auditEntries()) != 0 || len(h.api.recorded()) != 0 {
				t.Error("disabled actions must not reach the API or audit log")
			}
		})
	}
}

func TestAdminActionSuccessWritesAuditAndEvent(t *testing.T) {
	h := newHarness(t, withAdminActions())
	events, unsub := h.col.Events().Subscribe()
	defer unsub()

	w := h.do("POST", "/api/v1/devices/laptop/authorize", map[string]bool{"authorized": true}, ipAdmin)
	expect(t, w, 200, "")
	var dev model.Device
	decode(t, w, &dev)
	if dev.ID != deviceLaptop {
		t.Errorf("device = %+v", dev)
	}
	calls := h.api.recorded()
	if len(calls) != 1 || calls[0].method != "SetAuthorized" || calls[0].id != deviceLaptop || calls[0].args != true {
		t.Errorf("api calls = %+v", calls)
	}
	entries := h.auditEntries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d", len(entries))
	}
	e := entries[0]
	if !e.OK || e.Actor != "alice@example.com" || e.ActorNode != "alice-mbp.tail.ts.net" || e.RemoteIP != ipAdmin ||
		e.Action != "device.authorize" || e.Target != deviceLaptop || e.Details["authorized"] != true || e.Details["deviceName"] != "laptop" || e.Error != "" {
		t.Errorf("audit = %+v", e)
	}
	if !e.TS.Equal(baseTime) {
		t.Errorf("audit ts = %s", e.TS)
	}
	select {
	case ev := <-events:
		if ev.Type != model.EventAdminAction || ev.Severity != model.SeverityInfo || ev.DeviceID != deviceLaptop || ev.Data["actor"] != "alice@example.com" || ev.Data["ok"] != true {
			t.Errorf("event = %+v", ev)
		}
	default:
		t.Error("admin.action event not published")
	}
	if evs := h.adminEvents(); len(evs) != 1 {
		t.Errorf("stored admin events = %d", len(evs))
	}
}

func TestAdminActionFailures(t *testing.T) {
	h := newHarness(t, withAdminActions())
	tests := []struct {
		name    string
		method  string
		path    string
		body    any
		apiErr  error
		status  int
		code    string
		audited bool
		apiCall bool
	}{
		{"unknown device", "POST", "/api/v1/devices/ghost/authorize", map[string]bool{"authorized": true}, nil, 404, codeNotFound, true, false},
		{"missing field", "POST", "/api/v1/devices/laptop/authorize", map[string]string{"x": "y"}, nil, 400, codeBadRequest, true, false},
		{"empty body", "POST", "/api/v1/devices/laptop/authorize", nil, nil, 400, codeBadRequest, true, false},
		{"invalid json", "POST", "/api/v1/devices/laptop/authorize", "{", nil, 400, codeBadRequest, true, false},
		{"wrong type", "POST", "/api/v1/devices/laptop/authorize", map[string]string{"authorized": "yes"}, nil, 400, codeBadRequest, true, false},
		{"bad tag", "POST", "/api/v1/devices/laptop/tags", map[string][]string{"tags": {"tag:ok", "server"}}, nil, 400, codeBadRequest, true, false},
		{"bad tag chars", "POST", "/api/v1/devices/laptop/tags", map[string][]string{"tags": {"tag:Prod_1"}}, nil, 400, codeBadRequest, true, false},
		{"tags missing", "POST", "/api/v1/devices/laptop/tags", map[string]any{}, nil, 400, codeBadRequest, true, false},
		{"bad route", "POST", "/api/v1/devices/laptop/routes", map[string][]string{"routes": {"10.0.0.0/24", "nope"}}, nil, 400, codeBadRequest, true, false},
		{"routes missing", "POST", "/api/v1/devices/laptop/routes", map[string]any{}, nil, 400, codeBadRequest, true, false},
		{"bad name", "POST", "/api/v1/devices/laptop/name", map[string]string{"name": "-bad-"}, nil, 400, codeBadRequest, true, false},
		{"name too long", "POST", "/api/v1/devices/laptop/name", map[string]string{"name": strings.Repeat("a", 64)}, nil, 400, codeBadRequest, true, false},
		{"name with dot", "POST", "/api/v1/devices/laptop/name", map[string]string{"name": "a.b"}, nil, 400, codeBadRequest, true, false},
		{"name empty", "POST", "/api/v1/devices/laptop/name", map[string]string{"name": " "}, nil, 400, codeBadRequest, true, false},
		{"key expiry missing", "POST", "/api/v1/devices/laptop/key-expiry", map[string]any{}, nil, 400, codeBadRequest, true, false},
		{"upstream error", "POST", "/api/v1/devices/laptop/authorize", map[string]bool{"authorized": false}, errors.New("tsapi: 403 Forbidden"), http.StatusBadGateway, codeUpstream, true, true},
		{"upstream not found", "DELETE", "/api/v1/devices/laptop", nil, source.ErrNotFound, 404, codeNotFound, true, true},
		{"upstream not configured", "POST", "/api/v1/devices/laptop/name", map[string]string{"name": "ok"}, source.ErrNotConfigured, http.StatusNotImplemented, codeNotConfigured, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.api.mu.Lock()
			h.api.err = tc.apiErr
			h.api.calls = nil
			h.api.mu.Unlock()
			before := len(h.auditEntries())
			beforeEv := len(h.adminEvents())

			w := h.do(tc.method, tc.path, tc.body, ipAdmin)
			expect(t, w, tc.status, tc.code)

			entries := h.auditEntries()
			if got := len(entries) - before; (got == 1) != tc.audited {
				t.Fatalf("audit entries added = %d, want audited=%v", got, tc.audited)
			}
			if tc.audited {
				if entries[0].OK || entries[0].Error == "" {
					t.Errorf("audit entry should record the failure: %+v", entries[0])
				}
				evs := h.adminEvents()
				if len(evs)-beforeEv != 1 || evs[0].Severity != model.SeverityWarning {
					t.Errorf("expected one warning admin.action event, got %+v", evs)
				}
			}
			if got := len(h.api.recorded()) == 1; got != tc.apiCall {
				t.Errorf("api called = %v, want %v", got, tc.apiCall)
			}
		})
	}
	// The laptop still exists after the failed delete.
	if _, ok := h.col.Device(deviceLaptop); !ok {
		t.Error("device vanished after failed delete")
	}
}

func TestAdminActionBodies(t *testing.T) {
	h := newHarness(t, withAdminActions())
	tests := []struct {
		name     string
		path     string
		body     any
		method   string
		wantArgs any
	}{
		{"tags normalized", "/api/v1/devices/laptop/tags", map[string][]string{"tags": {" tag:Server ", "tag:server", "tag:db-1"}}, "SetTags", []string{"tag:server", "tag:db-1"}},
		{"tags empty list allowed", "/api/v1/devices/laptop/tags", map[string][]string{"tags": {}}, "SetTags", []string{}},
		{"routes canonical", "/api/v1/devices/laptop/routes", map[string][]string{"routes": {"10.0.0.5/24", "10.0.0.0/24", "fd00::1/64"}}, "SetRoutes", []string{"10.0.0.0/24", "fd00::/64"}},
		{"routes empty", "/api/v1/devices/laptop/routes", map[string][]string{"routes": {}}, "SetRoutes", []string{}},
		{"name lowercased", "/api/v1/devices/laptop/name", map[string]string{"name": " New-Name1 "}, "SetName", "new-name1"},
		{"key expiry", "/api/v1/devices/laptop/key-expiry", map[string]bool{"disabled": true}, "SetKeyExpiryDisabled", true},
		{"authorize false", "/api/v1/devices/laptop/authorize", map[string]bool{"authorized": false}, "SetAuthorized", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.api.mu.Lock()
			h.api.calls = nil
			h.api.mu.Unlock()
			w := h.do("POST", tc.path, tc.body, ipAdmin)
			expect(t, w, 200, "")
			calls := h.api.recorded()
			if len(calls) != 1 || calls[0].method != tc.method || calls[0].id != deviceLaptop || !reflect.DeepEqual(calls[0].args, tc.wantArgs) {
				t.Errorf("calls = %+v, want %s(%v)", calls, tc.method, tc.wantArgs)
			}
		})
	}
}

func TestDeleteDevice(t *testing.T) {
	h := newHarness(t, withAdminActions())
	w := h.do("DELETE", "/api/v1/devices/nas", nil, ipAdmin)
	expect(t, w, 204, "")
	if w.Body.Len() != 0 {
		t.Errorf("204 with body %q", w.Body.String())
	}
	calls := h.api.recorded()
	if len(calls) != 1 || calls[0].method != "DeleteDevice" || calls[0].id != deviceNAS {
		t.Errorf("calls = %+v", calls)
	}
	if _, ok := h.col.Device(deviceNAS); ok {
		t.Error("deleted device still in snapshot")
	}
	entries := h.auditEntries()
	if len(entries) != 1 || entries[0].Action != "device.delete" || !entries[0].OK || entries[0].Target != deviceNAS {
		t.Errorf("audit = %+v", entries)
	}
	// Second delete: unknown now.
	expect(t, h.do("DELETE", "/api/v1/devices/nas", nil, ipAdmin), 404, codeNotFound)
}

func TestValidators(t *testing.T) {
	if _, err := validateTags(make([]string, maxTags+1)); err == nil {
		t.Error("too many tags accepted")
	}
	if _, err := validateRoutes(make([]string, maxRoutes+1)); err == nil {
		t.Error("too many routes accepted")
	}
	names := []struct {
		in string
		ok bool
	}{
		{"a", true}, {"a-b", true}, {"abc123", true}, {strings.Repeat("a", 63), true},
		{"-a", false}, {"a-", false}, {"a_b", false}, {"", false}, {"A B", false}, {strings.Repeat("a", 64), false},
	}
	for _, n := range names {
		_, err := validateName(n.in)
		if (err == nil) != n.ok {
			t.Errorf("validateName(%q) err = %v, want ok=%v", n.in, err, n.ok)
		}
	}
}
