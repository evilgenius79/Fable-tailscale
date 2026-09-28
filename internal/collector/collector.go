// Package collector is the heart of the Tailwatch hub: it periodically polls
// the local tailscaled (source.LocalSource), optionally the Tailscale control
// API (source.ControlAPI) and the per-device tailwatch-agents
// (source.AgentClient), merges everything into one model.Snapshot, derives
// rates and uptime, emits events on state transitions, persists devices and
// samples to the store and publishes the snapshot on a bus for the alert
// engine and SSE clients.
//
// The poll itself is exposed as PollOnce so tests (and callers that need a
// synchronous refresh) can drive it deterministically; Run is a thin loop
// around it that adds the poll ticker, RefreshNow coalescing and the hourly
// rollup/prune maintenance task.
//
// Concurrency: the published snapshot and hub info are guarded by an RWMutex
// that is never held during network or database calls. Polls are serialized
// by a separate mutex, so PollOnce, Run and RefreshNow never overlap.
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/bus"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// Config tunes the collector. Zero values fall back to the defaults listed
// per field; PingInterval == 0 disables disco pings.
type Config struct {
	// PollInterval is the period of the main poll loop (default 15s).
	PollInterval time.Duration
	// APIInterval is how often the control API is queried (default 60s).
	APIInterval time.Duration
	// PingInterval is how often online peers are disco-pinged; 0 disables
	// pinging entirely.
	PingInterval time.Duration
	// AgentTimeout bounds one agent fetch (default 5s).
	AgentTimeout time.Duration
	// PingConcurrency is the maximum number of concurrent pings (default 8).
	PingConcurrency int
	// AgentConcurrency is the maximum number of concurrent agent fetches
	// (default 8).
	AgentConcurrency int
	// AgentPort is the TCP port tailwatch-agent listens on (default
	// agentproto.DefaultPort).
	AgentPort int
	// AgentEnabled turns agent collection on; when false every device
	// reports AgentDisabled and the AgentClient is never used.
	AgentEnabled bool
	// RawRetention, RollupRetention and EventRetention are passed to
	// store.Prune during hourly maintenance (<= 0 disables that category).
	RawRetention    time.Duration
	RollupRetention time.Duration
	EventRetention  time.Duration
	// Version is reported in HubInfo.Version.
	Version string
	// DemoMode and AdminActions are reported in HubInfo as-is.
	DemoMode     bool
	AdminActions bool
}

const (
	defaultPollInterval     = 15 * time.Second
	defaultAPIInterval      = time.Minute
	defaultAgentTimeout     = 5 * time.Second
	defaultConcurrency      = 8
	pingTimeout             = 5 * time.Second
	pingNowTimeout          = 10 * time.Second
	hubErrorRepeatInterval  = 10 * time.Minute
	apiOnlineWindow         = 5 * time.Minute
	keyExpiringWindow       = 7 * 24 * time.Hour
	removedAfterPolls       = 3
	ratiosRefreshInterval   = 5 * time.Minute
	maintenanceInterval     = time.Hour
	rollupLag               = time.Hour
	sparklineWindow         = 60 * time.Minute
	sparklineStep           = time.Minute
	agentBackoffMin         = 30 * time.Second
	agentBackoffMax         = 10 * time.Minute
	agentBackoffJitter      = 0.30 // +/- 15 percent around the nominal delay
	maxErrorTextLen         = 300
	ticksBufferSize         = 8
	eventsBufferSize        = 256
	scheduleToleranceFactor = 10 // an interval is "due" within PollInterval/10 of its nominal time
)

// withDefaults returns cfg with zero values replaced by defaults.
func (cfg Config) withDefaults() Config {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.APIInterval <= 0 {
		cfg.APIInterval = defaultAPIInterval
	}
	if cfg.PingInterval < 0 {
		cfg.PingInterval = 0
	}
	if cfg.AgentTimeout <= 0 {
		cfg.AgentTimeout = defaultAgentTimeout
	}
	if cfg.PingConcurrency <= 0 {
		cfg.PingConcurrency = defaultConcurrency
	}
	if cfg.AgentConcurrency <= 0 {
		cfg.AgentConcurrency = defaultConcurrency
	}
	if cfg.AgentPort <= 0 {
		cfg.AgentPort = agentproto.DefaultPort
	}
	return cfg
}

