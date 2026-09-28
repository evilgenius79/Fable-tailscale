// Package sysmetrics samples system metrics (CPU, memory, disks, network
// counters, temperatures, host facts) with gopsutil and assembles them into
// agentproto.Report documents for tailwatch-agent.
//
// The Sampler runs in the background and keeps the most recent report; HTTP
// handlers read it with Latest, so a request never triggers expensive work.
// Every sub-collector is isolated: when one fails (unsupported platform,
// permission denied, hung sysfs entry) it is logged once at debug level and
// its section of the report is left zero. A report is always produced.
package sysmetrics

import (
	"context"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

const (
	// DefaultInterval is used when NewSampler is given a non-positive
	// interval.
	DefaultInterval = 5 * time.Second

	// MinInterval is the shortest sampling interval accepted.
	MinInterval = time.Second

	// SampleTimeout bounds one full sampling pass. Probes that ignore their
	// context (statfs, sysfs reads) can still block, but the loop moves on
	// as soon as they return.
	SampleTimeout = 10 * time.Second

	// TailscaleRefresh is the minimum time between calls to the function
	// installed with SetTailscaleInfo.
	TailscaleRefresh = 60 * time.Second

	// MaxPerCore caps the number of per-core CPU percentages reported.
	MaxPerCore = 64

	// MaxDisks caps the number of filesystems reported.
	MaxDisks = 32

	// MaxInterfaces caps the number of network interfaces reported.
	MaxInterfaces = 64

	// MaxTemperatures caps the number of temperature sensors reported.
	MaxTemperatures = 64

	// firstSampleDelay is the warm-up between priming the CPU counters and
	// the first sample so the first CPU percentage covers a real interval.
	firstSampleDelay = time.Second
)

// Sampler periodically collects system metrics and keeps the latest report.
// It is safe for concurrent use: Run may execute in one goroutine while
// Latest and SetTailscaleInfo are called from others.
type Sampler struct {
	interval time.Duration
	version  string
	log      *slog.Logger
	probes   probes
	now      func() time.Time

	mu     sync.RWMutex
	latest *agentproto.Report

	tsMu     sync.Mutex
	tsFn     func(ctx context.Context) (*agentproto.TailscaleInfo, error)
	tsNext   time.Time
	tsInfo   *agentproto.TailscaleInfo
	cpuModel string

	logOnce sync.Map // collector name -> struct{}
}

// NewSampler returns a Sampler that collects a report every interval. The
// interval is clamped to [MinInterval, ...) and defaults to DefaultInterval
// when non-positive. agentVersion is copied into every report. A nil logger
// falls back to slog.Default().
func NewSampler(interval time.Duration, agentVersion string, log *slog.Logger) *Sampler {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if interval < MinInterval {
		interval = MinInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &Sampler{
		interval: interval,
		version:  agentVersion,
		log:      log.With("component", "sysmetrics"),
		probes:   defaultProbes(),
		now:      time.Now,
	}
}

// Run samples in a loop until ctx is cancelled. It primes the CPU counters,
// takes a first sample after a short warm-up so Latest becomes available
// quickly, then samples every interval. Run returns when ctx is done; it
// never panics on probe failures.
func (s *Sampler) Run(ctx context.Context) {
	s.prime(ctx)

	warm := min(firstSampleDelay, s.interval)
	if !sleepCtx(ctx, warm) {
		return
	}
	s.sample(ctx)

	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample(ctx)
		}
	}
}

// Latest returns a deep copy of the most recent report. ok is false until
// the first sample has completed.
func (s *Sampler) Latest() (agentproto.Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return agentproto.Report{}, false
	}
	return cloneReport(*s.latest), true
}

// SetTailscaleInfo installs a function that is called at most every
// TailscaleRefresh (from the sampling goroutine) to populate
// Report.Tailscale. Passing nil removes it. Errors from fn are logged once at
// debug level and the previous value, if any, is kept.
func (s *Sampler) SetTailscaleInfo(fn func(ctx context.Context) (*agentproto.TailscaleInfo, error)) {
	s.tsMu.Lock()
	defer s.tsMu.Unlock()
	s.tsFn = fn
	s.tsNext = time.Time{}
	if fn == nil {
		s.tsInfo = nil
	}
}

