package collector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentclient"
	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// maxAgentRateGap is the longest interval between two reports over which
// counter deltas are still turned into rates; after a longer gap the
// baseline is reset and rates are 0 for one report.
const maxAgentRateGap = 15 * time.Minute

// agentOutcome is the result of one agent fetch attempt.
type agentOutcome struct {
	report *agentproto.Report
	err    error
}

// agentTarget is one device selected for an agent fetch.
type agentTarget struct {
	id model.DeviceID
	ip string
}

// fetchAgents fetches reports concurrently from every online device with a
// Tailscale IP that is in the hub's netmap (including the hub itself) and
// not currently backing off. The result holds one entry per attempted
// device.
func (c *Collector) fetchAgents(ctx context.Context, now time.Time, entries []pollEntry, prevByID map[model.DeviceID]*model.Device) map[model.DeviceID]agentOutcome {
	targets := make([]agentTarget, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if !e.online || e.ip == "" || (e.local == nil && !e.isSelf) {
			continue
		}
		st := c.stateFor(e.id)
		if p := prevByID[e.id]; p != nil && !p.Online {
			// Fresh start after the device came back: forget the backoff.
			st.agent.failures = 0
			st.agent.nextTry = time.Time{}
		}
		if !st.agent.nextTry.IsZero() && now.Before(st.agent.nextTry) {
			continue
		}
		targets = append(targets, agentTarget{id: e.id, ip: e.ip})
	}
	if len(targets) == 0 {
		return map[model.DeviceID]agentOutcome{}
	}

	results := make([]agentOutcome, len(targets))
	sem := make(chan struct{}, c.cfg.AgentConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = agentOutcome{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			fctx, cancel := context.WithTimeout(ctx, c.cfg.AgentTimeout)
			defer cancel()
			rep, err := c.agents.Fetch(fctx, t.ip, c.cfg.AgentPort)
			if err == nil && rep == nil {
				err = errors.New("agent returned an empty report")
			}
			results[i] = agentOutcome{report: rep, err: err}
		}()
	}
	wg.Wait()

	out := make(map[model.DeviceID]agentOutcome, len(targets))
	for i, t := range targets {
		out[t.id] = results[i]
	}
	return out
}

// applyAgent sets d.Agent and d.Metrics from this poll's fetch outcome (or
// its absence), updates the backoff schedule and returns whether a fresh
// report was applied plus any agent state-transition events.
func (c *Collector) applyAgent(now time.Time, e *pollEntry, prev *model.Device, out *agentOutcome, st *devState, d *model.Device) (fresh bool, events []model.Event) {
	var prevAgent model.AgentStatus
	var prevMetrics *model.MetricsSnapshot
	if prev != nil {
		prevAgent = prev.Agent
		prevMetrics = prev.Metrics
	}
	url := ""
	if e.ip != "" {
		url = agentURL(e.ip, c.cfg.AgentPort)
	}

	switch {
	case !c.cfg.AgentEnabled:
		d.Agent = model.AgentStatus{State: model.AgentDisabled}
		d.Metrics = nil
		return false, nil

	case !e.online || e.ip == "" || (e.local == nil && !e.isSelf):
		// Offline, unreachable by definition, or not in the hub's netmap.
		d.Agent = model.AgentStatus{
			State:       model.AgentUnknown,
			Version:     prevAgent.Version,
			LastSuccess: prevAgent.LastSuccess,
			LastError:   prevAgent.LastError,
			URL:         url,
		}
		d.Metrics = nil
		return false, nil

	case out == nil:
		// Not attempted this poll (backing off): carry the last state.
		a := prevAgent
		a.URL = url
		if a.State == "" || a.State == model.AgentDisabled {
			a.State = model.AgentUnknown
		}
		d.Agent = a
		d.Metrics = prevMetrics
		return false, nil

	case out.err != nil:
		st.agent.failures++
		unauthorized := isUnauthorized(out.err)
		st.agent.nextTry = now.Add(c.backoffDelay(st.agent.failures, unauthorized))
		errText := truncate(out.err.Error(), maxErrorTextLen)
		d.Agent = model.AgentStatus{
			State:       model.AgentUnreachable,
			Version:     prevAgent.Version,
			LastSuccess: prevAgent.LastSuccess,
			LastError:   errText,
			URL:         url,
		}
		d.Metrics = prevMetrics
		// An agent that was reached before (also one whose state was
		// unknown while the device was offline or the hub was down) has
		// just become unreachable; a device that never had an agent stays
		// quiet.
		if prevAgent.State != model.AgentUnreachable && prevAgent.LastSuccess != nil {
			events = append(events, newDeviceEvent(now, d, model.EventAgentUnreachable, model.SeverityInfo,
				"Agent unreachable: "+d.Name,
				fmt.Sprintf("Agent on %s is unreachable: %s", d.Name, errText),
				map[string]any{"error": errText, "unauthorized": unauthorized}))
		}
		return false, events

	default:
		st.agent.failures = 0
		st.agent.nextTry = time.Time{}
		d.Metrics = buildMetrics(out.report, st.agent.prev)
		st.agent.prev = cloneReport(out.report) // baseline must not alias client memory
		at := now
		d.Agent = model.AgentStatus{
			State:       model.AgentReachable,
			Version:     out.report.AgentVersion,
			LastSuccess: &at,
			URL:         url,
		}
		if prevAgent.State != model.AgentReachable {
			events = append(events, newDeviceEvent(now, d, model.EventAgentReachable, model.SeverityInfo,
				"Agent reachable: "+d.Name,
				fmt.Sprintf("Agent on %s is reachable (version %s)", d.Name, displayVersion(out.report.AgentVersion)),
				map[string]any{"agentVersion": out.report.AgentVersion}))
		}
		return true, events
	}
}