// devState is per-device bookkeeping that lives between polls but is not
// part of model.Device. It is only touched by the poll goroutine.
type devState struct {
	// missing counts consecutive polls in which the device was absent from
	// every source. At removedAfterPolls the device is reported removed.
	missing int
	// agent holds the backoff schedule and the previous report used for
	// counter deltas.
	agent agentState
}

// agentState is the per-device agent fetch schedule.
type agentState struct {
	failures int
	nextTry  time.Time
	prev     *agentproto.Report
}

// uptimeRatios caches store.UptimeRatios for the three reporting windows.
type uptimeRatios struct {
	d24h, d7d, d30d map[model.DeviceID]float64
	at              time.Time
}

// Collector polls all sources and maintains the in-memory snapshot.
type Collector struct {
	cfg    Config
	st     *store.Store
	local  source.LocalSource
	api    source.ControlAPI
	agents source.AgentClient
	log    *slog.Logger

	// now and jitter are injectable for deterministic tests.
	now    func() time.Time
	jitter func() float64

	startedAt time.Time
	ticks     *bus.Bus[model.Snapshot]
	events    *bus.Bus[model.Event]
	refresh   chan struct{}

	// mu guards snap, hub and forgotten. It is never held during network or
	// store calls.
	mu        sync.RWMutex
	snap      model.Snapshot
	hub       model.HubInfo
	forgotten map[model.DeviceID]bool // devices dropped by Forget since the last poll consumed them

	// pollMu serializes polls; everything below it is poll-goroutine state.
	pollMu          sync.Mutex
	loaded          bool // stored devices loaded into the snapshot
	synced          bool // at least one successful Status() merge done
	state           map[model.DeviceID]*devState
	lastPoll        time.Time // last successful Status() merge; rate baseline
	lastAPIAttempt  time.Time
	lastAPIPoll     time.Time // last successful control API refresh
	lastPingAttempt time.Time
	apiDevices      []source.APIDevice
	hubErrors       map[string]time.Time
	ratios          uptimeRatios
}

// New creates a collector. st and local are required; api may be nil or
// unconfigured (then no control API data is merged) and agents may be nil
// when cfg.AgentEnabled is false. A nil log uses slog.Default().
func New(cfg Config, st *store.Store, local source.LocalSource, api source.ControlAPI, agents source.AgentClient, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	cfg = cfg.withDefaults()
	if cfg.AgentEnabled && agents == nil {
		log.Warn("collector: agent collection enabled but no agent client given; disabling")
		cfg.AgentEnabled = false
	}
	c := &Collector{
		cfg:       cfg,
		st:        st,
		local:     local,
		api:       api,
		agents:    agents,
		log:       log,
		now:       time.Now,
		jitter:    rand.Float64,
		ticks:     bus.New[model.Snapshot](ticksBufferSize),
		events:    bus.New[model.Event](eventsBufferSize),
		refresh:   make(chan struct{}, 1),
		state:     map[model.DeviceID]*devState{},
		hubErrors: map[string]time.Time{},
		forgotten: map[model.DeviceID]bool{},
	}
	c.startedAt = c.now()
	c.snap = model.Snapshot{Devices: []model.Device{}}
	c.hub = model.HubInfo{
		Version:         cfg.Version,
		StartedAt:       c.startedAt,
		DemoMode:        cfg.DemoMode,
		ControlAPI:      c.apiConfigured(),
		AdminActions:    cfg.AdminActions,
		AgentPort:       cfg.AgentPort,
		PollIntervalSec: int(cfg.PollInterval / time.Second),
		Health:          []string{},
		SelfIPs:         []string{},
	}
	c.snap.Overview = model.Overview{
		Hub:           c.hub,
		OSBreakdown:   map[string]int{},
		UserBreakdown: map[string]int{},
		Sparklines:    emptySparklines(),
	}
	return c
}