// prime performs the first CPU percentage calls so subsequent calls with a
// zero interval measure utilisation since the previous sample.
func (s *Sampler) prime(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, SampleTimeout)
	defer cancel()
	if _, err := s.probes.cpuPercent(ctx, false); err != nil {
		s.debugOnce(ctx, "cpu.percent.prime", err)
	}
	if _, err := s.probes.cpuPercent(ctx, true); err != nil {
		s.debugOnce(ctx, "cpu.percpu.prime", err)
	}
}

// sample collects one report and stores it as the latest. It is bounded by
// SampleTimeout.
func (s *Sampler) sample(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, SampleTimeout)
	defer cancel()

	start := s.now()
	rep := agentproto.Report{
		ProtocolVersion: agentproto.ProtocolVersion,
		AgentVersion:    s.version,
		SampledAt:       start.UTC(),
	}
	rep.Host = s.collectHost(ctx)
	rep.CPU = s.collectCPU(ctx)
	rep.Memory = s.collectMemory(ctx)
	rep.Disks = s.collectDisks(ctx)
	rep.Net = s.collectNet(ctx)
	rep.Temperatures = s.collectTemperatures(ctx)
	rep.Processes = s.collectProcesses(ctx)
	rep.Tailscale = s.collectTailscale(ctx)

	s.mu.Lock()
	s.latest = &rep
	s.mu.Unlock()

	s.log.DebugContext(ctx, "sampled",
		"duration", s.now().Sub(start),
		"cpu", rep.CPU.Percent,
		"mem", rep.Memory.Percent,
		"disks", len(rep.Disks),
		"interfaces", len(rep.Net.Interfaces),
	)
}

// collectHost fills the Host section from host.Info plus runtime.GOOS/GOARCH.
func (s *Sampler) collectHost(ctx context.Context) agentproto.Host {
	h := agentproto.Host{OS: runtime.GOOS, Arch: runtime.GOARCH}
	info, err := s.probes.hostInfo(ctx)
	if err != nil || info == nil {
		s.debugOnce(ctx, "host.info", err)
		return h
	}
	h.Hostname = info.Hostname
	h.Platform = info.Platform
	h.PlatformVersion = info.PlatformVersion
	h.Kernel = info.KernelVersion
	h.UptimeSeconds = info.Uptime
	if info.BootTime > 0 {
		h.BootTime = time.Unix(int64(info.BootTime), 0).UTC()
	}
	return h
}

// collectCPU fills the CPU section: utilisation since the previous sample
// (total and per core), logical core count, model name and load averages.
func (s *Sampler) collectCPU(ctx context.Context) agentproto.CPU {
	var c agentproto.CPU
	if pct, err := s.probes.cpuPercent(ctx, false); err != nil {
		s.debugOnce(ctx, "cpu.percent", err)
	} else if len(pct) > 0 {
		c.Percent = clampPercent(pct[0])
	}
	if per, err := s.probes.cpuPercent(ctx, true); err != nil {
		s.debugOnce(ctx, "cpu.percpu", err)
	} else if len(per) > 0 {
		if len(per) > MaxPerCore {
			per = per[:MaxPerCore]
		}
		c.PerCore = make([]float64, len(per))
		for i, v := range per {
			c.PerCore[i] = clampPercent(v)
		}
	}
	if n, err := s.probes.cpuCounts(ctx); err != nil {
		s.debugOnce(ctx, "cpu.counts", err)
	} else if n > 0 {
		c.Count = n
	}
	c.Model = s.cpuModelName(ctx)
	if avg, err := s.probes.loadAvg(ctx); err != nil || avg == nil {
		s.debugOnce(ctx, "load.avg", err)
	} else {
		c.Load1 = nonNegative(avg.Load1)
		c.Load5 = nonNegative(avg.Load5)
		c.Load15 = nonNegative(avg.Load15)
	}
	return c
}

// cpuModelName returns the CPU model, cached after the first success since
// it never changes.
func (s *Sampler) cpuModelName(ctx context.Context) string {
	s.tsMu.Lock()
	cached := s.cpuModel
	s.tsMu.Unlock()
	if cached != "" {
		return cached
	}
	infos, err := s.probes.cpuInfo(ctx)
	if err != nil || len(infos) == 0 {
		s.debugOnce(ctx, "cpu.info", err)
		return ""
	}
	name := trimSpaces(infos[0].ModelName)
	if name != "" {
		s.tsMu.Lock()
		s.cpuModel = name
		s.tsMu.Unlock()
	}
	return name
}

