package alerts

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// evalContext is the per-(rule, device) state handed to the condition
// functions.
type evalContext struct {
	now   time.Time
	open  *model.Alert // the open alert for this rule+device, nil when none
	since time.Time    // engine-tracked start of the condition (zero when none)
	isNew bool         // new_device: the device was first observed on this tick
}

// observation is the outcome of evaluating one rule against one device.
type observation struct {
	active   bool
	severity model.Severity // severity the alert should have now (rule severity or escalated)
	value    *float64       // observed value (also set on inactive observations when known)
	since    time.Time      // start of the condition (active observations only)
	title    string
	message  string
	resolved string // message stored when an open alert clears (inactive observations)
	data     map[string]any
}

// observe evaluates rule r for device d.
func observe(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	switch r.Type {
	case model.RuleDeviceOffline:
		return observeOffline(r, d, ec)
	case model.RuleHighCPU:
		return observeCPU(r, d, ec)
	case model.RuleHighMemory:
		return observeMemory(r, d, ec)
	case model.RuleDiskFull:
		return observeDisk(r, d, ec)
	case model.RuleHighLatency:
		return observeLatency(r, d, ec)
	case model.RuleRelayOnly:
		return observeRelay(r, d, ec)
	case model.RuleKeyExpiring:
		return observeKeyExpiring(r, d, ec)
	case model.RuleUpdateAvailable:
		return observeUpdate(r, d, ec)
	case model.RuleAgentUnreachable:
		return observeAgent(r, d, ec)
	case model.RuleNewDevice:
		return observeNewDevice(r, d, ec)
	case model.RuleUnauthorized:
		return observeUnauthorized(r, d, ec)
	case model.RuleHighTemp:
		return observeTemperature(r, d, ec)
	case model.RuleHighLoad:
		return observeLoad(r, d, ec)
	}
	return observation{}
}

// --- helpers --------------------------------------------------------------

// condStart picks the start time of an active condition: the upstream time
// when the collector knows one (and it is not in the future), otherwise the
// engine-tracked start, otherwise now.
func condStart(ec evalContext, upstream time.Time) time.Time {
	if !upstream.IsZero() && !upstream.After(ec.now) {
		return upstream
	}
	if !ec.since.IsZero() {
		return ec.since
	}
	return ec.now
}

// openDuration is how long the (still open) alert's condition has held,
// for use in resolution messages.
func openDuration(ec evalContext) time.Duration {
	start := ec.since
	if start.IsZero() && ec.open != nil {
		start = ec.open.OpenedAt
	}
	if start.IsZero() {
		return 0
	}
	return ec.now.Sub(start)
}

// usableMetrics returns the device's agent metrics when they can be trusted:
// the agent is reachable, or it became unreachable less than
// metricsStaleAfter ago (the collector carries the last report over).
func usableMetrics(d *model.Device, now time.Time) *model.MetricsSnapshot {
	m := d.Metrics
	if m == nil {
		return nil
	}
	switch d.Agent.State {
	case model.AgentReachable, "":
		return m
	case model.AgentDisabled:
		return nil
	default:
		if d.Agent.LastSuccess != nil && now.Sub(*d.Agent.LastSuccess) <= metricsStaleAfter {
			return m
		}
		return nil
	}
}

// displayName is the device name used in alert titles.
func displayName(d *model.Device) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.Hostname != "":
		return d.Hostname
	default:
		return string(d.ID)
	}
}

// humanDuration formats d compactly: "45s", "6m", "2h 13m", "3d 4h".
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, h)
	}
}