// apiConfigured reports whether a usable control API client is present.
func (c *Collector) apiConfigured() bool {
	return c.api != nil && c.api.Configured()
}

// Run performs one poll immediately, then polls every cfg.PollInterval (or
// sooner when RefreshNow is called). A maintenance goroutine runs
// store.Rollup and store.Prune once after the first poll and hourly
// thereafter. Run blocks until ctx is cancelled and returns nil once every
// goroutine has stopped.
func (c *Collector) Run(ctx context.Context) error {
	if err := c.PollOnce(ctx); err != nil && ctx.Err() == nil {
		c.log.Warn("collector: initial poll failed", "err", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.maintenanceLoop(ctx)
	}()

	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
		case <-c.refresh:
		}
		if err := c.PollOnce(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("collector: poll failed", "err", err)
		}
	}
}

// maintenanceLoop runs maintain immediately and then every hour until ctx
// is cancelled.
func (c *Collector) maintenanceLoop(ctx context.Context) {
	c.maintain(ctx)
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.maintain(ctx)
		}
	}
}

// maintain rolls raw samples older than one hour into 5-minute rollups and
// prunes old rows according to the retention configuration. Failures are
// logged, never fatal.
func (c *Collector) maintain(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := c.now()
	n, err := c.st.Rollup(ctx, now.Add(-rollupLag))
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("collector: rollup failed", "err", err)
		}
	} else if n > 0 {
		c.log.Info("collector: rollup complete", "buckets", n)
	}
	res, err := c.st.Prune(ctx, c.cfg.RawRetention, c.cfg.RollupRetention, c.cfg.EventRetention)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("collector: prune failed", "err", err)
		}
	} else if res.Total() > 0 {
		c.log.Info("collector: prune complete",
			"samples", res.Samples, "rollups", res.Rollups, "events", res.Events, "alerts", res.Alerts)
	}
}

// Ticks returns the bus on which a model.Snapshot is published after every
// poll (including degraded polls where tailscaled could not be reached).
func (c *Collector) Ticks() *bus.Bus[model.Snapshot] { return c.ticks }

// Events returns the bus on which every event is published after it has
// been persisted to the store.
func (c *Collector) Events() *bus.Bus[model.Event] { return c.events }

// RefreshNow requests an immediate poll from Run. It never blocks; requests
// that arrive while one is already pending are coalesced.
func (c *Collector) RefreshNow() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// Snapshot returns a copy of the latest in-memory snapshot. The devices
// slice and breakdown maps are fresh copies; nested slices and maps are
// shared but never mutated by the collector after publication.
func (c *Collector) Snapshot() model.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSnapshot(c.snap)
}

// cloneSnapshot copies the mutable top-level containers of a snapshot.
func cloneSnapshot(s model.Snapshot) model.Snapshot {
	out := s
	out.Devices = slices.Clone(s.Devices)
	if out.Devices == nil {
		out.Devices = []model.Device{}
	}
	out.Overview.OSBreakdown = maps.Clone(s.Overview.OSBreakdown)
	out.Overview.UserBreakdown = maps.Clone(s.Overview.UserBreakdown)
	out.Overview.Hub = cloneHub(s.Overview.Hub)
	return out
}

// cloneHub copies the slices inside a HubInfo.
func cloneHub(h model.HubInfo) model.HubInfo {
	h.Health = slices.Clone(h.Health)
	h.SelfIPs = slices.Clone(h.SelfIPs)
	if h.Health == nil {
		h.Health = []string{}
	}
	if h.SelfIPs == nil {
		h.SelfIPs = []string{}
	}
	if h.LastAPIPoll != nil {
		t := *h.LastAPIPoll
		h.LastAPIPoll = &t
	}
	return h
}