// collectMemory fills the Memory section from virtual and swap memory.
func (s *Sampler) collectMemory(ctx context.Context) agentproto.Memory {
	var m agentproto.Memory
	if vm, err := s.probes.virtualMemory(ctx); err != nil || vm == nil {
		s.debugOnce(ctx, "mem.virtual", err)
	} else {
		m.Total = vm.Total
		m.Used = vm.Used
		m.Available = vm.Available
		m.Percent = clampPercent(vm.UsedPercent)
	}
	if sw, err := s.probes.swapMemory(ctx); err != nil || sw == nil {
		s.debugOnce(ctx, "mem.swap", err)
	} else {
		m.SwapTotal = sw.Total
		m.SwapUsed = sw.Used
	}
	return m
}

// collectDisks lists real filesystems with their usage. Pseudo, container
// and network filesystems are skipped (see KeepPartition). The root volume
// is marked Primary and listed first; the rest are sorted by mount point.
func (s *Sampler) collectDisks(ctx context.Context) []agentproto.Disk {
	parts, err := s.probes.partitions(ctx)
	if err != nil {
		s.debugOnce(ctx, "disk.partitions", err)
		if len(parts) == 0 {
			return nil
		}
	}
	seen := make(map[string]struct{}, len(parts))
	var disks []agentproto.Disk
	for _, p := range parts {
		if !KeepPartition(p.Mountpoint, p.Fstype) {
			continue
		}
		if _, dup := seen[p.Mountpoint]; dup {
			continue
		}
		seen[p.Mountpoint] = struct{}{}
		u, err := s.probes.usage(ctx, p.Mountpoint)
		if err != nil || u == nil || u.Total == 0 {
			s.debugOnce(ctx, "disk.usage:"+p.Mountpoint, err)
			continue
		}
		disks = append(disks, agentproto.Disk{
			Mount:   p.Mountpoint,
			Device:  p.Device,
			FSType:  p.Fstype,
			Total:   u.Total,
			Used:    u.Used,
			Percent: clampPercent(u.UsedPercent),
			Primary: IsPrimaryMount(p.Mountpoint),
		})
	}
	slices.SortStableFunc(disks, func(a, b agentproto.Disk) int {
		if a.Primary != b.Primary {
			if a.Primary {
				return -1
			}
			return 1
		}
		if a.Mount < b.Mount {
			return -1
		}
		if a.Mount > b.Mount {
			return 1
		}
		return 0
	})
	if len(disks) > MaxDisks {
		disks = disks[:MaxDisks]
	}
	return disks
}

// collectNet fills the Net section: cumulative per-interface counters with
// the physical heuristic applied, and the detected Tailscale interface.
func (s *Sampler) collectNet(ctx context.Context) agentproto.Net {
	var n agentproto.Net
	counters, err := s.probes.ioCounters(ctx)
	if err != nil {
		s.debugOnce(ctx, "net.iocounters", err)
	}
	if len(counters) > 0 {
		n.Interfaces = make([]agentproto.Interface, 0, len(counters))
		seen := make(map[string]struct{}, len(counters))
		for _, c := range counters {
			if c.Name == "" {
				continue
			}
			if _, dup := seen[c.Name]; dup {
				continue
			}
			seen[c.Name] = struct{}{}
			n.Interfaces = append(n.Interfaces, agentproto.Interface{
				Name:      c.Name,
				RxBytes:   c.BytesRecv,
				TxBytes:   c.BytesSent,
				RxPackets: c.PacketsRecv,
				TxPackets: c.PacketsSent,
				RxErrors:  c.Errin,
				TxErrors:  c.Errout,
				Physical:  IsPhysicalInterface(c.Name),
			})
		}
		slices.SortStableFunc(n.Interfaces, func(a, b agentproto.Interface) int {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
			return 0
		})
		if len(n.Interfaces) > MaxInterfaces {
			n.Interfaces = n.Interfaces[:MaxInterfaces]
		}
	}
	ifaces, err := s.probes.interfaces(ctx)
	if err != nil {
		s.debugOnce(ctx, "net.interfaces", err)
	}
	n.TailscaleIf = DetectTailscaleInterface(ifaceAddrs(ifaces))
	return n
}

