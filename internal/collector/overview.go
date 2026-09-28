package collector

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// emptySparklines returns zero-length (non-nil) sparkline arrays.
func emptySparklines() model.OverviewSparklines {
	return model.OverviewSparklines{T: []int64{}, Online: []float64{}, RxRate: []float64{}, TxRate: []float64{}, Latency: []float64{}}
}

// buildHub derives HubInfo from the local status, the self device and the
// errors recorded during this poll.
func (c *Collector) buildHub(now time.Time, status *source.LocalStatus, self *model.Device, errs []string) model.HubInfo {
	h := c.Hub()
	h.Version = c.cfg.Version
	h.StartedAt = c.startedAt
	h.DemoMode = c.cfg.DemoMode
	h.ControlAPI = c.apiConfigured()
	h.AdminActions = c.cfg.AdminActions
	h.AgentPort = c.cfg.AgentPort
	h.PollIntervalSec = int(c.cfg.PollInterval / time.Second)
	h.LastPoll = now
	h.LastError = strings.Join(errs, "; ")
	h.LastAPIPoll = nil
	if !c.lastAPIPoll.IsZero() {
		t := c.lastAPIPoll
		h.LastAPIPoll = &t
	}
	if status != nil {
		h.Tailnet = status.TailnetName
		h.MagicDNSSuffix = strings.TrimSuffix(status.MagicDNSSuffix, ".")
		h.TailscaleVer = status.Version
		h.BackendState = status.BackendState
		h.Health = nonNil(slices.Clone(status.Health))
		h.SelfIPs = sortAddresses(status.TailscaleIPs)
	}
	if h.Tailnet == "" && c.api != nil {
		if tn := c.api.Tailnet(); tn != "" && tn != "-" {
			h.Tailnet = tn
		}
	}
	if self != nil {
		h.SelfName = self.Name
		h.SelfID = self.ID
		if len(h.SelfIPs) == 0 {
			h.SelfIPs = sortAddresses(self.Addresses)
		}
	}
	h.Health = nonNil(h.Health)
	h.SelfIPs = nonNil(h.SelfIPs)
	return h
}

// buildOverview computes the dashboard summary for the given devices. Store
// lookups (open alerts, sparklines) that fail are logged and leave their
// fields at zero values.
func (c *Collector) buildOverview(ctx context.Context, now time.Time, devices []model.Device, hub model.HubInfo) model.Overview {
	ov := model.Overview{
		Hub:           hub,
		OSBreakdown:   map[string]int{},
		UserBreakdown: map[string]int{},
		Sparklines:    emptySparklines(),
	}
	var latSum float64
	var latN int
	for i := range devices {
		d := &devices[i]
		ov.Devices++
		if d.Online {
			ov.Online++
		} else {
			ov.Offline++
		}
		if d.Online && !d.IsSelf {
			switch d.Connectivity.Path {
			case model.PathDirect:
				ov.Direct++
			case model.PathRelay:
				ov.Relayed++
			}
			if d.Connectivity.LatencyMs != nil {
				latSum += *d.Connectivity.LatencyMs
				latN++
			}
		}
		if d.Agent.State == model.AgentReachable {
			ov.AgentsUp++
		}
		if d.UpdateAvailable {
			ov.UpdatesPending++
		}
		if keyExpiringSoon(now, d) {
			ov.KeysExpiring++
		}
		if !d.Authorized {
			ov.Unauthorized++
		}
		if d.ExitNodeOption {
			ov.ExitNodes++
		}
		if isSubnetRouter(d) {
			ov.SubnetRouters++
		}
		if !d.IsSelf {
			ov.TotalRxRate += d.Connectivity.RxRate
			ov.TotalTxRate += d.Connectivity.TxRate
			ov.TotalRxBytes += d.Connectivity.RxBytes
			ov.TotalTxBytes += d.Connectivity.TxBytes
		}
		ov.OSBreakdown[osKey(d.OS)]++
		ov.UserBreakdown[userKey(d)]++
	}
	if latN > 0 {
		avg := latSum / float64(latN)
		ov.AvgLatencyMs = &avg
	}

	if ctx.Err() != nil {
		return ov
	}
	alerts, err := c.st.ListAlerts(ctx, model.AlertQuery{State: model.AlertOpen, Limit: 1000})
	if err != nil {
		c.log.Warn("collector: list open alerts failed", "err", err)
	} else {
		ov.OpenAlerts = len(alerts)
		for i := range alerts {
			if alerts[i].Severity == model.SeverityCritical {
				ov.CriticalAlerts++
			}
		}
	}
	// Use complete buckets only: the bucket containing "now" is still filling
	// and would render as a misleading drop at the right edge of every
	// sparkline (and skew the "vs 1h ago" deltas derived from it).
	end := now.Truncate(sparklineStep)
	sp, err := c.st.NetworkSparklines(ctx, end.Add(-sparklineWindow), end, sparklineStep)
	if err != nil {
		c.log.Warn("collector: sparklines failed", "err", err)
	} else if sp != nil {
		ov.Sparklines = *sp
		if ov.Sparklines.T == nil {
			ov.Sparklines = emptySparklines()
		}
	}
	return ov
}

// keyExpiringSoon reports whether the device's node key expires within the
// next seven days (and has not expired or been exempted).
func keyExpiringSoon(now time.Time, d *model.Device) bool {
	if d.KeyExpiry == nil || d.KeyExpiryDisabled || d.Expired {
		return false
	}
	return d.KeyExpiry.After(now) && d.KeyExpiry.Before(now.Add(keyExpiringWindow))
}

// osKey normalises an OS string for the breakdown map.
func osKey(os string) string {
	if os == "" {
		return "unknown"
	}
	return os
}

// userKey normalises an owner for the breakdown map: tagged devices are
// grouped under "tagged" and ownerless ones under "unknown".
func userKey(d *model.Device) string {
	switch {
	case d.User != "":
		return d.User
	case len(d.Tags) > 0:
		return "tagged"
	}
	return "unknown"
}