// Hub returns a copy of the current hub information.
func (c *Collector) Hub() model.HubInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneHub(c.hub)
}

// Device looks a device up by its stable ID first and then, case
// insensitively, by its MagicDNS base name or full DNS name.
func (c *Collector) Device(id model.DeviceID) (model.Device, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if i := c.indexOfLocked(id); i >= 0 {
		return c.snap.Devices[i], true
	}
	return model.Device{}, false
}

// indexOfLocked returns the index of the device with the given ID or name
// in the snapshot, or -1. c.mu must be held.
func (c *Collector) indexOfLocked(id model.DeviceID) int {
	for i := range c.snap.Devices {
		if c.snap.Devices[i].ID == id {
			return i
		}
	}
	name := strings.TrimSuffix(strings.TrimSpace(string(id)), ".")
	if name == "" {
		return -1
	}
	for i := range c.snap.Devices {
		d := &c.snap.Devices[i]
		if strings.EqualFold(d.Name, name) || strings.EqualFold(d.DNSName, name) {
			return i
		}
	}
	return -1
}

// Emit persists e to the store (setting its ID) and publishes it on
// Events(). A zero TS becomes now and an empty severity becomes info. Store
// failures are logged and the event is still published so live consumers
// see it.
func (c *Collector) Emit(ctx context.Context, e model.Event) {
	if e.TS.IsZero() {
		e.TS = c.now()
	}
	if e.Severity == "" {
		e.Severity = model.SeverityInfo
	}
	if err := c.st.InsertEvent(ctx, &e); err != nil {
		if ctx.Err() == nil {
			c.log.Warn("collector: persist event failed", "type", e.Type, "device", e.DeviceID, "err", err)
		}
	}
	c.events.Publish(e)
}

// PingNow sends an on-demand disco ping to the device's primary Tailscale IP
// with a 10 second timeout and updates the in-memory connectivity of the
// device on success. Failures of the ping itself (daemon-reported errors or
// a timeout) are returned in PingResult.Err with a nil error; an unknown
// device wraps source.ErrNotFound and a LocalAPI failure is returned as an
// error.
func (c *Collector) PingNow(ctx context.Context, id model.DeviceID) (*model.PingResult, error) {
	d, ok := c.Device(id)
	if !ok {
		return nil, fmt.Errorf("collector: device %q: %w", id, source.ErrNotFound)
	}
	now := c.now()
	ip := d.PrimaryIP()
	res := &model.PingResult{DeviceID: d.ID, IP: ip, Path: model.PathUnknown, At: now}
	switch {
	case d.IsSelf:
		res.Err = "cannot ping the hub itself"
		return res, nil
	case ip == "":
		res.Err = "device has no Tailscale IP"
		return res, nil
	}

	pctx, cancel := context.WithTimeout(ctx, pingNowTimeout)
	defer cancel()
	reply, err := c.local.Ping(pctx, ip, pingNowTimeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			res.Err = fmt.Sprintf("ping timed out after %s", pingNowTimeout)
			return res, nil
		}
		return nil, fmt.Errorf("collector: ping %s: %w", d.ID, err)
	}
	if reply == nil {
		return nil, fmt.Errorf("collector: ping %s: empty reply", d.ID)
	}
	if reply.Err != "" {
		res.Err = truncate(reply.Err, maxErrorTextLen)
		return res, nil
	}
	res.LatencyMs = reply.LatencyMs
	res.Endpoint = reply.Endpoint
	if p, relay, ok := pathFromPing(reply); ok {
		res.Path = p
		res.Relay = relay
	}

	var ev *model.Event
	c.mu.Lock()
	if i := c.indexOfLocked(d.ID); i >= 0 {
		dev := c.snap.Devices[i] // copy; published structs are never mutated in place
		lat := reply.LatencyMs
		dev.Connectivity.LatencyMs = &lat
		at := now
		dev.Connectivity.LastPing = &at
		if res.Path != model.PathUnknown && dev.Online {
			if e, ok := pathChangedEvent(now, &dev, res.Path, res.Relay); ok {
				ev = &e
			}
			dev.Connectivity.Path = res.Path
			dev.Connectivity.Relay = res.Relay
			if res.Path == model.PathDirect && res.Endpoint != "" {
				dev.Connectivity.CurAddr = res.Endpoint
			}
		}
		c.snap.Devices[i] = dev
	}
	c.mu.Unlock()
	if ev != nil {
		c.Emit(ctx, *ev)
	}
	return res, nil
}