// collectTemperatures returns sensor readings, deduplicated by sensor name,
// with zero and implausible values dropped. Errors are ignored because
// gopsutil reports partial results alongside warnings.
func (s *Sampler) collectTemperatures(ctx context.Context) []agentproto.Temperature {
	stats, err := s.probes.temperatures(ctx)
	if err != nil && len(stats) == 0 {
		s.debugOnce(ctx, "sensors.temperatures", err)
		return nil
	}
	seen := make(map[string]struct{}, len(stats))
	var out []agentproto.Temperature
	for _, t := range stats {
		key := trimSpaces(t.SensorKey)
		if key == "" || !plausibleCelsius(t.Temperature) {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		temp := agentproto.Temperature{Sensor: key, Celsius: t.Temperature}
		if plausibleCelsius(t.Critical) {
			temp.Critical = t.Critical
		}
		out = append(out, temp)
	}
	slices.SortStableFunc(out, func(a, b agentproto.Temperature) int {
		if a.Sensor < b.Sensor {
			return -1
		}
		if a.Sensor > b.Sensor {
			return 1
		}
		return 0
	})
	if len(out) > MaxTemperatures {
		out = out[:MaxTemperatures]
	}
	return out
}

// collectProcesses returns the number of processes, or 0 when unavailable.
func (s *Sampler) collectProcesses(ctx context.Context) int {
	pids, err := s.probes.pids(ctx)
	if err != nil && len(pids) == 0 {
		s.debugOnce(ctx, "process.pids", err)
		return 0
	}
	return len(pids)
}

// collectTailscale calls the installed TailscaleInfo function at most every
// TailscaleRefresh and returns a copy of the latest known value.
func (s *Sampler) collectTailscale(ctx context.Context) *agentproto.TailscaleInfo {
	s.tsMu.Lock()
	fn := s.tsFn
	due := fn != nil && !s.now().Before(s.tsNext)
	s.tsMu.Unlock()
	if fn == nil {
		return nil
	}
	if due {
		info, err := fn(ctx)
		s.tsMu.Lock()
		s.tsNext = s.now().Add(TailscaleRefresh)
		if err != nil || info == nil {
			s.tsMu.Unlock()
			s.debugOnce(ctx, "tailscale.info", err)
		} else {
			s.tsInfo = cloneTailscaleInfo(info)
			s.tsMu.Unlock()
		}
	}
	s.tsMu.Lock()
	defer s.tsMu.Unlock()
	return cloneTailscaleInfo(s.tsInfo)
}

// debugOnce logs a collector failure at debug level the first time it is
// seen for the given key; later failures are silent so a permanently
// unsupported probe does not spam the log every interval.
func (s *Sampler) debugOnce(ctx context.Context, key string, err error) {
	if _, loaded := s.logOnce.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	if err == nil {
		s.log.DebugContext(ctx, "collector returned no data", "collector", key)
		return
	}
	s.log.DebugContext(ctx, "collector failed", "collector", key, "err", err)
}

// cloneReport returns a deep copy of r.
func cloneReport(r agentproto.Report) agentproto.Report {
	r.CPU.PerCore = slices.Clone(r.CPU.PerCore)
	r.Disks = slices.Clone(r.Disks)
	r.Net.Interfaces = slices.Clone(r.Net.Interfaces)
	r.Temperatures = slices.Clone(r.Temperatures)
	r.Tailscale = cloneTailscaleInfo(r.Tailscale)
	return r
}

// cloneTailscaleInfo returns a deep copy of t, or nil.
func cloneTailscaleInfo(t *agentproto.TailscaleInfo) *agentproto.TailscaleInfo {
	if t == nil {
		return nil
	}
	c := *t
	c.IPs = slices.Clone(t.IPs)
	c.Health = slices.Clone(t.Health)
	return &c
}

// sleepCtx waits for d or until ctx is done; it reports whether the full
// duration elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// clampPercent bounds v to [0, 100] and maps NaN/Inf to 0.
func clampPercent(v float64) float64 {
	if v != v || v < 0 { // NaN or negative
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// nonNegative maps NaN, Inf and negatives to 0.
func nonNegative(v float64) float64 {
	if v != v || v < 0 || v > 1e12 {
		return 0
	}
	return v
}

// plausibleCelsius reports whether t looks like a real sensor reading:
// non-zero, finite and within a range hardware can actually report.
func plausibleCelsius(t float64) bool {
	if t != t || t == 0 {
		return false
	}
	return t > -100 && t < 300
}