// backoffDelay returns the delay before the next agent attempt: 30s
// doubling per consecutive failure up to 10 minutes, or 10 minutes at once
// for an authorization failure, with +/-15% jitter.
func (c *Collector) backoffDelay(failures int, unauthorized bool) time.Duration {
	d := agentBackoffMax
	if !unauthorized {
		d = agentBackoffMin
		for i := 1; i < failures && d < agentBackoffMax; i++ {
			d *= 2
		}
		if d > agentBackoffMax {
			d = agentBackoffMax
		}
	}
	j := 0.5
	if c.jitter != nil {
		j = c.jitter()
	}
	if j < 0 || j > 1 || math.IsNaN(j) {
		j = 0.5
	}
	factor := 1 - agentBackoffJitter/2 + agentBackoffJitter*j
	return time.Duration(float64(d) * factor)
}

// isUnauthorized reports whether an agent error means the hub was rejected
// by the agent's allow-list or token check.
func isUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, agentclient.ErrUnauthorized) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "http 401") || strings.Contains(msg, "http 403")
}

// agentURL is the base URL of the agent on the given IP, for display.
func agentURL(ip string, port int) string {
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
}

// displayVersion renders a possibly empty version string.
func displayVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// buildMetrics converts an agent report into a MetricsSnapshot, computing
// per-interface and total physical-interface rates from the previous
// report's counters (bytes/second; negative deltas and missing baselines
// yield 0).
func buildMetrics(rep, prev *agentproto.Report) *model.MetricsSnapshot {
	if rep == nil {
		return nil
	}
	m := &model.MetricsSnapshot{
		SampledAt:       rep.SampledAt,
		Platform:        rep.Host.Platform,
		PlatformVersion: rep.Host.PlatformVersion,
		Kernel:          rep.Host.Kernel,
		Arch:            rep.Host.Arch,
		UptimeSeconds:   rep.Host.UptimeSeconds,
		CPUPercent:      finite(rep.CPU.Percent),
		CPUCount:        rep.CPU.Count,
		CPUModel:        rep.CPU.Model,
		Load1:           finite(rep.CPU.Load1),
		Load5:           finite(rep.CPU.Load5),
		Load15:          finite(rep.CPU.Load15),
		MemTotal:        rep.Memory.Total,
		MemUsed:         rep.Memory.Used,
		MemAvailable:    rep.Memory.Available,
		MemPercent:      finite(rep.Memory.Percent),
		SwapTotal:       rep.Memory.SwapTotal,
		SwapUsed:        rep.Memory.SwapUsed,
		Disks:           make([]model.DiskUsage, 0, len(rep.Disks)),
		Interfaces:      make([]model.InterfaceCounters, 0, len(rep.Net.Interfaces)),
		TailscaleIf:     rep.Net.TailscaleIf,
		Processes:       rep.Processes,
	}
	if len(rep.CPU.PerCore) > 0 {
		m.PerCore = make([]float64, len(rep.CPU.PerCore))
		for i, v := range rep.CPU.PerCore {
			m.PerCore[i] = finite(v)
		}
	}
	if !rep.Host.BootTime.IsZero() {
		bt := rep.Host.BootTime
		m.BootTime = &bt
	}
	if rep.Tailscale != nil {
		m.TailscaleVer = rep.Tailscale.Version
	}

	primary := false
	maxPct := 0.0
	for _, dk := range rep.Disks {
		pct := finite(dk.Percent)
		m.Disks = append(m.Disks, model.DiskUsage{Mount: dk.Mount, FSType: dk.FSType, Total: dk.Total, Used: dk.Used, Percent: pct})
		if dk.Primary && !primary {
			m.DiskPercent = pct
			primary = true
		}
		if pct > maxPct {
			maxPct = pct
		}
	}
	if !primary {
		m.DiskPercent = maxPct
	}

	var dt float64
	prevIf := map[string]agentproto.Interface{}
	if prev != nil {
		dt = rep.SampledAt.Sub(prev.SampledAt).Seconds()
		if dt <= 0 || dt > maxAgentRateGap.Seconds() {
			dt = 0
		} else {
			for _, pi := range prev.Net.Interfaces {
				prevIf[pi.Name] = pi
			}
		}
	}
	for _, in := range rep.Net.Interfaces {
		ic := model.InterfaceCounters{
			Name:      in.Name,
			RxBytes:   in.RxBytes,
			TxBytes:   in.TxBytes,
			RxPackets: in.RxPackets,
			TxPackets: in.TxPackets,
			RxErrors:  in.RxErrors,
			TxErrors:  in.TxErrors,
		}
		if dt > 0 {
			if pi, ok := prevIf[in.Name]; ok {
				ic.RxRate = counterRateUint(in.RxBytes, pi.RxBytes, dt)
				ic.TxRate = counterRateUint(in.TxBytes, pi.TxBytes, dt)
			}
		}
		if in.Physical {
			m.NetRxRate += ic.RxRate
			m.NetTxRate += ic.TxRate
		}
		m.Interfaces = append(m.Interfaces, ic)
	}

	if len(rep.Temperatures) > 0 {
		m.Temperatures = make([]model.Temperature, 0, len(rep.Temperatures))
		for _, t := range rep.Temperatures {
			m.Temperatures = append(m.Temperatures, model.Temperature{Sensor: t.Sensor, Celsius: finite(t.Celsius), Critical: finite(t.Critical)})
		}
	}
	return m
}

