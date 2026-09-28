package demo

import (
	"math"
	"time"
)

// All simulated signals are pure functions of (seed, time). Time is handled
// in two forms:
//
//   - absolute unix seconds (float64) for metrics such as CPU, memory,
//     latency and temperature, whose noise components have arbitrary periods;
//   - "u", the number of seconds since the start of the device's current
//     integration period (24h for most devices, 120h for the block-pattern
//     laptop), for anything that feeds the bandwidth counters. Those signals
//     use only components whose period divides the integration period, so
//     the integral over one full period is identical for every period and
//     cumulative counters can be computed in bounded time (see flows.go).
//
// The period grid is anchored at anchorUnix (2026-01-01T00:00:00Z); for a
// 24h period u is therefore simply the number of seconds since midnight UTC.

const (
	// anchorUnix is 2026-01-01T00:00:00Z, the origin of the period grid.
	anchorUnix int64 = 1767225600

	dayPeriod int64 = 86400

	// Block-pattern presence (old-laptop): 6-hour blocks, 20 per 120h cycle,
	// exactly two of which are online (10%).
	blockLen       int64 = 6 * 3600
	blocksPerCycle int64 = 20
	blocksPeriod         = blockLen * blocksPerCycle // 120h

	// Flapping presence (alice-iphone): one offline window per 40 minutes.
	flapPeriod int64 = 40 * 60
	flapPerDay       = dayPeriod / flapPeriod

	// How long the "never online" device has been away.
	neverSeenFor = 4*day + 3*time.Hour + 17*time.Minute

	twoPi = 2 * math.Pi
)

// Channel identifiers: mixed into the per-device hash so every signal has
// its own independent set of phases.
const (
	chDiurnal uint64 = iota + 1
	chCPU
	chCPUSpike
	chCPUBurst
	chMem
	chDisk
	chTemp
	chLatency
	chLatSpike
	chProc
	chLoad
	chBoot
	chOnline
	chHandshake
	chDERP
	chAPISeen
	chFlowA
	chFlowB
	chFlowBurst = 60  // + flow index
	chCounter0  = 70  // + flow index
	chCore      = 100 // + core index
	chKey       = 200
)

