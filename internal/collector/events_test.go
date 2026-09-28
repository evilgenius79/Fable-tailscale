package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestEventsNewOnlineOfflinePath(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", relay("nyc")),
		peer("b", "bravo", "100.64.0.3", offline()),
	))
	h.poll()
	evs := h.drainEvents()
	if countType(evs, model.EventDeviceNew) != 3 || len(evs) != 3 {
		t.Fatalf("first poll events: %v", eventTypes(evs))
	}
	if e, _ := findEvent(evs, model.EventDeviceNew, "a"); e.Device != "alpha" || e.Severity != model.SeverityInfo || e.ID == 0 {
		t.Errorf("device.new event: %+v", e)
	}
	stored, err := h.st.ListEvents(t.Context(), model.EventQuery{})
	if err != nil || len(stored) != 3 {
		t.Fatalf("events persisted: %d (%v)", len(stored), err)
	}

	// Transitions.
	h.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", offline()),
		peer("b", "bravo", "100.64.0.3", relay("fra")),
	))
	h.pollAfter(15 * time.Second)
	evs = h.drainEvents()
	if len(evs) != 2 {
		t.Fatalf("transition events: %v", eventTypes(evs))
	}
	off, ok := findEvent(evs, model.EventDeviceOffline, "a")
	if !ok || off.Severity != model.SeverityWarning {
		t.Errorf("device.offline: %+v", off)
	}
	on, ok := findEvent(evs, model.EventDeviceOnline, "b")
	if !ok || on.Severity != model.SeverityInfo || !strings.Contains(on.Message, "relay (fra)") {
		t.Errorf("device.online: %+v", on)
	}
	if d := h.device("a"); d.Uptime.LastChange == nil || !d.Uptime.LastChange.Equal(h.now) || d.Uptime.OnlineFor != nil {
		t.Errorf("uptime after going offline: %+v", d.Uptime)
	}

	// relay → direct is a path change; direct → unknown and unknown → relay are not.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline()), peer("b", "bravo", "100.64.0.3", curAddr("1.1.1.1:1"), relay("fra"))))
	h.pollAfter(15 * time.Second)
	evs = h.drainEvents()
	pc, ok := findEvent(evs, model.EventPathChanged, "b")
	if !ok || len(evs) != 1 || !strings.Contains(pc.Message, "relay (fra) to direct") {
		t.Errorf("path_changed: %v %+v", eventTypes(evs), pc)
	}
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline()), peer("b", "bravo", "100.64.0.3")))
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("direct → unknown must not emit: %v", eventTypes(evs))
	}
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", offline()), peer("b", "bravo", "100.64.0.3", relay("nyc"))))
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("unknown → relay must not emit: %v", eventTypes(evs))
	}
	// Uptime.LastChange sticks while the state is unchanged; OnlineFor grows.
	d := h.device("b")
	if d.Uptime.LastChange == nil || !d.Uptime.LastChange.Equal(baseTime.Add(15*time.Second)) || d.Uptime.OnlineFor == nil || *d.Uptime.OnlineFor != 45 {
		t.Errorf("uptime bookkeeping: %+v", d.Uptime)
	}
}

func TestEventsUpdatedExpiredAuthorized(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.api.configured = true
	// A device awaiting authorization is only known to the control API; it
	// enters the netmap once an admin approves it.
	h.api.setDevices(source.APIDevice{NodeID: "a", Name: "alpha.tail.ts.net", Hostname: "alpha", OS: "linux",
		ClientVersion: "1.80.0", Authorized: false, Tags: []string{"tag:one"}, LastSeen: baseTime})
	h.poll()
	h.drainEvents()
	if d := h.device("a"); d.Authorized {
		t.Fatalf("api-only pending device must be unauthorized")
	}

	h.api.setDevices(source.APIDevice{NodeID: "a", Name: "alpha.tail.ts.net", ClientVersion: "1.82.0", Authorized: true})
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", tags("tag:one", "tag:two"), hostname("alpha-2"), osName("macOS"), expired())))
	h.pollAfter(60 * time.Second)
	evs := h.drainEvents()
	types := eventTypes(evs)
	if countType(evs, model.EventDeviceUpdated) != 1 || countType(evs, model.EventDeviceExpired) != 1 || countType(evs, model.EventDeviceAuthorized) != 1 || len(evs) != 3 {
		t.Fatalf("events: %v", types)
	}
	up, _ := findEvent(evs, model.EventDeviceUpdated, "a")
	for _, want := range []string{"client version 1.80.0 → 1.82.0", "hostname alpha → alpha-2", "OS linux → macOS", "tags [tag:one] → [tag:one, tag:two]"} {
		if !strings.Contains(up.Message, want) {
			t.Errorf("device.updated message %q lacks %q", up.Message, want)
		}
	}
	if up.Data["clientVersionTo"] != "1.82.0" {
		t.Errorf("device.updated data: %v", up.Data)
	}
	if ex, _ := findEvent(evs, model.EventDeviceExpired, "a"); ex.Severity != model.SeverityWarning {
		t.Errorf("device.expired severity: %v", ex.Severity)
	}

	// Same state again → nothing; version disappearing (API gone) → nothing.
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("steady state emitted: %v", eventTypes(evs))
	}
	h.api.setDevices()
	h.pollAfter(60 * time.Second)
	if evs := h.drainEvents(); countType(evs, model.EventDeviceUpdated) != 0 {
		t.Errorf("version vanishing must not be an update: %v", eventTypes(evs))
	}
}