// counterRateUint computes a bytes/second rate from two cumulative
// counters; a counter reset (cur < prev) yields 0.
func counterRateUint(cur, prev uint64, dt float64) float64 {
	if dt <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

// maxTemperature returns the hottest sensor reading, or nil when there are
// no readings.
func maxTemperature(temps []model.Temperature) *float64 {
	if len(temps) == 0 {
		return nil
	}
	mx := temps[0].Celsius
	for _, t := range temps[1:] {
		mx = max(mx, t.Celsius)
	}
	return &mx
}

// finite maps NaN and infinities to 0 so bad upstream numbers never reach
// JSON encoding or SQLite.
func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// cloneReport deep-copies a report so the rate baseline kept between polls
// never aliases memory owned by the AgentClient implementation.
func cloneReport(r *agentproto.Report) *agentproto.Report {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Disks = slices.Clone(r.Disks)
	cp.Net.Interfaces = slices.Clone(r.Net.Interfaces)
	cp.Temperatures = slices.Clone(r.Temperatures)
	cp.CPU.PerCore = slices.Clone(r.CPU.PerCore)
	if r.Tailscale != nil {
		ts := *r.Tailscale
		ts.IPs = slices.Clone(r.Tailscale.IPs)
		ts.Health = slices.Clone(r.Tailscale.Health)
		cp.Tailscale = &ts
	}
	return &cp
}
