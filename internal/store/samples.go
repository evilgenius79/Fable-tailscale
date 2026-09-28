package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// sampleColumns lists the samples table columns in insert/select order.
const sampleColumns = `device_id, ts, online, latency_ms, relay, direct, ts_rx_bytes, ts_tx_bytes,
	ts_rx_rate, ts_tx_rate, agent_ok, cpu, mem, disk, load1, net_rx_rate, net_tx_rate, temp_c, uptime_s`

// maxSparklineBuckets bounds the size of the arrays NetworkSparklines
// allocates for a single call.
const maxSparklineBuckets = 100_000

// InsertSamples writes a batch of raw samples in one transaction. A sample
// with the same (device, second) as an existing row replaces it. Timestamps
// are truncated to whole seconds; a zero timestamp becomes now.
func (s *Store) InsertSamples(ctx context.Context, samples []model.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	for i := range samples {
		if samples[i].DeviceID == "" {
			return fmt.Errorf("store: insert samples: sample %d has empty device id", i)
		}
	}
	now := s.now()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO samples(`+sampleColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("store: prepare insert sample: %w", err)
		}
		defer stmt.Close()
		for i := range samples {
			sm := &samples[i]
			ts := sm.TS
			if ts.IsZero() {
				ts = now
			}
			var uptime sql.NullInt64
			if sm.Uptime != nil {
				uptime = sql.NullInt64{Int64: clampUint64(*sm.Uptime), Valid: true}
			}
			if _, err := stmt.ExecContext(ctx,
				string(sm.DeviceID), ts.Unix(), boolInt(sm.Online), nullFloat(sm.LatencyMs), nullString(sm.Relay), nullBool(sm.Direct),
				sm.TSRxBytes, sm.TSTxBytes, sm.TSRxRate, sm.TSTxRate, boolInt(sm.AgentOK),
				nullFloat(sm.CPU), nullFloat(sm.Mem), nullFloat(sm.Disk), nullFloat(sm.Load1),
				nullFloat(sm.NetRxRate), nullFloat(sm.NetTxRate), nullFloat(sm.TempC), uptime,
			); err != nil {
				return fmt.Errorf("store: insert sample %s@%d: %w", sm.DeviceID, ts.Unix(), err)
			}
		}
		return nil
	})
}

func clampUint64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// LatestSample returns the most recent raw sample for a device, or
// ErrNotFound when the device has no samples.
func (s *Store) LatestSample(ctx context.Context, id model.DeviceID) (*model.Sample, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sampleColumns+` FROM samples WHERE device_id = ? ORDER BY ts DESC LIMIT 1`, string(id))
	sm, err := scanSample(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: latest sample %s: %w", id, err)
	}
	return sm, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSample(sc scanner) (*model.Sample, error) {
	var (
		sm                                          model.Sample
		id                                          string
		ts, online, agentOK                         int64
		latency, cpu, mem, disk, load1, nrx, ntx, t sql.NullFloat64
		relay                                       sql.NullString
		direct, uptime                              sql.NullInt64
	)
	if err := sc.Scan(&id, &ts, &online, &latency, &relay, &direct, &sm.TSRxBytes, &sm.TSTxBytes,
		&sm.TSRxRate, &sm.TSTxRate, &agentOK, &cpu, &mem, &disk, &load1, &nrx, &ntx, &t, &uptime); err != nil {
		return nil, err
	}
	sm.DeviceID = model.DeviceID(id)
	sm.TS = fromUnix(ts)
	sm.Online = online != 0
	sm.LatencyMs = floatPtr(latency)
	sm.Relay = relay.String
	sm.Direct = boolPtr(direct)
	sm.AgentOK = agentOK != 0
	sm.CPU = floatPtr(cpu)
	sm.Mem = floatPtr(mem)
	sm.Disk = floatPtr(disk)
	sm.Load1 = floatPtr(load1)
	sm.NetRxRate = floatPtr(nrx)
	sm.NetTxRate = floatPtr(ntx)
	sm.TempC = floatPtr(t)
	if uptime.Valid && uptime.Int64 >= 0 {
		u := uint64(uptime.Int64)
		sm.Uptime = &u
	}
	return &sm, nil
}

// --- series ---------------------------------------------------------------

const (
	// SourceRaw marks a Series built from raw per-tick samples.
	SourceRaw = "raw"
	// SourceRollup marks a Series built from 5-minute rollups.
	SourceRollup = "rollup"
)

// seriesPlan describes which table and bucket width serve a query range.
type seriesPlan struct {
	source string
	step   int64 // seconds
	bucket bool  // false = return raw samples as-is
}

// planSeries picks the source and step for a range per docs/API.md:
//
//	<= 3h   raw samples at native resolution (15s, no bucketing)
//	<= 24h  raw samples bucketed to 60s
//	<= 48h  raw samples bucketed to 300s
//	> 48h   rollups: 300s (<= 7d), 1800s (<= 14d), 3600s (> 14d)
func planSeries(rng time.Duration) seriesPlan {
	const day = 24 * time.Hour
	switch {
	case rng <= 3*time.Hour:
		return seriesPlan{source: SourceRaw, step: rawIntervalSec, bucket: false}
	case rng <= day:
		return seriesPlan{source: SourceRaw, step: 60, bucket: true}
	case rng <= 2*day:
		return seriesPlan{source: SourceRaw, step: 300, bucket: true}
	case rng <= 7*day:
		return seriesPlan{source: SourceRollup, step: 300, bucket: true}
	case rng <= 14*day:
		return seriesPlan{source: SourceRollup, step: 1800, bucket: true}
	default:
		return seriesPlan{source: SourceRollup, step: 3600, bucket: true}
	}
}

// QuerySeries returns the time series of a device between from and to
// (inclusive). The source table and step are chosen from the range length
// (see planSeries). Bucketed points start at floor(ts/step)*step; averages
// ignore null values; Online and Direct are ratios in 0..1. Points are sorted
// by time and the slice is never nil.
//
// A rollup-sourced series also covers the newest part of the range that has
// not been rolled up yet (rollups lag behind by at least an hour): raw
// samples at or after the device's rollup boundary (see rollupBoundary) are
// aggregated into 5-minute buckets on the fly and re-bucketed to the step
// together with the stored rollups, so the series always extends to the
// newest sample.
func (s *Store) QuerySeries(ctx context.Context, id model.DeviceID, from, to time.Time) (*model.Series, error) {
	plan := planSeries(to.Sub(from))
	out := &model.Series{
		DeviceID: id,
		From:     from,
		To:       to,
		StepSec:  plan.step,
		Source:   plan.source,
		Points:   []model.SeriesPoint{},
	}
	if id == "" || !to.After(from) {
		return out, nil
	}
	fromU, toU := from.Unix(), to.Unix()

	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case plan.source == SourceRaw && !plan.bucket:
		rows, err = s.db.QueryContext(ctx, `SELECT ts, 1, online, direct, latency_ms, NULL, ts_rx_rate, ts_tx_rate,
			cpu, NULL, mem, disk, load1, net_rx_rate, net_tx_rate, temp_c
			FROM samples WHERE device_id = ? AND ts >= ? AND ts <= ? ORDER BY ts`, string(id), fromU, toU)
	case plan.source == SourceRaw:
		rows, err = s.db.QueryContext(ctx, `SELECT (ts / ?1) * ?1 AS bucket, COUNT(*), AVG(online), AVG(direct),
			AVG(latency_ms), MAX(latency_ms), AVG(ts_rx_rate), AVG(ts_tx_rate),
			AVG(cpu), MAX(cpu), AVG(mem), AVG(disk), AVG(load1), AVG(net_rx_rate), AVG(net_tx_rate), AVG(temp_c)
			FROM samples WHERE device_id = ?2 AND ts >= ?3 AND ts <= ?4
			GROUP BY bucket ORDER BY bucket`, plan.step, string(id), fromU, toU)
	default:
		// Stored rollups serve the range up to the device's rollup boundary;
		// raw samples from the boundary on are aggregated into 300s buckets
		// of the same shape. Both are then re-bucketed to the requested
		// step, weighting each average by the number of raw samples behind
		// it. The two halves are disjoint so no sample is counted twice.
		boundary, berr := s.rollupBoundary(ctx, id, fromU)
		if berr != nil {
			return nil, fmt.Errorf("store: query series %s: %w", id, berr)
		}
		rows, err = s.db.QueryContext(ctx, `SELECT (bucket / ?1) * ?1 AS b, SUM(samples),
			SUM(online_ratio * samples) / SUM(samples),
			`+weightedAvg("direct_ratio")+`, `+weightedAvg("latency_avg")+`, MAX(latency_max),
			SUM(ts_rx_rate_avg * samples) / SUM(samples), SUM(ts_tx_rate_avg * samples) / SUM(samples),
			`+weightedAvg("cpu_avg")+`, MAX(cpu_max), `+weightedAvg("mem_avg")+`, `+weightedAvg("disk_avg")+`,
			`+weightedAvg("load1_avg")+`, `+weightedAvg("net_rx_rate_avg")+`, `+weightedAvg("net_tx_rate_avg")+`,
			`+weightedAvg("temp_avg")+`
			FROM (
				SELECT bucket, samples, online_ratio, direct_ratio, latency_avg, latency_max,
					ts_rx_rate_avg, ts_tx_rate_avg, cpu_avg, cpu_max, mem_avg, disk_avg, load1_avg,
					net_rx_rate_avg, net_tx_rate_avg, temp_avg
				FROM rollups WHERE device_id = ?2 AND bucket >= ?3 AND bucket <= ?4 AND bucket < ?5
				UNION ALL
				SELECT (ts / ?6) * ?6 AS bucket, COUNT(*), AVG(online), AVG(direct), AVG(latency_ms), MAX(latency_ms),
					AVG(ts_rx_rate), AVG(ts_tx_rate), AVG(cpu), MAX(cpu), AVG(mem), AVG(disk), AVG(load1),
					AVG(net_rx_rate), AVG(net_tx_rate), AVG(temp_c)
				FROM samples WHERE device_id = ?2 AND ts >= MAX(?3, ?5) AND ts <= ?4
				GROUP BY bucket
			)
			GROUP BY b ORDER BY b`, plan.step, string(id), fromU, toU, boundary, rollupStepSec)
	}
	if err != nil {
		return nil, fmt.Errorf("store: query series %s: %w", id, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			p                             model.SeriesPoint
			n                             int64
			online                        sql.NullFloat64
			direct, lat, latMax           sql.NullFloat64
			rx, tx                        sql.NullFloat64
			cpu, cpuMax, mem, disk, load1 sql.NullFloat64
			nrx, ntx, temp                sql.NullFloat64
		)
		if err := rows.Scan(&p.T, &n, &online, &direct, &lat, &latMax, &rx, &tx,
			&cpu, &cpuMax, &mem, &disk, &load1, &nrx, &ntx, &temp); err != nil {
			return nil, fmt.Errorf("store: query series %s: scan: %w", id, err)
		}
		p.Online = online.Float64
		p.Direct = floatPtr(direct)
		p.LatencyMs = floatPtr(lat)
		p.LatencyMax = floatPtr(latMax)
		p.TSRxRate = rx.Float64
		p.TSTxRate = tx.Float64
		p.CPU = floatPtr(cpu)
		p.CPUMax = floatPtr(cpuMax)
		p.Mem = floatPtr(mem)
		p.Disk = floatPtr(disk)
		p.Load1 = floatPtr(load1)
		p.NetRxRate = floatPtr(nrx)
		p.NetTxRate = floatPtr(ntx)
		p.TempC = floatPtr(temp)
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query series %s: %w", id, err)
	}
	return out, nil
}

// rollupBoundary returns the unix second from which raw samples, rather than
// stored rollups, serve a rollup-sourced series for a device: the earlier of
// the global rollup watermark (the start of the bucket Rollup will recompute
// next, see Rollup) and the end of the device's newest rollup bucket. Rollups
// strictly before the boundary are complete; every sample at or after it is
// still raw. from is returned when the device has no rollups at all, so the
// whole range is served from raw samples.
func (s *Store) rollupBoundary(ctx context.Context, id model.DeviceID, from int64) (int64, error) {
	var newest int64
	err := s.db.QueryRowContext(ctx, `SELECT bucket + step FROM rollups WHERE device_id = ? ORDER BY bucket DESC LIMIT 1`, string(id)).Scan(&newest)
	if errors.Is(err, sql.ErrNoRows) {
		return from, nil
	}
	if err != nil {
		return 0, fmt.Errorf("rollup boundary: %w", err)
	}
	wm, ok, err := s.GetKV(ctx, kvRollupWatermark)
	if err != nil {
		return 0, fmt.Errorf("rollup boundary: %w", err)
	}
	if ok {
		if parsed, err := strconv.ParseInt(wm, 10, 64); err == nil && parsed >= 0 && parsed < newest {
			newest = parsed
		}
	}
	return newest, nil
}

// weightedAvg returns the SQL for a samples-weighted average of a nullable
// rollup column, considering only rows where the column is non-null. col is
// always one of the fixed rollup column names.
func weightedAvg(col string) string {
	return `SUM(CASE WHEN ` + col + ` IS NOT NULL THEN ` + col + ` * samples END) / ` +
		`SUM(CASE WHEN ` + col + ` IS NOT NULL THEN samples END)`
}

// --- sparklines -----------------------------------------------------------

// NetworkSparklines returns network-wide series for the dashboard with one
// entry per step-sized bucket covering [from, to). Buckets start at
// floor(t/step)*step. Online is the number of distinct devices with at least
// one online sample in the bucket, RxRate/TxRate are the sum over devices of
// the per-device average hub<->peer rates, and Latency is the average
// latency over online samples (0 when there are none). Every bucket is
// present so the arrays align; buckets without data hold zeros.
func (s *Store) NetworkSparklines(ctx context.Context, from, to time.Time, step time.Duration) (*model.OverviewSparklines, error) {
	if step <= 0 {
		step = time.Minute
	}
	stepSec := int64(step / time.Second)
	if stepSec < 1 {
		stepSec = 1
	}
	out := &model.OverviewSparklines{
		T: []int64{}, Online: []float64{}, RxRate: []float64{}, TxRate: []float64{}, Latency: []float64{},
	}
	if !to.After(from) {
		return out, nil
	}
	fromU, toU := from.Unix(), to.Unix()
	start := (fromU / stepSec) * stepSec
	if fromU < 0 && fromU%stepSec != 0 {
		start -= stepSec // floor for negative values (defensive; unix seconds are positive)
	}
	nb := (toU - start + stepSec - 1) / stepSec
	if nb <= 0 {
		return out, nil
	}
	if nb > maxSparklineBuckets {
		return nil, fmt.Errorf("store: sparklines: %d buckets exceeds limit of %d", nb, maxSparklineBuckets)
	}
	n := int(nb)
	out.T = make([]int64, n)
	out.Online = make([]float64, n)
	out.RxRate = make([]float64, n)
	out.TxRate = make([]float64, n)
	out.Latency = make([]float64, n)
	for i := range out.T {
		out.T[i] = start + int64(i)*stepSec
	}

	rows, err := s.db.QueryContext(ctx, `SELECT b, SUM(on_max), SUM(rx_avg), SUM(tx_avg), SUM(lat_sum), SUM(lat_n)
		FROM (
			SELECT (ts / ?1) * ?1 AS b, device_id,
				MAX(online) AS on_max,
				AVG(ts_rx_rate) AS rx_avg,
				AVG(ts_tx_rate) AS tx_avg,
				SUM(CASE WHEN online = 1 THEN latency_ms END) AS lat_sum,
				COUNT(CASE WHEN online = 1 THEN latency_ms END) AS lat_n
			FROM samples WHERE ts >= ?2 AND ts < ?3
			GROUP BY b, device_id
		)
		GROUP BY b ORDER BY b`, stepSec, fromU, toU)
	if err != nil {
		return nil, fmt.Errorf("store: sparklines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			b              int64
			online, rx, tx sql.NullFloat64
			latSum         sql.NullFloat64
			latN           sql.NullInt64
		)
		if err := rows.Scan(&b, &online, &rx, &tx, &latSum, &latN); err != nil {
			return nil, fmt.Errorf("store: sparklines: scan: %w", err)
		}
		i := (b - start) / stepSec
		if i < 0 || i >= nb {
			continue
		}
		out.Online[i] = online.Float64
		out.RxRate[i] = rx.Float64
		out.TxRate[i] = tx.Float64
		if latN.Valid && latN.Int64 > 0 && latSum.Valid {
			out.Latency[i] = latSum.Float64 / float64(latN.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: sparklines: %w", err)
	}
	return out, nil
}

// --- rollup / prune -------------------------------------------------------

// Rollup aggregates raw samples with ts < olderThan into 5-minute buckets in
// the rollups table and returns the number of buckets written. It is
// idempotent: re-running over the same samples produces identical rows, and a
// bucket is only replaced when the new aggregate covers at least as many
// samples as the stored one (so a partially pruned bucket never degrades an
// existing rollup). A watermark in kv ("rollup_watermark") skips samples that
// were already rolled up; deleting it simply causes a full re-scan.
func (s *Store) Rollup(ctx context.Context, olderThan time.Time) (int, error) {
	end := olderThan.Unix()
	var written int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		start, _, err := s.rollupWatermarkTx(ctx, tx)
		if err != nil {
			return fmt.Errorf("store: rollup: read watermark: %w", err)
		}
		if start > end {
			// The clock went backwards since the watermark was written (a
			// restored snapshot, an RTC corrected by NTP). Waiting for real
			// time to catch up would leave every sample until then un-rolled
			// while Prune keeps deleting them, so rescan from the beginning;
			// the ON CONFLICT guard keeps the existing rollups intact and
			// the corrected watermark is written below.
			s.log.Warn("store: rollup: watermark is ahead of the rollup horizon; rescanning all samples",
				"watermark", time.Unix(start, 0).UTC().Format(time.RFC3339), "older_than", olderThan.UTC().Format(time.RFC3339))
			start = 0
		}
		if end <= start {
			return nil
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO rollups(device_id, bucket, step, samples, online_ratio, direct_ratio,
				latency_avg, latency_max, ts_rx_rate_avg, ts_tx_rate_avg, cpu_avg, cpu_max, mem_avg, disk_avg,
				load1_avg, net_rx_rate_avg, net_tx_rate_avg, temp_avg)
			SELECT device_id, (ts / ?1) * ?1 AS bucket, ?1, COUNT(*), AVG(online), AVG(direct),
				AVG(latency_ms), MAX(latency_ms), AVG(ts_rx_rate), AVG(ts_tx_rate), AVG(cpu), MAX(cpu), AVG(mem), AVG(disk),
				AVG(load1), AVG(net_rx_rate), AVG(net_tx_rate), AVG(temp_c)
			FROM samples WHERE ts >= ?2 AND ts < ?3
			GROUP BY device_id, bucket
			ON CONFLICT(device_id, bucket) DO UPDATE SET
				step = excluded.step, samples = excluded.samples, online_ratio = excluded.online_ratio,
				direct_ratio = excluded.direct_ratio, latency_avg = excluded.latency_avg, latency_max = excluded.latency_max,
				ts_rx_rate_avg = excluded.ts_rx_rate_avg, ts_tx_rate_avg = excluded.ts_tx_rate_avg,
				cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max, mem_avg = excluded.mem_avg, disk_avg = excluded.disk_avg,
				load1_avg = excluded.load1_avg, net_rx_rate_avg = excluded.net_rx_rate_avg,
				net_tx_rate_avg = excluded.net_tx_rate_avg, temp_avg = excluded.temp_avg
			WHERE excluded.samples >= rollups.samples`, rollupStepSec, start, end)
		if err != nil {
			return fmt.Errorf("store: rollup: aggregate: %w", err)
		}
		written, err = res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: rollup: rows affected: %w", err)
		}
		// The last bucket may be partial (end is rarely bucket-aligned), so
		// the watermark points at its start and it is recomputed next time.
		// Prune never deletes samples at or after the watermark, so the
		// recomputation always sees the whole bucket.
		watermark := (end / rollupStepSec) * rollupStepSec
		if watermark > start {
			if err := setKVTx(ctx, tx, kvRollupWatermark, strconv.FormatInt(watermark, 10)); err != nil {
				return fmt.Errorf("store: rollup: write watermark: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if written > 0 {
		s.log.Debug("store: rollup complete", "buckets", written, "older_than", olderThan.UTC().Format(time.RFC3339))
	}
	return int(written), nil
}

// Prune deletes raw samples older than now-rawRetention, rollups older than
// now-rollupRetention, and events and resolved alerts older than
// now-eventRetention. A retention <= 0 disables pruning for that category.
// Raw samples at or after the rollup watermark are kept regardless of
// retention: they have not been (completely) rolled up yet, and deleting them
// would leave the next Rollup with a fraction of a bucket. When more than
// 100k rows were removed the database is compacted with VACUUM (failures are
// logged, not returned).
func (s *Store) Prune(ctx context.Context, rawRetention, rollupRetention, eventRetention time.Duration) (PruneResult, error) {
	now := s.now()
	var res PruneResult
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		del := func(dst *int64, query string, retention time.Duration) error {
			if retention <= 0 {
				return nil
			}
			r, err := tx.ExecContext(ctx, query, now.Add(-retention).Unix())
			if err != nil {
				return err
			}
			n, err := r.RowsAffected()
			if err != nil {
				return err
			}
			*dst = n
			return nil
		}
		if rawRetention > 0 {
			cutoff := now.Add(-rawRetention).Unix()
			if wm, ok, err := s.rollupWatermarkTx(ctx, tx); err != nil {
				return fmt.Errorf("store: prune samples: %w", err)
			} else if ok && wm < cutoff {
				cutoff = wm
			}
			r, err := tx.ExecContext(ctx, `DELETE FROM samples WHERE ts < ?`, cutoff)
			if err != nil {
				return fmt.Errorf("store: prune samples: %w", err)
			}
			if res.Samples, err = r.RowsAffected(); err != nil {
				return fmt.Errorf("store: prune samples: %w", err)
			}
		}
		if err := del(&res.Rollups, `DELETE FROM rollups WHERE bucket < ?`, rollupRetention); err != nil {
			return fmt.Errorf("store: prune rollups: %w", err)
		}
		if err := del(&res.Events, `DELETE FROM events WHERE ts < ?`, eventRetention); err != nil {
			return fmt.Errorf("store: prune events: %w", err)
		}
		if err := del(&res.Alerts, `DELETE FROM alerts WHERE state = 'resolved' AND COALESCE(resolved_at, updated_at) < ?`, eventRetention); err != nil {
			return fmt.Errorf("store: prune alerts: %w", err)
		}
		return nil
	})
	if err != nil {
		return PruneResult{}, err
	}
	if res.Total() > 0 {
		s.log.Info("store: pruned rows", "samples", res.Samples, "rollups", res.Rollups, "events", res.Events, "alerts", res.Alerts)
	}
	if res.Total() > vacuumThreshold {
		s.vacuum(ctx)
	}
	return res, nil
}

