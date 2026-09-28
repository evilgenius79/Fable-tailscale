package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// baseTime is the fake clock's starting point.
var baseTime = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// --- fake LocalSource ------------------------------------------------------

type fakeLocal struct {
	mu        sync.Mutex
	status    *source.LocalStatus
	statusErr error
	pings     map[string]source.PingReply
	pingErrs  map[string]error
	pingCalls []string
	inFlight  int
	maxFlight int
}

func (f *fakeLocal) Status(context.Context) (*source.LocalStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.status == nil {
		return &source.LocalStatus{}, nil
	}
	cp := *f.status
	cp.Peers = slices.Clone(f.status.Peers)
	return &cp, nil
}

func (f *fakeLocal) WhoIs(context.Context, string) (*source.WhoIs, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLocal) Ping(ctx context.Context, ip string, _ time.Duration) (*source.PingReply, error) {
	f.mu.Lock()
	f.pingCalls = append(f.pingCalls, ip)
	f.inFlight++
	f.maxFlight = max(f.maxFlight, f.inFlight)
	err := f.pingErrs[ip]
	reply, ok := f.pings[ip]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if err != nil {
		return nil, err
	}
	if !ok {
		return &source.PingReply{Err: "no route to " + ip}, nil
	}
	return &reply, nil
}

func (f *fakeLocal) setStatus(st *source.LocalStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = st
	f.statusErr = nil
}

func (f *fakeLocal) setStatusErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusErr = err
}

func (f *fakeLocal) setPing(ip string, r source.PingReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pings == nil {
		f.pings = map[string]source.PingReply{}
	}
	f.pings[ip] = r
}

func (f *fakeLocal) setPingErr(ip string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pingErrs == nil {
		f.pingErrs = map[string]error{}
	}
	f.pingErrs[ip] = err
}

func (f *fakeLocal) pingsTo(ip string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.pingCalls {
		if p == ip {
			n++
		}
	}
	return n
}

func (f *fakeLocal) resetPings() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingCalls = nil
}

// --- fake ControlAPI -------------------------------------------------------

type fakeAPI struct {
	mu         sync.Mutex
	configured bool
	tailnet    string
	devices    []source.APIDevice
	err        error
	calls      int
}

func (f *fakeAPI) Configured() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.configured }
func (f *fakeAPI) Tailnet() string  { f.mu.Lock(); defer f.mu.Unlock(); return f.tailnet }
func (f *fakeAPI) Devices(context.Context) ([]source.APIDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.devices), nil
}
func (f *fakeAPI) SetAuthorized(context.Context, string, bool) error        { return nil }
func (f *fakeAPI) SetTags(context.Context, string, []string) error          { return nil }
func (f *fakeAPI) SetKeyExpiryDisabled(context.Context, string, bool) error { return nil }
func (f *fakeAPI) SetRoutes(context.Context, string, []string) error        { return nil }
func (f *fakeAPI) DeleteDevice(context.Context, string) error               { return nil }
func (f *fakeAPI) SetName(context.Context, string, string) error            { return nil }

func (f *fakeAPI) setDevices(devs ...source.APIDevice) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = devs
	f.err = nil
}

func (f *fakeAPI) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeAPI) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// --- fake AgentClient ------------------------------------------------------

type fakeAgents struct {
	mu        sync.Mutex
	reports   map[string]*agentproto.Report
	errs      map[string]error
	calls     map[string]int
	inFlight  int
	maxFlight int
	delay     time.Duration
}

func (f *fakeAgents) Fetch(ctx context.Context, ip string, _ int) (*agentproto.Report, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[ip]++
	f.inFlight++
	f.maxFlight = max(f.maxFlight, f.inFlight)
	err := f.errs[ip]
	rep := f.reports[ip]
	delay := f.delay
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return nil, errors.New("dial tcp " + ip + ": connection refused")
	}
	return cloneReport(rep), nil
}

func (f *fakeAgents) setReport(ip string, r *agentproto.Report) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reports == nil {
		f.reports = map[string]*agentproto.Report{}
	}
	f.reports[ip] = r
	delete(f.errs, ip)
}