// Forget drops a device from the in-memory snapshot and its poll-to-poll
// bookkeeping and deletes its stored row, samples and rollups (events and
// alerts are kept as history). It is meant to follow an administrative
// delete so the next poll does not resurrect the row; a device that a
// source still reports simply reappears on the next poll as device.new.
// An unknown device wraps source.ErrNotFound. Forget never blocks on an
// in-flight poll.
func (c *Collector) Forget(ctx context.Context, id model.DeviceID) error {
	d, ok := c.Device(id)
	if !ok {
		return fmt.Errorf("collector: device %q: %w", id, source.ErrNotFound)
	}
	if err := c.st.DeleteDevice(ctx, d.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("collector: forget %s: %w", d.ID, err)
	}
	c.mu.Lock()
	c.snap.Devices = slices.DeleteFunc(slices.Clone(c.snap.Devices), func(x model.Device) bool { return x.ID == d.ID })
	c.forgotten[d.ID] = true
	c.mu.Unlock()
	c.log.Info("collector: device forgotten", "device", d.ID, "name", d.Name)
	return nil
}

// takeForgotten returns and clears the set of devices forgotten since the
// last call.
func (c *Collector) takeForgotten() map[model.DeviceID]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.forgotten) == 0 {
		return nil
	}
	out := c.forgotten
	c.forgotten = map[model.DeviceID]bool{}
	return out
}

// Topology builds the network map from the current snapshot: one node per
// device and one edge from the hub to every other device carrying the hub's
// view of that link.
func (c *Collector) Topology() model.Topology {
	snap := c.Snapshot()
	hubID := snap.Overview.Hub.SelfID
	if hubID == "" {
		for i := range snap.Devices {
			if snap.Devices[i].IsSelf {
				hubID = snap.Devices[i].ID
				break
			}
		}
	}
	top := model.Topology{
		Nodes:       make([]model.TopologyNode, 0, len(snap.Devices)),
		Edges:       make([]model.TopologyEdge, 0, len(snap.Devices)),
		DERPRegions: map[string]string{},
	}
	for i := range snap.Devices {
		d := &snap.Devices[i]
		tags := slices.Clone(d.Tags)
		if tags == nil {
			tags = []string{}
		}
		top.Nodes = append(top.Nodes, model.TopologyNode{
			ID:        d.ID,
			Name:      d.Name,
			OS:        d.OS,
			Online:    d.Online,
			IsSelf:    d.IsSelf,
			IsExit:    d.ExitNodeOption,
			IsRouter:  isSubnetRouter(d),
			Tags:      tags,
			User:      d.User,
			Relay:     d.Connectivity.Relay,
			Path:      d.Connectivity.Path,
			LatencyMs: d.Connectivity.LatencyMs,
		})
		if d.Connectivity.Relay != "" {
			top.DERPRegions[d.Connectivity.Relay] = DERPRegionName(d.Connectivity.Relay)
		}
		if d.IsSelf || d.ID == hubID {
			continue
		}
		top.Edges = append(top.Edges, model.TopologyEdge{
			From:      hubID,
			To:        d.ID,
			Path:      d.Connectivity.Path,
			Relay:     d.Connectivity.Relay,
			LatencyMs: d.Connectivity.LatencyMs,
			RxRate:    d.Connectivity.RxRate,
			TxRate:    d.Connectivity.TxRate,
		})
	}
	return top
}

// truncate shortens s to at most n bytes (on a rune boundary) for use in
// event messages and status fields.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