// rollupWatermarkTx reads the rollup watermark inside tx. ok is false when no
// watermark is stored or the stored value is malformed (the latter is logged
// and otherwise ignored). A negative value is clamped to 0.
func (s *Store) rollupWatermarkTx(ctx context.Context, tx *sql.Tx) (wm int64, ok bool, err error) {
	v, ok, err := getKVTx(ctx, tx, kvRollupWatermark)
	if err != nil || !ok {
		return 0, false, err
	}
	parsed, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		s.log.Warn("store: ignoring malformed rollup watermark", "value", v)
		return 0, false, nil
	}
	if parsed < 0 {
		parsed = 0
	}
	return parsed, true, nil
}

// vacuum compacts the database file. It holds the write mutex so no writer
// races with it; readers are unaffected in WAL mode.
func (s *Store) vacuum(ctx context.Context) {
	if s.path == "" {
		return
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	started := s.now()
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		s.log.Warn("store: vacuum failed", "err", err)
		return
	}
	s.log.Info("store: vacuum complete", "duration", time.Since(started).Round(time.Millisecond))
}

// --- uptime ---------------------------------------------------------------

// UptimeRatios returns, per device, the fraction (0..1) of samples since the
// given time in which the device was online. Rollup buckets are used for the
// period before a device's oldest raw sample, weighted by the number of raw
// samples they summarize; raw samples cover the rest. Devices without any
// data in the window are absent from the map.
func (s *Store) UptimeRatios(ctx context.Context, since time.Time) (map[model.DeviceID]float64, error) {
	type acc struct{ n, online float64 }
	sums := map[model.DeviceID]*acc{}
	sinceU := since.Unix()

	rows, err := s.db.QueryContext(ctx, `SELECT device_id, COUNT(*), SUM(online) FROM samples WHERE ts >= ? GROUP BY device_id`, sinceU)
	if err != nil {
		return nil, fmt.Errorf("store: uptime ratios: raw: %w", err)
	}
	for rows.Next() {
		var (
			id        string
			n, online float64
		)
		if err := rows.Scan(&id, &n, &online); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: uptime ratios: raw scan: %w", err)
		}
		sums[model.DeviceID(id)] = &acc{n: n, online: online}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: uptime ratios: raw: %w", err)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `WITH raw AS (
			SELECT device_id, MIN(ts) AS min_ts FROM samples WHERE ts >= ?1 GROUP BY device_id
		)
		SELECT r.device_id, SUM(r.samples), SUM(r.online_ratio * r.samples)
		FROM rollups r LEFT JOIN raw ON raw.device_id = r.device_id
		WHERE r.bucket >= ?1 AND (raw.min_ts IS NULL OR r.bucket + r.step <= raw.min_ts)
		GROUP BY r.device_id`, sinceU)
	if err != nil {
		return nil, fmt.Errorf("store: uptime ratios: rollups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id        string
			n, online sql.NullFloat64
		)
		if err := rows.Scan(&id, &n, &online); err != nil {
			return nil, fmt.Errorf("store: uptime ratios: rollup scan: %w", err)
		}
		if !n.Valid || n.Float64 <= 0 {
			continue
		}
		a := sums[model.DeviceID(id)]
		if a == nil {
			a = &acc{}
			sums[model.DeviceID(id)] = a
		}
		a.n += n.Float64
		a.online += online.Float64
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: uptime ratios: rollups: %w", err)
	}

	out := make(map[model.DeviceID]float64, len(sums))
	for id, a := range sums {
		if a.n <= 0 {
			continue
		}
		out[id] = clamp01(a.online / a.n)
	}
	return out, nil
}

func clamp01(v float64) float64 {
	switch {
	case v < 0 || math.IsNaN(v):
		return 0
	case v > 1:
		return 1
	}
	return v
}

// tlPoint is one observation on the online timeline.
type tlPoint struct {
	t        int64 // unix seconds
	online   bool
	interval int64 // expected spacing to the next observation, seconds
}

// OnlineTimeline builds an online/offline timeline for a device between from
// and to. Raw samples are used wherever they exist in the range; rollup
// buckets (online_ratio >= 0.5 counts as online) fill the part of the range
// before the oldest raw sample. A gap between observations longer than three
// times the sampling interval (15s raw, 300s rollups) counts as offline.
// Adjacent segments with the same state are merged, Pct is the online share
// (0..100) of the covered time and Outages is the number of offline
// segments. The Segments slice is never nil.
func (s *Store) OnlineTimeline(ctx context.Context, id model.DeviceID, from, to time.Time) (*model.UptimeReport, error) {
	rep := &model.UptimeReport{DeviceID: id, From: from, To: to, Segments: []model.TimelineSegment{}}
	if id == "" || !to.After(from) {
		return rep, nil
	}
	fromU, toU := from.Unix(), to.Unix()

	var raw []tlPoint
	rows, err := s.db.QueryContext(ctx, `SELECT ts, online FROM samples WHERE device_id = ? AND ts >= ? AND ts <= ? ORDER BY ts`,
		string(id), fromU, toU)
	if err != nil {
		return nil, fmt.Errorf("store: timeline %s: raw: %w", id, err)
	}
	for rows.Next() {
		var p tlPoint
		var online int64
		if err := rows.Scan(&p.t, &online); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: timeline %s: raw scan: %w", id, err)
		}
		p.online = online != 0
		p.interval = rawIntervalSec
		raw = append(raw, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: timeline %s: raw: %w", id, err)
	}
	rows.Close()

	// Rollups only for the part of the range before the oldest raw sample.
	rollupEnd := toU + 1
	if len(raw) > 0 {
		rollupEnd = raw[0].t
	}
	var points []tlPoint
	rows, err = s.db.QueryContext(ctx, `SELECT bucket, step, online_ratio FROM rollups
		WHERE device_id = ? AND bucket >= ? AND bucket <= ? AND bucket + step <= ? ORDER BY bucket`,
		string(id), fromU, toU, rollupEnd)
	if err != nil {
		return nil, fmt.Errorf("store: timeline %s: rollups: %w", id, err)
	}
	for rows.Next() {
		var (
			p     tlPoint
			step  int64
			ratio float64
		)
		if err := rows.Scan(&p.t, &step, &ratio); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: timeline %s: rollup scan: %w", id, err)
		}
		if step <= 0 {
			step = rollupStepSec
		}
		p.interval = step
		p.online = ratio >= 0.5
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: timeline %s: rollups: %w", id, err)
	}
	rows.Close()
	points = append(points, raw...)

	segs, onlineSec, coveredSec := buildTimeline(points, toU)
	rep.Segments = segs
	if coveredSec > 0 {
		rep.Pct = 100 * float64(onlineSec) / float64(coveredSec)
	}
	for _, sg := range segs {
		if !sg.Online {
			rep.Outages++
		}
	}
	return rep, nil
}

// buildTimeline turns sorted observations into merged segments ending at
// end. Each observation covers the time until the next one unless the gap
// exceeds 3x its interval, in which case it covers one interval and the rest
// of the gap is offline. It returns the segments plus the online and total
// covered seconds.
func buildTimeline(points []tlPoint, end int64) ([]model.TimelineSegment, int64, int64) {
	segs := []model.TimelineSegment{}
	var onlineSec, coveredSec int64
	add := func(from, to int64, online bool) {
		if to <= from {
			return
		}
		coveredSec += to - from
		if online {
			onlineSec += to - from
		}
		if n := len(segs); n > 0 && segs[n-1].Online == online && segs[n-1].To.Unix() == from {
			segs[n-1].To = fromUnix(to)
			return
		}
		segs = append(segs, model.TimelineSegment{From: fromUnix(from), To: fromUnix(to), Online: online})
	}
	for i, p := range points {
		next := end
		if i+1 < len(points) {
			next = points[i+1].t
		}
		if next <= p.t {
			continue
		}
		if next-p.t > 3*p.interval {
			cut := p.t + p.interval
			add(p.t, cut, p.online)
			add(cut, next, false)
			continue
		}
		add(p.t, next, p.online)
	}
	return segs, onlineSec, coveredSec
}