func (f *fakeAgents) setErr(ip string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errs == nil {
		f.errs = map[string]error{}
	}
	f.errs[ip] = err
}

func (f *fakeAgents) callCount(ip string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[ip] }

// --- fixtures --------------------------------------------------------------

type peerOpt func(*source.LocalPeer)

func offline() peerOpt         { return func(p *source.LocalPeer) { p.Online = false } }
func curAddr(a string) peerOpt { return func(p *source.LocalPeer) { p.CurAddr = a } }
func relay(r string) peerOpt   { return func(p *source.LocalPeer) { p.Relay = r } }
func bytes(rx, tx int64) peerOpt {
	return func(p *source.LocalPeer) { p.RxBytes, p.TxBytes = rx, tx }
}
func tags(t ...string) peerOpt {
	return func(p *source.LocalPeer) { p.Tags = t; p.UserLogin = ""; p.UserDisplay = "" }
}
func osName(s string) peerOpt           { return func(p *source.LocalPeer) { p.OS = s } }
func hostname(s string) peerOpt         { return func(p *source.LocalPeer) { p.HostName = s } }
func user(login string) peerOpt         { return func(p *source.LocalPeer) { p.UserLogin = login } }
func primaryRoutes(r ...string) peerOpt { return func(p *source.LocalPeer) { p.PrimaryRoutes = r } }
func exitNodeOpt() peerOpt              { return func(p *source.LocalPeer) { p.ExitNodeOpt = true } }
func expired() peerOpt                  { return func(p *source.LocalPeer) { p.Expired = true } }
func keyExpiry(t time.Time) peerOpt     { return func(p *source.LocalPeer) { p.KeyExpiry = &t } }
func ips(addrs ...string) peerOpt       { return func(p *source.LocalPeer) { p.TailscaleIPs = addrs } }