// clockTime formats t in now's location: "15:04" on the same day, otherwise
// "Jan 2 15:04".
func clockTime(t, now time.Time) string {
	t = t.In(now.Location())
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// clockDate formats t in now's location as "Jan 2 15:04".
func clockDate(t, now time.Time) string {
	return t.In(now.Location()).Format("Jan 2 15:04")
}

func fmtPercent(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func fmtMs(v float64) string      { return fmt.Sprintf("%.0fms", v) }
func fmtTemp(v float64) string    { return fmt.Sprintf("%.0f°C", v) }
func fmtRatio(v float64) string   { return fmt.Sprintf("%.2f", v) }

// truncate shortens s to at most n bytes without splitting a rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// numericParams describes a "value above threshold" rule for
// numericObservation.
type numericParams struct {
	label  string               // "CPU", "Memory", "Disk", ...
	title  string               // format with one %s for the device name
	format func(float64) string // value formatter
	extra  map[string]any       // extra Data entries
}

// numericObservation implements the shared logic of every "value >
// threshold" rule, including hysteresis for open alerts and the "for"
// suffix of sustained rules. Callers may refine severity and message.
func numericObservation(r *model.AlertRule, d *model.Device, ec evalContext, v float64, p numericParams) observation {
	spec := ruleSpecs[r.Type]
	thr := r.Threshold
	if ec.open != nil {
		thr = math.Max(0, thr-spec.hysteresis)
	}
	val := v
	data := map[string]any{
		"threshold":  r.Threshold,
		"value":      val,
		"unit":       spec.unit,
		"forSeconds": r.ForSeconds,
	}
	for k, x := range p.extra {
		data[k] = x
	}
	if !(v > thr) {
		return observation{
			value:    &val,
			resolved: fmt.Sprintf("%s back to %s (threshold %s)", p.label, p.format(v), p.format(r.Threshold)),
			data:     data,
		}
	}
	start := condStart(ec, time.Time{})
	msg := fmt.Sprintf("%s at %s", p.label, p.format(v))
	if r.ForSeconds > 0 {
		msg += " for " + humanDuration(ec.now.Sub(start))
	}
	msg += fmt.Sprintf(" (threshold %s)", p.format(r.Threshold))
	return observation{
		active:   true,
		severity: r.Severity,
		value:    &val,
		since:    start,
		title:    fmt.Sprintf(p.title, displayName(d)),
		message:  msg,
		data:     data,
	}
}

// --- rule conditions ------------------------------------------------------

func observeOffline(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if d.Online {
		return observation{resolved: "Back online after " + humanDuration(openDuration(ec))}
	}
	var upstream time.Time
	if d.Uptime.LastChange != nil {
		upstream = *d.Uptime.LastChange
	}
	start := condStart(ec, upstream)
	dur := ec.now.Sub(start)
	msg := "Offline for " + humanDuration(dur)
	data := map[string]any{
		"offlineSeconds": int64(dur.Seconds()),
		"forSeconds":     r.ForSeconds,
	}
	if !d.LastSeen.IsZero() {
		msg += " (last seen " + clockTime(d.LastSeen, ec.now) + ")"
		data["lastSeen"] = d.LastSeen.UTC().Format(time.RFC3339)
	}
	return observation{
		active:   true,
		severity: r.Severity,
		since:    start,
		title:    displayName(d) + " is offline",
		message:  msg,
		data:     data,
	}
}

func observeCPU(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	m := usableMetrics(d, ec.now)
	if m == nil {
		return observation{resolved: "Agent metrics no longer available"}
	}
	return numericObservation(r, d, ec, m.CPUPercent, numericParams{
		label: "CPU", title: "CPU high on %s", format: fmtPercent,
		extra: map[string]any{"cpuCount": m.CPUCount, "load1": m.Load1},
	})
}

func observeMemory(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	m := usableMetrics(d, ec.now)
	if m == nil {
		return observation{resolved: "Agent metrics no longer available"}
	}
	return numericObservation(r, d, ec, m.MemPercent, numericParams{
		label: "Memory", title: "Memory high on %s", format: fmtPercent,
		extra: map[string]any{"memTotal": m.MemTotal, "memUsed": m.MemUsed},
	})
}

func observeDisk(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	m := usableMetrics(d, ec.now)
	if m == nil {
		return observation{resolved: "Agent metrics no longer available"}
	}
	mount := ""
	for _, disk := range m.Disks {
		if disk.Percent == m.DiskPercent && disk.Mount != "" {
			mount = disk.Mount
			break
		}
	}
	obs := numericObservation(r, d, ec, m.DiskPercent, numericParams{
		label: "Disk", title: "Disk almost full on %s", format: fmtPercent,
		extra: map[string]any{"mount": mount},
	})
	if !obs.active {
		return obs
	}
	if m.DiskPercent >= diskCriticalPercent {
		obs.severity = maxSeverity(obs.severity, model.SeverityCritical)
	}
	msg := "Disk at " + fmtPercent(m.DiskPercent)
	if mount != "" {
		msg += " on " + mount
	}
	if r.ForSeconds > 0 {
		msg += " for " + humanDuration(ec.now.Sub(obs.since))
	}
	msg += " (threshold " + fmtPercent(r.Threshold) + ")"
	obs.message = msg
	return obs
}

func observeLatency(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	c := &d.Connectivity
	if !d.Online || c.LatencyMs == nil || c.LastPing == nil || ec.now.Sub(*c.LastPing) > latencyStaleAfter {
		return observation{resolved: "Device offline or latency no longer measured"}
	}
	return numericObservation(r, d, ec, *d.Connectivity.LatencyMs, numericParams{
		label: "Latency", title: "High latency to %s", format: fmtMs,
		extra: map[string]any{"path": string(d.Connectivity.Path), "relay": d.Connectivity.Relay},
	})
}

func observeRelay(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if !d.Online {
		return observation{resolved: "Device went offline"}
	}
	if d.Connectivity.Path != model.PathRelay {
		return observation{resolved: "Direct path restored"}
	}
	start := condStart(ec, time.Time{})
	via := "a DERP relay"
	if d.Connectivity.Relay != "" {
		via = "DERP " + d.Connectivity.Relay
	}
	return observation{
		active:   true,
		severity: r.Severity,
		since:    start,
		title:    displayName(d) + " is relayed",
		message:  fmt.Sprintf("Relayed via %s for %s (no direct path)", via, humanDuration(ec.now.Sub(start))),
		data:     map[string]any{"relay": d.Connectivity.Relay, "forSeconds": r.ForSeconds},
	}
}

func observeKeyExpiring(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if d.KeyExpiryDisabled || d.KeyExpiry == nil || d.Expired {
		return observation{resolved: "Key expiry no longer imminent"}
	}
	remaining := d.KeyExpiry.Sub(ec.now)
	if remaining < 0 {
		return observation{resolved: "Key expired"}
	}
	days := remaining.Hours() / 24
	val := math.Round(days*10) / 10
	data := map[string]any{
		"threshold": r.Threshold,
		"value":     val,
		"unit":      "days",
		"keyExpiry": d.KeyExpiry.UTC().Format(time.RFC3339),
	}
	if days > r.Threshold {
		return observation{value: &val, resolved: "Key now expires in " + humanDuration(remaining), data: data}
	}
	sev := r.Severity
	if days < keyCriticalDays {
		sev = maxSeverity(sev, model.SeverityCritical)
	}
	return observation{
		active:   true,
		severity: sev,
		value:    &val,
		since:    condStart(ec, time.Time{}),
		title:    "Key expiring on " + displayName(d),
		message:  fmt.Sprintf("Node key expires in %s (%s)", humanDuration(remaining), clockDate(*d.KeyExpiry, ec.now)),
		data:     data,
	}
}

func observeUpdate(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if !d.UpdateAvailable {
		return observation{resolved: "Client up to date"}
	}
	msg := "A newer Tailscale client is available"
	if d.ClientVersion != "" {
		msg = fmt.Sprintf("Running Tailscale %s; a newer version is available", d.ClientVersion)
	}
	return observation{
		active:   true,
		severity: r.Severity,
		since:    condStart(ec, time.Time{}),
		title:    "Update available for " + displayName(d),
		message:  msg,
		data:     map[string]any{"clientVersion": d.ClientVersion, "os": d.OS},
	}
}

func observeAgent(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if d.Agent.State != model.AgentUnreachable || d.Agent.LastSuccess == nil {
		if d.Agent.State == model.AgentReachable {
			return observation{resolved: "Agent reachable again"}
		}
		return observation{resolved: "Agent no longer polled"}
	}
	// The condition cannot have started before the device last came online:
	// LastSuccess is carried through an offline period, and the ForSeconds
	// grace must cover an agent still starting after a reboot.
	upstream := *d.Agent.LastSuccess
	if d.Uptime.LastChange != nil && d.Uptime.LastChange.After(upstream) {
		upstream = *d.Uptime.LastChange
	}
	start := condStart(ec, upstream)
	msg := fmt.Sprintf("Agent unreachable for %s (last success %s)",
		humanDuration(ec.now.Sub(start)), clockTime(*d.Agent.LastSuccess, ec.now))
	if d.Agent.LastError != "" {
		msg += ": " + truncate(strings.TrimSpace(d.Agent.LastError), 160)
	}
	return observation{
		active:   true,
		severity: r.Severity,
		since:    start,
		title:    "Agent unreachable on " + displayName(d),
		message:  msg,
		data: map[string]any{
			"lastSuccess":  d.Agent.LastSuccess.UTC().Format(time.RFC3339),
			"error":        d.Agent.LastError,
			"agentVersion": d.Agent.Version,
			"forSeconds":   r.ForSeconds,
		},
	}
}

func observeNewDevice(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if ec.open != nil {
		age := ec.now.Sub(ec.open.OpenedAt)
		if age >= time.Duration(r.ForSeconds)*time.Second {
			return observation{resolved: "Auto-resolved after " + humanDuration(age)}
		}
		// Still within the auto-resolve window: keep the alert as it is.
		return observation{
			active:   true,
			severity: ec.open.Severity,
			value:    ec.open.Value,
			since:    ec.open.OpenedAt,
			title:    ec.open.Title,
			message:  ec.open.Message,
			data:     ec.open.Data,
		}
	}
	if !ec.isNew {
		return observation{}
	}
	var parts []string
	if d.OS != "" {
		parts = append(parts, d.OS)
	}
	switch {
	case d.User != "":
		parts = append(parts, d.User)
	case len(d.Tags) > 0:
		parts = append(parts, strings.Join(d.Tags, " "))
	}
	if d.IsExternal {
		parts = append(parts, "shared from another tailnet")
	}
	msg := displayName(d) + " joined the tailnet"
	if len(parts) > 0 {
		msg += " (" + strings.Join(parts, ", ") + ")"
	}
	first := d.FirstSeen
	if first.IsZero() {
		first = ec.now
	}
	msg += " at " + clockTime(first, ec.now)
	return observation{
		active:   true,
		severity: r.Severity,
		since:    ec.now,
		title:    "New device: " + displayName(d),
		message:  msg,
		data: map[string]any{
			"os":                 d.OS,
			"user":               d.User,
			"tags":               cloneStrings(d.Tags),
			"firstSeen":          first.UTC().Format(time.RFC3339),
			"autoResolveSeconds": r.ForSeconds,
		},
	}
}

func observeUnauthorized(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	if d.Authorized {
		return observation{resolved: "Device authorized"}
	}
	msg := "Waiting for authorization"
	if !d.Created.IsZero() {
		msg += " since " + clockDate(d.Created, ec.now)
	}
	switch {
	case d.User != "":
		msg += " (" + d.User + ")"
	case len(d.Tags) > 0:
		msg += " (" + strings.Join(d.Tags, " ") + ")"
	}
	return observation{
		active:   true,
		severity: r.Severity,
		since:    condStart(ec, time.Time{}),
		title:    displayName(d) + " needs authorization",
		message:  msg,
		data:     map[string]any{"user": d.User, "os": d.OS, "tags": cloneStrings(d.Tags)},
	}
}

func observeTemperature(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	m := usableMetrics(d, ec.now)
	if m == nil || len(m.Temperatures) == 0 {
		return observation{resolved: "Temperature readings no longer available"}
	}
	hottest := m.Temperatures[0]
	for _, t := range m.Temperatures[1:] {
		if t.Celsius > hottest.Celsius {
			hottest = t
		}
	}
	label := hottest.Sensor
	if label == "" {
		label = "Temperature"
	}
	return numericObservation(r, d, ec, hottest.Celsius, numericParams{
		label: label, title: "High temperature on %s", format: fmtTemp,
		extra: map[string]any{"sensor": hottest.Sensor, "critical": hottest.Critical},
	})
}

func observeLoad(r *model.AlertRule, d *model.Device, ec evalContext) observation {
	m := usableMetrics(d, ec.now)
	if m == nil || m.CPUCount <= 0 {
		return observation{resolved: "Load metrics no longer available"}
	}
	ratio := m.Load1 / float64(m.CPUCount)
	obs := numericObservation(r, d, ec, ratio, numericParams{
		label: "Load", title: "Load high on %s", format: fmtRatio,
		extra: map[string]any{"load1": m.Load1, "cpuCount": m.CPUCount},
	})
	if !obs.active {
		return obs
	}
	msg := fmt.Sprintf("Load %.2f on %d cores (%.2f per core)", m.Load1, m.CPUCount, ratio)
	if r.ForSeconds > 0 {
		msg += " for " + humanDuration(ec.now.Sub(obs.since))
	}
	msg += fmt.Sprintf(" (threshold %.2f)", r.Threshold)
	obs.message = msg
	return obs
}
