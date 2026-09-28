package demo

import "math"

// flowStep is the integration grid in seconds. Rates are evaluated at grid
// points and linearly interpolated in between, so the derivative of a
// counter is continuous and the counter itself is monotonic (rates are
// never negative).
const flowStep int64 = 60

// integrator turns a periodic, non-negative rate vector into cumulative
// counters that are a pure function of time:
//
//	G(t) = F*k + I(periodStart_k, t)
//	C(t) = max(0, G(t) - G(epoch))
//
// where k is the index of the integration period containing t, F is the
// (integer) integral of the rate over one full period, I is the trapezoid
// integral from the start of the current period to t, and epoch is the
// per-device counter origin (a few days before the simulation base time, so
// counters look lived-in without being absurd). Because the rate is
// periodic, F is the same for every period and I never spans more than one
// period, so the cost is bounded regardless of how far t is from the grid
// anchor. The per-period partial sum is cached so consecutive polls only
// integrate the cells crossed since the previous call; calls that move
// backwards in time simply recompute from the period start.
type integrator struct {
	period int64

	fullOK bool
	full   [nFlows]float64 // integral over one whole period (integer-valued)

	originOK bool
	origin   [nFlows]float64 // G(epoch)

	// forward cache for G
	ready  bool
	k      int64           // period index the cache belongs to
	cell   int64           // number of completed cells in sums
	sums   [nFlows]float64 // integral over cells [0, cell)
	r0     [nFlows]float64 // rate at the start of cell 'cell'
	r1     [nFlows]float64 // rate at the end of cell r1cell
	r1cell int64
}

// rateFunc is the analytic rate vector as a function of seconds into the
// integration period.
type rateFunc func(u float64) [nFlows]float64

// cellIntegral integrates a rate that ramps linearly from r0 to r1 over the
// first f (0..1) of a cell.
func cellIntegral(r0, r1, f float64) float64 {
	return float64(flowStep) * (r0*f + (r1-r0)*f*f/2)
}

// computeFull integrates one whole period.
func (it *integrator) computeFull(rate rateFunc) {
	nCells := it.period / flowStep
	r0 := rate(0)
	var sum [nFlows]float64
	for c := int64(0); c < nCells; c++ {
		r1 := rate(float64((c + 1) * flowStep))
		for f := range sum {
			sum[f] += cellIntegral(r0[f], r1[f], 1)
		}
		r0 = r1
	}
	for f := range sum {
		it.full[f] = math.Ceil(sum[f])
	}
	it.fullOK = true
}

// endRate returns the rate at the end of cell c, cached per cell.
func (it *integrator) endRate(c int64, rate rateFunc) [nFlows]float64 {
	if !it.ready || it.r1cell != c {
		it.r1 = rate(float64((c + 1) * flowStep))
		it.r1cell = c
	}
	return it.r1
}

// g returns G at uSec+uFrac seconds into period k together with the
// instantaneous rates, using and updating the forward cache.
func (it *integrator) g(k, uSec int64, uFrac float64, rate rateFunc) (g, rates [nFlows]float64) {
	cellIdx := uSec / flowStep
	if !it.ready || k != it.k || cellIdx < it.cell {
		it.k = k
		it.cell = 0
		it.sums = [nFlows]float64{}
		it.r0 = rate(0)
		it.r1cell = -1
		it.ready = true
	}
	for it.cell < cellIdx {
		r1 := it.endRate(it.cell, rate)
		for f := range it.sums {
			it.sums[f] += cellIntegral(it.r0[f], r1[f], 1)
		}
		it.r0 = r1
		it.cell++
	}
	r1 := it.endRate(cellIdx, rate)
	frac := (float64(uSec-cellIdx*flowStep) + uFrac) / float64(flowStep)
	for f := range g {
		// Sum the in-period part first: the partial cell is strictly below
		// the full cell for any frac < 1 and floating-point addition is
		// monotonic, so G cannot step backwards at a cell or period
		// boundary.
		total := it.sums[f] + cellIntegral(it.r0[f], r1[f], frac)
		g[f] = it.full[f]*float64(k) + total
		rates[f] = it.r0[f] + (r1[f]-it.r0[f])*frac
	}
	return g, rates
}

// gFresh computes G without touching the cache (used once for the origin).
func (it *integrator) gFresh(k, uSec int64, uFrac float64, rate rateFunc) [nFlows]float64 {
	var tmp integrator
	tmp.period = it.period
	tmp.full = it.full
	tmp.fullOK = true
	g, _ := tmp.g(k, uSec, uFrac, rate)
	return g
}

// at returns the cumulative counters and instantaneous rates at uSec+uFrac
// seconds into period k, relative to the origin at epochK/epochU/epochFrac.
func (it *integrator) at(k, uSec int64, uFrac float64, epochK, epochU int64, epochFrac float64, rate rateFunc) (counters, rates [nFlows]float64) {
	if it.period <= 0 {
		it.period = dayPeriod
	}
	if !it.fullOK {
		it.computeFull(rate)
	}
	if !it.originOK {
		it.origin = it.gFresh(epochK, epochU, epochFrac, rate)
		it.originOK = true
	}
	g, rates := it.g(k, uSec, uFrac, rate)
	for f := range counters {
		counters[f] = math.Max(0, g[f]-it.origin[f])
	}
	return counters, rates
}