// peer builds an online linux peer owned by alice.
func peer(id, name, ip string, opts ...peerOpt) source.LocalPeer {
	p := source.LocalPeer{
		ID:           model.DeviceID(id),
		HostName:     name,
		DNSName:      name + ".tail.ts.net",
		OS:           "linux",
		UserLogin:    "alice@example.com",
		UserDisplay:  "Alice",
		TailscaleIPs: []string{ip},
		Online:       true,
		Created:      baseTime.Add(-30 * 24 * time.Hour),
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

// hubStatus builds a running local status with the given peers.
func hubStatus(peers ...source.LocalPeer) *source.LocalStatus {
	return &source.LocalStatus{
		Version:        "1.86.0",
		BackendState:   "Running",
		TailscaleIPs:   []string{"100.64.0.1", "fd7a:115c:a1e0::1"},
		Self:           peer("self", "hub", "100.64.0.1", ips("100.64.0.1", "fd7a:115c:a1e0::1")),
		Health:         []string{},
		TailnetName:    "example.com",
		MagicDNSSuffix: "tail.ts.net",
		Peers:          peers,
	}
}

// report builds an agent report with one physical and one virtual interface.
func report(sampledAt time.Time, ethRx, ethTx uint64) *agentproto.Report {
	return &agentproto.Report{
		ProtocolVersion: agentproto.ProtocolVersion,
		AgentVersion:    "0.3.0",
		SampledAt:       sampledAt,
		Host: agentproto.Host{
			Hostname: "laptop", OS: "linux", Platform: "ubuntu", PlatformVersion: "24.04",
			Kernel: "6.8", Arch: "amd64", BootTime: baseTime.Add(-48 * time.Hour), UptimeSeconds: 172800,
		},
		CPU:    agentproto.CPU{Percent: 37.5, PerCore: []float64{30, 45}, Count: 2, Model: "test cpu", Load1: 0.7, Load5: 0.5, Load15: 0.4},
		Memory: agentproto.Memory{Total: 8 << 30, Used: 4 << 30, Available: 4 << 30, Percent: 50, SwapTotal: 1 << 30, SwapUsed: 0},
		Disks: []agentproto.Disk{
			{Mount: "/data", FSType: "ext4", Total: 1000, Used: 800, Percent: 80},
			{Mount: "/", FSType: "ext4", Total: 1000, Used: 420, Percent: 42, Primary: true},
		},
		Net: agentproto.Net{
			Interfaces: []agentproto.Interface{
				{Name: "eth0", RxBytes: ethRx, TxBytes: ethTx, RxPackets: 10, TxPackets: 20, Physical: true},
				{Name: "tailscale0", RxBytes: ethRx * 10, TxBytes: ethTx * 10, Physical: false},
			},
			TailscaleIf: "tailscale0",
		},
		Temperatures: []agentproto.Temperature{{Sensor: "cpu", Celsius: 40}, {Sensor: "nvme", Celsius: 65, Critical: 90}},
		Processes:    123,
		Tailscale:    &agentproto.TailscaleInfo{Version: "1.86.0", BackendState: "Running", IPs: []string{"100.64.0.2"}},
	}
}

// --- harness ---------------------------------------------------------------

type harness struct {
	t      *testing.T
	c      *Collector
	st     *store.Store
	local  *fakeLocal
	api    *fakeAPI
	agents *fakeAgents
	now    time.Time
	events <-chan model.Event
	ticks  <-chan model.Snapshot
}

// defaultCfg is a poll every 15s, API every 60s, pings disabled, agents on.
func defaultCfg() Config {
	return Config{
		PollInterval: 15 * time.Second,
		APIInterval:  60 * time.Second,
		AgentEnabled: true,
		AgentTimeout: time.Second,
		Version:      "test",
	}
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:", quietLogger())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return newHarnessWithStore(t, cfg, st)
}

func newHarnessWithStore(t *testing.T, cfg Config, st *store.Store) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		st:     st,
		local:  &fakeLocal{status: hubStatus()},
		api:    &fakeAPI{tailnet: "example.com"},
		agents: &fakeAgents{},
		now:    baseTime,
	}
	h.c = New(cfg, st, h.local, h.api, h.agents, quietLogger())
	h.c.now = func() time.Time { return h.now }
	h.c.jitter = func() float64 { return 0.5 } // factor exactly 1.0
	h.c.startedAt = baseTime
	events, cancelEvents := h.c.Events().Subscribe()
	ticks, cancelTicks := h.c.Ticks().Subscribe()
	t.Cleanup(cancelEvents)
	t.Cleanup(cancelTicks)
	h.events = events
	h.ticks = ticks
	return h
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (h *harness) advance(d time.Duration) { h.now = h.now.Add(d) }

// poll runs PollOnce and fails the test on error.
func (h *harness) poll() {
	h.t.Helper()
	if err := h.c.PollOnce(context.Background()); err != nil {
		h.t.Fatalf("PollOnce: %v", err)
	}
}

// pollAfter advances the clock and polls.
func (h *harness) pollAfter(d time.Duration) {
	h.t.Helper()
	h.advance(d)
	h.poll()
}

// drainEvents returns every event published so far.
func (h *harness) drainEvents() []model.Event {
	var out []model.Event
	for {
		select {
		case e := <-h.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

// drainTicks returns every snapshot published so far.
func (h *harness) drainTicks() []model.Snapshot {
	var out []model.Snapshot
	for {
		select {
		case s := <-h.ticks:
			out = append(out, s)
		default:
			return out
		}
	}
}

// device fetches a device by ID and fails the test when missing.
func (h *harness) device(id string) model.Device {
	h.t.Helper()
	d, ok := h.c.Device(model.DeviceID(id))
	if !ok {
		h.t.Fatalf("device %q not in snapshot", id)
	}
	return d
}

// eventTypes lists the types of the given events in order.
func eventTypes(evs []model.Event) []model.EventType {
	out := make([]model.EventType, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// countType counts events of one type.
func countType(evs []model.Event, typ model.EventType) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// findEvent returns the first event of the given type for a device.
func findEvent(evs []model.Event, typ model.EventType, id string) (model.Event, bool) {
	for _, e := range evs {
		if e.Type == typ && (id == "" || string(e.DeviceID) == id) {
			return e, true
		}
	}
	return model.Event{}, false
}

func floatVal(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
