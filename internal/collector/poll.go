package collector

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

// pingOutcome is the result of one disco ping.
type pingOutcome struct {
	reply *source.PingReply
	err   error
}

// pingTarget is one peer selected for a ping.
type pingTarget struct {
	id model.DeviceID
	ip string
}

// polledDevice is a merged device plus what this poll learned about it.
type polledDevice struct {
	dev    model.Device
	pinged bool // a ping succeeded in this poll
	fresh  bool // a fresh agent report was applied in this poll
}

// PollOnce performs a single collection cycle: it reads tailscaled status,
// refreshes the control API cache when due, pings and fetches agents,
// merges everything into devices, derives events, persists devices and
// samples, and publishes the new snapshot on Ticks(). Polls are serialized.
//
// A tailscaled failure is not an error: the previous devices are republished
// with Path unknown, HubInfo.LastError is set and a hub.error event is
// emitted (at most once per distinct message per 10 minutes). PollOnce
// returns an error only when ctx is done or the store fails.
func (c *Collector) PollOnce(ctx context.Context) error {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.loadStored(ctx); err != nil {
		return err
	}
	now := c.now()
	c.refreshRatiosIfStale(ctx, now)

	prev := c.Snapshot().Devices
	prevByID := make(map[model.DeviceID]*model.Device, len(prev))
	for i := range prev {
		prevByID[prev[i].ID] = &prev[i]
	}

	status, err := c.local.Status(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.degradedPoll(ctx, now, prev, err)
	}
	if status == nil {
		status = &source.LocalStatus{}
	}

	var errs []string
	// A forced refresh (RefreshNow/Refresh after an admin action) must not
	// keep serving the cached API rows until APIInterval elapses.
	forceAPI := c.forceAPI.Swap(false)
	if c.apiConfigured() && (forceAPI || c.due(now, c.lastAPIAttempt, c.cfg.APIInterval)) {
		c.lastAPIAttempt = now
		devs, err := c.api.Devices(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			c.log.Warn("collector: control API poll failed", "err", err)
			errs = append(errs, "control API: "+truncate(err.Error(), maxErrorTextLen))
		default:
			c.apiDevices = devs
			c.lastAPIPoll = now
		}
	}

	entries := c.buildEntries(now, status)

	pingResults := map[model.DeviceID]pingOutcome{}
	if c.cfg.PingInterval > 0 && c.due(now, c.lastPingAttempt, c.cfg.PingInterval) {
		c.lastPingAttempt = now
		pingResults = c.pingAll(ctx, entries)
	}
	agentResults := map[model.DeviceID]agentOutcome{}
	if c.cfg.AgentEnabled {
		agentResults = c.fetchAgents(ctx, now, entries, prevByID)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	polled := make([]polledDevice, 0, len(entries)+len(prev))
	var events []model.Event
	seen := make(map[model.DeviceID]bool, len(entries))
	removed := make(map[model.DeviceID]bool)
	var self *model.Device
	for i := range entries {
		e := &entries[i]
		seen[e.id] = true
		st := c.stateFor(e.id)
		st.missing = 0
		p := prevByID[e.id]

		// Rates are computed over the time since this device's counters
		// were last read, not since the last poll: a device carried through
		// polls it was absent from would otherwise report its whole delta
		// against a single interval.
		var rateDT float64
		if e.local != nil && !st.lastBytesAt.IsZero() {
			rateDT = now.Sub(st.lastBytesAt).Seconds()
		}
		d := c.mergeDevice(now, status, e, p, rateDT)
		if e.local != nil {
			st.lastBytesAt = now
		} else {
			st.lastBytesAt = time.Time{}
		}
		pd := polledDevice{}
		if po, ok := pingResults[e.id]; ok {
			pd.pinged = applyPing(now, &d, po)
			if !pd.pinged {
				// The scheduled ping failed: the carried latency no longer
				// describes the link. LastPing stays for display.
				d.Connectivity.LatencyMs = nil
			}
		}
		var out *agentOutcome
		if ao, ok := agentResults[e.id]; ok {
			out = &ao
		}
		fresh, agentEvents := c.applyAgent(now, e, p, out, st, &d)
		pd.fresh = fresh
		c.finishUptime(now, p, &d)

		events = append(events, deviceEvents(now, p, &d)...)
		events = append(events, agentEvents...)
		pd.dev = d
		polled = append(polled, pd)
		if d.IsSelf && self == nil {
			selfCopy := d
			self = &selfCopy
		}
	}

	// Devices from the previous snapshot that no source reported.
	for i := range prev {
		p := &prev[i]
		if seen[p.ID] {
			continue
		}
		st := c.stateFor(p.ID)
		d := *p
		d.UpdatedAt = now
		d.Connectivity.RxRate = 0
		d.Connectivity.TxRate = 0
		switch {
		case !c.synced:
			// First successful sync after start: a stored device that no
			// source knows was removed while the hub was down. Keep the
			// row, mark it offline, but do not replay a removal event.
			st.missing = removedAfterPolls
			st.lastBytesAt = time.Time{}
			c.markRemoved(now, &d)
		case st.missing < removedAfterPolls:
			st.missing++
			if st.missing == removedAfterPolls {
				st.lastBytesAt = time.Time{}
				c.markRemoved(now, &d)
				events = append(events, removedEvent(now, &d, removedAfterPolls))
			}
		}
		if st.missing >= removedAfterPolls {
			removed[d.ID] = true
		}
		c.finishUptime(now, p, &d)
		polled = append(polled, polledDevice{dev: d})
	}
	c.synced = true

	// Devices forgotten while this poll was running must not be written
	// back; if a source still reports one it returns next poll as new.
	forgotten := c.consumeForgotten(ctx)

	devices := make([]model.Device, 0, len(polled))
	samples := make([]model.Sample, 0, len(polled))
	for i := range polled {
		if forgotten[polled[i].dev.ID] {
			continue
		}
		devices = append(devices, polled[i].dev)
		samples = append(samples, buildSample(now, &polled[i].dev, polled[i].pinged, polled[i].fresh))
	}
	sortDevices(devices)

	var persistErr error
	if err := c.st.UpsertDevices(ctx, devices); err != nil {
		persistErr = errors.Join(persistErr, fmt.Errorf("collector: persist devices: %w", err))
	}
	if err := c.st.InsertSamples(ctx, samples); err != nil {
		persistErr = errors.Join(persistErr, fmt.Errorf("collector: persist samples: %w", err))
	}
	if persistErr != nil && ctx.Err() == nil {
		c.log.Warn("collector: persistence failed", "err", persistErr)
	}

	// A Forget that ran between the check above and UpsertDevices has
	// already deleted its row, which the upsert just wrote back:
	// consumeForgotten deletes it again; keep the device out of this
	// snapshot too.
	if late := c.consumeForgotten(ctx); len(late) > 0 {
		maps.Copy(forgotten, late)
		devices = slices.DeleteFunc(devices, func(d model.Device) bool { return late[d.ID] })
	}
	for id := range forgotten {
		delete(removed, id)
	}

	hub := c.buildHub(now, status, self, errs)
	overview := c.buildOverview(ctx, now, devices, hub)
	snap := model.Snapshot{Overview: overview, Devices: devices}

	c.publish(snap, hub, removed)
	for _, e := range events {
		if forgotten[e.DeviceID] {
			continue
		}
		c.Emit(ctx, e)
	}
	// Publish a copy taken under the read lock: the local slice now backs
	// c.snap, which PingNow may update concurrently.
	c.ticks.Publish(c.Snapshot())
	c.log.Debug("collector: poll complete",
		"devices", len(devices), "online", overview.Online, "events", len(events),
		"pinged", len(pingResults), "agents", len(agentResults), "duration", c.now().Sub(now))
	return persistErr
}

// degradedPoll republishes the previous devices with Path unknown when
// tailscaled cannot be reached, records the error and emits hub.error.
func (c *Collector) degradedPoll(ctx context.Context, now time.Time, prev []model.Device, statusErr error) error {
	msg := truncate(statusErr.Error(), maxErrorTextLen)
	c.log.Warn("collector: tailscaled status failed", "err", statusErr)

	devices := make([]model.Device, len(prev))
	for i, d := range prev {
		d.Connectivity.Path = model.PathUnknown
		d.Connectivity.Relay = ""
		d.Connectivity.LatencyMs = nil // measured through the daemon we cannot reach
		d.Connectivity.RxRate = 0
		d.Connectivity.TxRate = 0
		d.UpdatedAt = now
		devices[i] = d
	}
	sortDevices(devices)

	hub := c.Hub()
	hub.LastPoll = now
	hub.LastError = "tailscaled: " + msg
	overview := c.buildOverview(ctx, now, devices, hub)
	c.mu.RLock()
	removed := maps.Clone(c.removed)
	c.mu.RUnlock()
	c.publish(model.Snapshot{Overview: overview, Devices: devices}, hub, removed)
	if c.shouldEmitHubError(now, msg) {
		c.Emit(ctx, hubErrorEvent(now, msg))
	}
	c.ticks.Publish(c.Snapshot())
	return nil
}

// loadStored seeds the snapshot with the devices persisted by a previous
// run so the first poll compares against them (no event flood on restart).
func (c *Collector) loadStored(ctx context.Context) error {
	if c.loaded {
		return nil
	}
	stored, err := c.st.ListDevices(ctx)
	if err != nil {
		return fmt.Errorf("collector: load stored devices: %w", err)
	}
	for i := range stored {
		c.stateFor(stored[i].ID)
	}
	sortDevices(stored)
	c.mu.Lock()
	c.snap.Devices = stored
	c.mu.Unlock()
	c.loaded = true
	if len(stored) > 0 {
		c.log.Info("collector: loaded stored devices", "count", len(stored))
	}
	return nil
}

// refreshRatiosIfStale recomputes the 24h/7d/30d uptime ratios when they
// are older than five minutes (or never computed).
func (c *Collector) refreshRatiosIfStale(ctx context.Context, now time.Time) {
	if !c.ratios.at.IsZero() && now.Sub(c.ratios.at) < ratiosRefreshInterval {
		return
	}
	r := uptimeRatios{at: now}
	var err error
	if r.d24h, err = c.st.UptimeRatios(ctx, now.Add(-24*time.Hour)); err == nil {
		if r.d7d, err = c.st.UptimeRatios(ctx, now.Add(-7*24*time.Hour)); err == nil {
			r.d30d, err = c.st.UptimeRatios(ctx, now.Add(-30*24*time.Hour))
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("collector: uptime ratios failed", "err", err)
		}
		c.ratios.at = now // do not retry on every poll
		return
	}
	c.ratios = r
}

// due reports whether a periodic action last attempted at last is due
// again at now, allowing a small tolerance so ticker drift never skips a
// whole interval.
func (c *Collector) due(now, last time.Time, interval time.Duration) bool {
	if last.IsZero() {
		return true
	}
	tolerance := c.cfg.PollInterval / scheduleToleranceFactor
	return now.Sub(last) >= interval-tolerance
}

// stateFor returns (creating if needed) the bookkeeping for a device.
func (c *Collector) stateFor(id model.DeviceID) *devState {
	st, ok := c.state[id]
	if !ok {
		st = &devState{}
		c.state[id] = st
	}
	return st
}

// shouldEmitHubError rate-limits hub.error events to one per distinct
// message per 10 minutes.
func (c *Collector) shouldEmitHubError(now time.Time, msg string) bool {
	for k, t := range c.hubErrors {
		if now.Sub(t) >= hubErrorRepeatInterval {
			delete(c.hubErrors, k)
		}
	}
	if _, ok := c.hubErrors[msg]; ok {
		return false
	}
	c.hubErrors[msg] = now
	return true
}

// publish installs a new snapshot, hub info and removed set under the
// write lock. Devices forgotten since the poll last consumed the forgotten
// set are dropped here so a Forget that raced with the tail of a poll never
// reappears in the published snapshot; the next poll cleans up their
// bookkeeping.
func (c *Collector) publish(snap model.Snapshot, hub model.HubInfo, removed map[model.DeviceID]bool) {
	if removed == nil {
		removed = map[model.DeviceID]bool{}
	}
	c.mu.Lock()
	if len(c.forgotten) > 0 {
		snap.Devices = slices.DeleteFunc(slices.Clone(snap.Devices), func(d model.Device) bool { return c.forgotten[d.ID] })
		for id := range c.forgotten {
			delete(removed, id)
		}
	}
	c.snap = snap
	c.hub = hub
	c.removed = removed
	c.mu.Unlock()
}

// consumeForgotten takes the set of devices forgotten since the last call
// and drops their poll-to-poll bookkeeping and cached control API rows, so
// a forgotten device only returns when a source reports it again. It also
// deletes their stored rows once more: Forget deletes the row itself, but
// a poll's UpsertDevices may have written it back in between (the row is
// gone from the snapshot either way, so this is cheap insurance against a
// restart resurrecting it). The result is never nil.
func (c *Collector) consumeForgotten(ctx context.Context) map[model.DeviceID]bool {
	forgotten := c.takeForgotten()
	if forgotten == nil {
		return map[model.DeviceID]bool{}
	}
	for id := range forgotten {
		delete(c.state, id)
		if err := c.st.DeleteDevice(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			c.log.Warn("collector: deleting forgotten device failed", "device", id, "err", err)
		}
	}
	c.apiDevices = slices.DeleteFunc(c.apiDevices, func(ad source.APIDevice) bool { return forgotten[ad.NodeID] })
	return forgotten
}

// pingAll disco-pings every online, non-self netmap peer with a Tailscale
// IP concurrently (bounded by PingConcurrency, 5s per ping).
func (c *Collector) pingAll(ctx context.Context, entries []pollEntry) map[model.DeviceID]pingOutcome {
	targets := make([]pingTarget, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.isSelf || !e.online || e.local == nil || e.ip == "" {
			continue
		}
		targets = append(targets, pingTarget{id: e.id, ip: e.ip})
	}
	if len(targets) == 0 {
		return map[model.DeviceID]pingOutcome{}
	}
	results := make([]pingOutcome, len(targets))
	sem := make(chan struct{}, c.cfg.PingConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = pingOutcome{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			defer cancel()
			reply, err := c.local.Ping(pctx, t.ip, pingTimeout)
			results[i] = pingOutcome{reply: reply, err: err}
		}()
	}
	wg.Wait()
	out := make(map[model.DeviceID]pingOutcome, len(targets))
	for i, t := range targets {
		if results[i].err != nil {
			c.log.Debug("collector: ping failed", "device", t.id, "err", results[i].err)
		}
		out[t.id] = results[i]
	}
	return out
}

// buildSample derives the raw time-series row for a device from this poll.
// Latency is recorded only when a ping succeeded in this poll and agent
// metrics only when a fresh report was applied.
func buildSample(now time.Time, d *model.Device, pinged, fresh bool) model.Sample {
	s := model.Sample{
		DeviceID:  d.ID,
		TS:        now,
		Online:    d.Online,
		TSRxBytes: d.Connectivity.RxBytes,
		TSTxBytes: d.Connectivity.TxBytes,
		TSRxRate:  d.Connectivity.RxRate,
		TSTxRate:  d.Connectivity.TxRate,
	}
	if pinged && d.Connectivity.LatencyMs != nil {
		v := *d.Connectivity.LatencyMs
		s.LatencyMs = &v
	}
	switch d.Connectivity.Path {
	case model.PathDirect:
		t := true
		s.Direct = &t
	case model.PathRelay:
		f := false
		s.Direct = &f
		s.Relay = d.Connectivity.Relay
	}
	if fresh && d.Metrics != nil {
		m := d.Metrics
		s.AgentOK = true
		s.CPU = ptr(m.CPUPercent)
		s.Mem = ptr(m.MemPercent)
		if len(m.Disks) > 0 {
			s.Disk = ptr(m.DiskPercent)
		}
		s.Load1 = ptr(m.Load1)
		s.NetRxRate = ptr(m.NetRxRate)
		s.NetTxRate = ptr(m.NetTxRate)
		s.TempC = maxTemperature(m.Temperatures)
		up := m.UptimeSeconds
		s.Uptime = &up
	}
	return s
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
