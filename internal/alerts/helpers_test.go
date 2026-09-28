package alerts

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/bus"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// base is the fixed "now" every test starts from.
var base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// longAgo is a FirstSeen value that never counts as "new".
var longAgo = base.Add(-30 * 24 * time.Hour)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// fakeSource is a Source backed by a bus that records emitted events.
type fakeSource struct {
	ticks  *bus.Bus[model.Snapshot]
	hub    model.HubInfo
	mu     sync.Mutex
	events []model.Event
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		ticks: bus.New[model.Snapshot](16),
		hub: model.HubInfo{
			Version:         "test",
			SelfName:        "hub",
			SelfID:          "hubid",
			PollIntervalSec: 15,
			StartedAt:       base,
		},
	}
}

func (f *fakeSource) Ticks() *bus.Bus[model.Snapshot] { return f.ticks }

func (f *fakeSource) Emit(_ context.Context, e model.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeSource) Hub() model.HubInfo { return f.hub }

// eventsOfType returns the recorded events of type t.
func (f *fakeSource) eventsOfType(t model.EventType) []model.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Event
	for _, e := range f.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// fakeNotifier records notifications and optionally fails.
type fakeNotifier struct {
	name string
	err  error
	mu   sync.Mutex
	got  []Notification
}

func (n *fakeNotifier) Name() string { return n.name }

func (n *fakeNotifier) Send(_ context.Context, msg Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, msg)
	return n.err
}

func (n *fakeNotifier) sent() []Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Notification(nil), n.got...)
}

// harness wires an engine to an in-memory store, a fake source, a fake
// notifier and a fake clock.
type harness struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	src *fakeSource
	nf  *fakeNotifier
	clk *fakeClock
	e   *Engine
}

// newStore opens an in-memory store closed at test end.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:", testLogger())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newHarness builds a harness on a fresh store.
func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessOn(t, newStore(t), &fakeClock{t: base})
}

// newHarnessOn builds a harness on an existing store and clock (restarts).
func newHarnessOn(t *testing.T, st *store.Store, clk *fakeClock) *harness {
	t.Helper()
	src := newFakeSource()
	nf := &fakeNotifier{name: "fake"}
	e := newEngine(st, src, []Notifier{nf}, testLogger(), clk.Now)
	if !e.loaded {
		t.Fatalf("engine did not load its state")
	}
	return &harness{t: t, ctx: context.Background(), st: st, src: src, nf: nf, clk: clk, e: e}
}

// tick evaluates one snapshot containing devs and delivers queued
// notifications synchronously.
func (h *harness) tick(devs ...model.Device) {
	h.t.Helper()
	h.e.evaluate(h.ctx, model.Snapshot{Overview: model.Overview{Hub: h.src.hub}, Devices: devs})
	h.flush()
}

// flush delivers every queued notification on the calling goroutine.
func (h *harness) flush() {
	for {
		select {
		case n := <-h.e.queue:
			h.e.deliver(h.ctx, n)
		default:
			return
		}
	}
}

// setRule tweaks one stored rule through SaveRule.
func (h *harness) setRule(id string, fn func(r *model.AlertRule)) model.AlertRule {
	h.t.Helper()
	var cur model.AlertRule
	found := false
	for _, r := range h.e.Rules() {
		if r.ID == id {
			cur, found = r, true
		}
	}
	if !found {
		h.t.Fatalf("rule %q not found", id)
	}
	fn(&cur)
	saved, err := h.e.SaveRule(h.ctx, cur)
	if err != nil {
		h.t.Fatalf("SaveRule(%s): %v", id, err)
	}
	return saved
}

// openAlerts lists the open alerts in the store.
func (h *harness) openAlerts() []model.Alert {
	h.t.Helper()
	out, err := h.st.ListAlerts(h.ctx, model.AlertQuery{State: model.AlertOpen})
	if err != nil {
		h.t.Fatalf("ListAlerts: %v", err)
	}
	return out
}

// allAlerts lists every alert in the store.
func (h *harness) allAlerts() []model.Alert {
	h.t.Helper()
	out, err := h.st.ListAlerts(h.ctx, model.AlertQuery{})
	if err != nil {
		h.t.Fatalf("ListAlerts: %v", err)
	}
	return out
}

// onlyOpen asserts exactly one open alert and returns it.
func (h *harness) onlyOpen() model.Alert {
	h.t.Helper()
	open := h.openAlerts()
	if len(open) != 1 {
		h.t.Fatalf("open alerts = %d, want 1: %+v", len(open), open)
	}
	return open[0]
}

// dev builds an online/offline device known for a long time.
func dev(id, name string, online bool) model.Device {
	d := model.Device{
		ID:         model.DeviceID(id),
		Name:       name,
		DNSName:    name + ".example.ts.net",
		Hostname:   name,
		OS:         "linux",
		User:       "alice@example.com",
		Addresses:  []string{"100.64.0.10"},
		Tags:       []string{},
		Online:     online,
		Authorized: true,
		FirstSeen:  longAgo,
		Created:    longAgo,
		LastSeen:   base.Add(-7 * time.Minute),
		Agent:      model.AgentStatus{State: model.AgentUnknown},
	}
	if online {
		d.LastSeen = base
		d.Connectivity.Path = model.PathDirect
	} else {
		d.Connectivity.Path = model.PathNone
	}
	return d
}

// withMetrics attaches reachable-agent metrics sampled at now.
func withMetrics(d model.Device, now time.Time, m model.MetricsSnapshot) model.Device {
	m.SampledAt = now
	if m.CPUCount == 0 {
		m.CPUCount = 4
	}
	d.Metrics = &m
	t := now
	d.Agent = model.AgentStatus{State: model.AgentReachable, Version: "1.0.0", LastSuccess: &t}
	return d
}

// waitFor polls cond for up to three seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func fp(v float64) *float64 { return &v }

func tp(t time.Time) *time.Time { return &t }