// splitmix is the SplitMix64 finaliser: a fast, well-distributed 64-bit mixer.
func splitmix(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// mix hashes a sequence of words into one 64-bit value.
func mix(words ...uint64) uint64 {
	h := uint64(0x9E3779B97F4A7C15)
	for _, w := range words {
		h = splitmix(h ^ (w + 0x632BE59BD9B4E019 + (h << 6) + (h >> 2)))
	}
	return h
}

// unit maps a hash to a float in [0, 1).
func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// floorDiv is integer division rounding towards negative infinity.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// floorMod is the non-negative remainder of a modulo b (b > 0).
func floorMod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func unix(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// component is one sine term of a band-limited noise signal.
type component struct{ period, weight float64 }

// slowComponents have arbitrary periods and are used for absolute-time signals.
var slowComponents = []component{
	{4.7 * 3600, 1}, {1.9 * 3600, 0.8}, {41 * 60, 0.6}, {13 * 60, 0.45},
	{5.3 * 60, 0.3}, {2.1 * 60, 0.2}, {47, 0.12},
}

// periodicComponents have periods that divide 24h (and therefore 120h) and
// are used for the bandwidth rate functions.
var periodicComponents = []component{
	{8 * 3600, 1}, {3 * 3600, 0.8}, {4800, 0.6}, {1440, 0.45},
	{540, 0.3}, {240, 0.2}, {90, 0.12},
}

// noise is a smooth pseudo-random signal in [-1, 1]: a fixed set of sine
// components with phases derived once from a hash.
type noise struct {
	comps  []component
	phases []float64
	wsum   float64
}

// newNoise derives the phases for the given components from h.
func newNoise(h uint64, comps []component) noise {
	n := noise{comps: comps, phases: make([]float64, len(comps))}
	for k, c := range comps {
		n.phases[k] = twoPi * unit(mix(h, uint64(k)+1))
		n.wsum += c.weight
	}
	return n
}

// at evaluates the signal at time t (seconds).
func (n *noise) at(t float64) float64 {
	if n.wsum == 0 {
		return 0
	}
	var acc float64
	for k, c := range n.comps {
		acc += c.weight * math.Sin(twoPi*t/c.period+n.phases[k])
	}
	return acc / n.wsum
}

// sumOfSines is the one-shot form of noise for callers without a cache.
func sumOfSines(h uint64, comps []component, t float64) float64 {
	n := newNoise(h, comps)
	return n.at(t)
}

// diurnal01 is a 24-hour sinusoid mapped to [0, 1].
func diurnal01(t, phase float64) float64 {
	return 0.5 * (1 + math.Sin(twoPi*t/float64(dayPeriod)+phase))
}

// raisedCosine is a smooth bump on [0, 1] peaking at 0.5.
func raisedCosine(x float64) float64 {
	if x <= 0 || x >= 1 {
		return 0
	}
	return 0.5 * (1 - math.Cos(twoPi*x))
}

// plateau is 1 over most of [0, 1] with smooth ramps of relative width edge.
func plateau(x, edge float64) float64 {
	switch {
	case x <= 0 || x >= 1:
		return 0
	case x < edge:
		return raisedCosine(x / edge / 2)
	case x > 1-edge:
		return raisedCosine(0.5 + (x-(1-edge))/edge/2)
	default:
		return 1
	}
}

// spikeTrain returns the shape value (0..1) of a periodic train of events:
// every period seconds an event of the given width may occur at a
// hash-derived offset. Only about one in every occurrences fires (every<=1
// means all). When wrap>0 the occurrence index is taken modulo wrap so the
// train is periodic with period*wrap seconds (required for rate functions).
func spikeTrain(h uint64, t, period, width float64, every, wrap int64, shape func(float64) float64) float64 {
	off := unit(mix(h, 7)) * (period - width)
	n := math.Floor((t - off) / period)
	local := t - off - n*period
	if local >= width {
		return 0
	}
	idx := int64(n)
	if wrap > 0 {
		idx = floorMod(idx, wrap)
	}
	if every > 1 && mix(h, 11, uint64(idx))%uint64(every) != 0 {
		return 0
	}
	return shape(local / width)
}

// signals holds a device's precomputed noise generators.
type signals struct {
	diurnalPhase float64 // metrics
	flowPhase    float64 // bandwidth (period-relative)
	cpu, cpuTop  noise
	mem, disk    noise
	temp, lat    noise
	proc, load   noise
	cores        []noise
	flowA, flowB noise // rx-ish and tx-ish bandwidth noise
}

// newSignals derives every generator for a device from its hash.
func newSignals(h uint64, cores int) signals {
	s := signals{
		diurnalPhase: twoPi * unit(mix(h, chDiurnal)),
		flowPhase:    twoPi * unit(mix(h, chDiurnal, 2)),
		cpu:          newNoise(mix(h, chCPU), slowComponents),
		cpuTop:       newNoise(mix(h, chCPUSpike, 1), slowComponents),
		mem:          newNoise(mix(h, chMem), slowComponents),
		disk:         newNoise(mix(h, chDisk), slowComponents),
		temp:         newNoise(mix(h, chTemp), slowComponents),
		lat:          newNoise(mix(h, chLatency), slowComponents),
		proc:         newNoise(mix(h, chProc), slowComponents),
		load:         newNoise(mix(h, chLoad), slowComponents),
		flowA:        newNoise(mix(h, chFlowA), periodicComponents),
		flowB:        newNoise(mix(h, chFlowB), periodicComponents),
	}
	if cores < 1 {
		cores = 1
	}
	s.cores = make([]noise, cores)
	for i := range s.cores {
		s.cores[i] = newNoise(mix(h, chCore+uint64(i)), slowComponents)
	}
	return s
}

// period returns the device's integration period in seconds.
func (d *device) period() int64 {
	if d.presence == onlineBlocks {
		return blocksPeriod
	}
	return dayPeriod
}

// periodIndex splits t into the index of the device's current integration
// period, the whole seconds into it and the fractional second.
func (d *device) periodIndex(t time.Time) (k, uSec int64, uFrac float64) {
	p := d.period()
	sec := t.Unix()
	k = floorDiv(sec-anchorUnix, p)
	uSec = sec - anchorUnix - k*p
	uFrac = float64(t.Nanosecond()) / 1e9
	return
}

// --- presence -------------------------------------------------------------

// flapWindow returns the absolute [start, end) of the n-th offline window of
// the day starting at dayStart. n may be outside [0, flapPerDay) to address
// adjacent days.
func (d *device) flapWindow(dayStart, n int64) (start, end int64) {
	for n < 0 {
		n += flapPerDay
		dayStart -= dayPeriod
	}
	for n >= flapPerDay {
		n -= flapPerDay
		dayStart += dayPeriod
	}
	h := mix(d.h, chOnline, uint64(n))
	off := int64(300 + unit(h)*1500)           // 5..30 minutes into the slot
	length := int64(180 + unit(mix(h, 1))*120) // 3..5 minutes
	start = dayStart + n*flapPeriod + off
	return start, start + length
}

// onlineBlock reports whether 6-hour block b (of the 120h cycle) is online:
// exactly two distinct blocks per cycle are.
func (d *device) onlineBlock(b int64) bool {
	b = floorMod(b, blocksPerCycle)
	c1 := int64(mix(d.h, chOnline, 98) % uint64(blocksPerCycle))
	c2 := (c1 + 1 + int64(mix(d.h, chOnline, 99)%uint64(blocksPerCycle-1))) % blocksPerCycle
	return b == c1 || b == c2
}

// periodicOnline is the device's presence as a pure function of u (seconds
// into its integration period). It ignores key expiry.
func (d *device) periodicOnline(uSec int64) bool {
	switch d.presence {
	case onlineNever:
		return false
	case onlineFlap:
		n := uSec / flapPeriod
		start, end := d.flapWindow(0, n)
		return uSec < start || uSec >= end
	case onlineBlocks:
		return d.onlineBlock(uSec / blockLen)
	default:
		return true
	}
}

// presence reports whether d is online at t and when its current state
// began (the start of the current offline window, or of the current online
// stretch). s.mu must be held.
func (s *Sim) presence(d *device, t time.Time) (online bool, since time.Time) {
	if s.isExpired(d, t) {
		exp, _ := s.keyExpiry(d)
		return false, exp
	}
	sec := t.Unix()
	switch d.presence {
	case onlineNever:
		return false, s.t0.Add(-neverSeenFor)
	case onlineFlap:
		dayStart := anchorUnix + dayPeriod*floorDiv(sec-anchorUnix, dayPeriod)
		n := (sec - dayStart) / flapPeriod
		ws, we := d.flapWindow(dayStart, n)
		switch {
		case sec >= ws && sec < we:
			return false, unix(ws)
		case sec < ws:
			_, pe := d.flapWindow(dayStart, n-1)
			return true, unix(pe)
		default:
			return true, unix(we)
		}
	case onlineBlocks:
		cycleStart := anchorUnix + blocksPeriod*floorDiv(sec-anchorUnix, blocksPeriod)
		b := (sec - cycleStart) / blockLen
		if d.onlineBlock(b) {
			start := b
			for i := int64(0); i < blocksPerCycle && d.onlineBlock(start-1); i++ {
				start--
			}
			return true, unix(cycleStart + start*blockLen)
		}
		last := b - 1
		for i := int64(0); i < blocksPerCycle && !d.onlineBlock(last); i++ {
			last--
		}
		return false, unix(cycleStart + (last+1)*blockLen)
	default:
		return true, s.bootTime(d, t)
	}
}

// bootTime returns the device's last boot time at t: fixed per device until
// its reboot period elapses. The block-pattern laptop boots when it comes
// online. s.mu must be held.
func (s *Sim) bootTime(d *device, t time.Time) time.Time {
	if d.presence == onlineBlocks {
		_, since := s.presence(d, t)
		return since
	}
	r := int64(d.rebootPeriod / time.Second)
	if r <= 0 {
		r = int64(30 * day / time.Second)
	}
	phase := int64(unit(mix(d.h, chBoot)) * float64(r))
	sec := t.Unix()
	return unix(anchorUnix + phase + r*floorDiv(sec-anchorUnix-phase, r))
}

// counterEpoch is the origin of the device's cumulative counters: between
// three and ten days before the simulation base time, so counters look
// lived-in from the first poll and a 24h backfill never reaches zero.
// s.mu must be held.
func (s *Sim) counterEpoch(d *device) time.Time {
	back := time.Duration((3 + 7*unit(mix(d.h, chCounter0))) * float64(day))
	return s.t0.Add(-back).Truncate(time.Second)
}

// lastHandshake returns a handshake time within the last two minutes that
// only moves forward as t advances.
func (d *device) lastHandshake(t time.Time) time.Time {
	const window = 120
	sec := t.Unix()
	base := sec - floorMod(sec, window)
	hs := base + int64(unit(mix(d.h, chHandshake))*window)
	if hs > sec {
		hs -= window
	}
	return unix(hs)
}

// --- per-instant metrics ---------------------------------------------------

// instant is everything the simulation knows about one device at one time.
type instant struct {
	online  bool
	since   time.Time // start of the current online/offline state
	expired bool
	boot    time.Time
	uptime  uint64
	active  bool

	cpu     float64
	cpuSlow float64
	perCore []float64
	load1   float64
	load5   float64
	load15  float64
	mem     float64
	disk    float64
	temp    float64
	latency float64
	procs   int

	counters [nFlows]float64 // cumulative bytes
	rates    [nFlows]float64 // bytes/s
}

// instantAt computes the device state at t. perCore values are only filled
// when full is set (they are relatively expensive and only agent reports
// need them). s.mu must be held.
func (s *Sim) instantAt(d *device, t time.Time, full bool) instant {
	var in instant
	ft := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	p := &d.profile
	sg := &d.sig

	in.online, in.since = s.presence(d, t)
	in.expired = s.isExpired(d, t)
	in.boot = s.bootTime(d, t)
	if in.boot.After(t) {
		in.boot = t
	}
	in.uptime = uint64(t.Sub(in.boot) / time.Second)

	dn := diurnal01(ft, sg.diurnalPhase)

	// CPU: diurnal + noise + occasional short bursts (+ the spiky plateau).
	in.cpuSlow = p.cpuBase + p.cpuAmp*dn
	cpu := in.cpuSlow + 0.35*(p.cpuAmp+2)*sg.cpu.at(ft)
	cpu += 30 * spikeTrain(mix(d.h, chCPUBurst), ft, 37*60, 150, 3, 0, raisedCosine)
	if p.cpuSpiky {
		sh := spikeTrain(mix(d.h, chCPUSpike), ft, 2*3600, 12*60, 1, 0, func(x float64) float64 { return plateau(x, 0.04) })
		target := 95 + 3*sg.cpuTop.at(ft)
		cpu += (target - cpu) * sh
	}
	in.cpu = clamp(cpu, 0.3, 100)

	cores := p.cores
	if cores <= 0 {
		cores = 1
	}
	if full && p.agent != agentNone {
		in.perCore = make([]float64, cores)
		for i := range in.perCore {
			v := in.cpu + 18*sg.cores[i].at(ft)
			in.perCore[i] = math.Round(clamp(v, 0, 100)*10) / 10
		}
	}
	loadNoise := sg.load.at(ft)
	in.load1 = math.Max(0, float64(cores)*in.cpu/100*0.9+0.15*loadNoise)
	in.load5 = math.Max(0, float64(cores)*in.cpuSlow/100*0.85+0.08*loadNoise)
	in.load15 = math.Max(0, float64(cores)*in.cpuSlow/100*0.8)

	in.mem = clamp(p.memPct+1.5*dn+3*sg.mem.at(ft), 1, 99.5)
	in.disk = clamp(d.primaryDisk().pct+0.15*sg.disk.at(ft/20), 0, 100)
	if d.hasSensors() {
		in.temp = p.tempBase + 0.12*in.cpu + 1.5*sg.temp.at(ft)
	}
	if p.latency > 0 {
		lat := p.latency * (1 + 0.2*sg.lat.at(ft))
		lat += p.latency * 1.5 * spikeTrain(mix(d.h, chLatSpike), ft, 7*60, 25, 4, 0, raisedCosine)
		in.latency = math.Max(0.4, lat)
	}
	in.procs = p.procs + int(float64(p.procs)*(0.08*sg.proc.at(ft)+0.05*dn))

	k, uSec, uFrac := d.periodIndex(t)
	ek, eu, ef := d.periodIndex(s.counterEpoch(d))
	in.counters, in.rates = d.flows.at(k, uSec, uFrac, ek, eu, ef, d.rateVector)
	in.active = in.online && (in.rates[flowPeerRx]+in.rates[flowPeerTx]) > 0.45*(p.flows[flowPeerRx]+p.flows[flowPeerTx])
	return in
}

// flowMix gives each flow its blend of the two shared bandwidth noises so
// related flows (tailnet traffic is part of physical traffic) correlate.
var flowMix = [nFlows][2]float64{
	flowPeerRx: {1, 0},
	flowPeerTx: {0, 1},
	flowPhysRx: {0.7, 0.3},
	flowPhysTx: {0.3, 0.7},
	flowTSRx:   {0.85, 0.15},
	flowTSTx:   {0.15, 0.85},
}

// rateVector is the analytic bandwidth rate (bytes/s) for every flow at u
// seconds into the integration period. Every component is periodic in the
// device's integration period, and every value is non-negative.
func (d *device) rateVector(u float64) [nFlows]float64 {
	var r [nFlows]float64
	p := &d.profile
	uInt := int64(math.Floor(u))
	on := d.periodicOnline(uInt)
	dn := diurnal01(u, d.sig.flowPhase)
	nA, nB := d.sig.flowA.at(u), d.sig.flowB.at(u)
	wrapHour := d.period() / 3600
	var backup float64
	if p.backup {
		// Nightly backup to the hub: 02:00-02:45 UTC.
		local := floorMod(uInt-2*3600, dayPeriod)
		if local < 45*60 {
			backup = 3e6 * raisedCosine((float64(local)+(u-math.Floor(u)))/(45*60))
		}
	}
	for f := 0; f < nFlows; f++ {
		base := p.flows[f]
		if base <= 0 {
			continue
		}
		n := flowMix[f][0]*nA + flowMix[f][1]*nB
		v := base * (0.35 + 0.9*dn) * (1 + 0.45*n)
		v += base * 3 * spikeTrain(mix(d.h, chFlowBurst+uint64(f)), u, 3600, 240, 3, wrapHour, raisedCosine)
		if f == flowPeerRx || f == flowTSRx || f == flowPhysTx {
			v += backup
		}
		if !on && f != flowPhysRx && f != flowPhysTx {
			v = 0
		}
		r[f] = math.Max(0, v)
	}
	return r
}