func TestEventsRemovedAndReappear(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"), bytes(10, 10))))
	h.poll()
	h.drainEvents()

	h.local.setStatus(hubStatus())
	for i := 1; i <= 2; i++ {
		h.pollAfter(15 * time.Second)
		if evs := h.drainEvents(); len(evs) != 0 {
			t.Fatalf("poll %d absent: unexpected events %v", i, eventTypes(evs))
		}
		if d := h.device("a"); !d.Online || d.Connectivity.Path != model.PathDirect || d.Connectivity.RxRate != 0 {
			t.Fatalf("poll %d absent: device should be carried forward unchanged: %+v", i, d.Connectivity)
		}
	}
	h.pollAfter(15 * time.Second)
	evs := h.drainEvents()
	if countType(evs, model.EventDeviceRemoved) != 1 || len(evs) != 1 {
		t.Fatalf("third absent poll: %v", eventTypes(evs))
	}
	d := h.device("a")
	if d.Online || d.Connectivity.Path != model.PathNone || d.Metrics != nil || d.Uptime.LastChange == nil || !d.Uptime.LastChange.Equal(h.now) {
		t.Errorf("removed device: online=%v path=%v uptime=%+v", d.Online, d.Connectivity.Path, d.Uptime)
	}
	if stored, err := h.st.GetDevice(t.Context(), "a"); err != nil || stored.Online {
		t.Errorf("row must be kept and offline: %+v (%v)", stored, err)
	}
	h.pollAfter(15 * time.Second)
	if evs := h.drainEvents(); len(evs) != 0 {
		t.Errorf("removed device must not re-emit: %v", eventTypes(evs))
	}

	// Reappearing: device.online, never device.new again.
	h.local.setStatus(hubStatus(peer("a", "alpha", "100.64.0.2", curAddr("1.1.1.1:1"))))
	h.pollAfter(15 * time.Second)
	evs = h.drainEvents()
	if countType(evs, model.EventDeviceOnline) != 1 || countType(evs, model.EventDeviceNew) != 0 {
		t.Errorf("reappearance events: %v", eventTypes(evs))
	}
	if d := h.device("a"); !d.Online || d.Connectivity.Path != model.PathDirect {
		t.Errorf("reappeared device: %+v", d.Connectivity)
	}
}

func TestNoEventFloodOnRestart(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", relay("nyc")),
		peer("b", "bravo", "100.64.0.3"),
		peer("c", "charlie", "100.64.0.4", offline()),
	))
	h.agents.setReport("100.64.0.2", report(baseTime, 1, 1))
	h.poll()
	h.drainEvents()
	firstChange := h.device("a").Uptime.LastChange

	// A second collector over the same store, as after a restart.
	h2 := newHarnessWithStore(t, defaultCfg(), h.st)
	h2.now = baseTime.Add(time.Hour)
	h2.local.setStatus(hubStatus(
		peer("a", "alpha", "100.64.0.2", relay("nyc")),
		peer("b", "bravo", "100.64.0.3", offline()),
		peer("d", "delta", "100.64.0.5"),
	))
	h2.poll()
	evs := h2.drainEvents()
	types := eventTypes(evs)
	if countType(evs, model.EventDeviceNew) != 1 || countType(evs, model.EventDeviceOffline) != 1 ||
		countType(evs, model.EventAgentUnreachable) != 1 || len(evs) != 3 {
		t.Fatalf("restart events: %v", types)
	}
	if e, _ := findEvent(evs, model.EventDeviceNew, ""); e.DeviceID != "d" {
		t.Errorf("device.new for wrong device: %+v", e)
	}
	if e, _ := findEvent(evs, model.EventDeviceOffline, ""); e.DeviceID != "b" {
		t.Errorf("device.offline for wrong device: %+v", e)
	}
	// Device c vanished while the hub was down: kept, offline, no event.
	if d := h2.device("c"); d.Online || d.Connectivity.Path != model.PathNone {
		t.Errorf("stored device absent at restart: %+v", d.Connectivity)
	}
	// FirstSeen and LastChange persist across restarts.
	a := h2.device("a")
	if !a.FirstSeen.Equal(baseTime) || a.Uptime.LastChange == nil || !a.Uptime.LastChange.Equal(*firstChange) {
		t.Errorf("persisted timestamps: firstSeen=%v lastChange=%v", a.FirstSeen, a.Uptime.LastChange)
	}
	if a.Uptime.OnlineFor == nil || *a.Uptime.OnlineFor != 3600 {
		t.Errorf("OnlineFor across restart: %v", a.Uptime.OnlineFor)
	}
}

func TestPathChangedEventHelper(t *testing.T) {
	cases := []struct {
		from, to model.PathType
		want     bool
	}{
		{model.PathDirect, model.PathRelay, true},
		{model.PathRelay, model.PathDirect, true},
		{model.PathDirect, model.PathDirect, false},
		{model.PathDirect, model.PathUnknown, false},
		{model.PathUnknown, model.PathRelay, false},
		{model.PathNone, model.PathDirect, false},
		{model.PathRelay, model.PathNone, false},
	}
	for _, tc := range cases {
		if got := isPathTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("isPathTransition(%s,%s)=%v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
