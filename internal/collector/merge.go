package collector

import (
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// backendRunning is the tailscaled backend state of a connected daemon.
const backendRunning = "Running"

// taggedDevicesPseudoUser is the login name the control plane assigns to
// tagged nodes; the model represents tagged devices with an empty owner.
const taggedDevicesPseudoUser = "tagged-devices"

// pollEntry is one device present in at least one source during a poll.
type pollEntry struct {
	id     model.DeviceID
	local  *source.LocalPeer // nil for API-only devices
	api    *source.APIDevice // nil when absent from (or without) the control API
	isSelf bool
	online bool
	ip     string // primary Tailscale IP for pings and agent fetches
}

// buildEntries joins the local netmap (self + peers) with the cached control
// API device list on the stable node ID. Devices without an ID are skipped.
func (c *Collector) buildEntries(now time.Time, status *source.LocalStatus) []pollEntry {
	entries := make([]pollEntry, 0, len(status.Peers)+len(c.apiDevices)+1)
	index := make(map[model.DeviceID]int, len(status.Peers)+1)

	if status.Self.ID != "" {
		self := status.Self
		if len(self.TailscaleIPs) == 0 {
			self.TailscaleIPs = slices.Clone(status.TailscaleIPs)
		}
		e := pollEntry{
			id:     self.ID,
			local:  &self,
			isSelf: true,
			online: status.BackendState == backendRunning,
			ip:     primaryIP(self.TailscaleIPs),
		}
		index[e.id] = len(entries)
		entries = append(entries, e)
	}
	for i := range status.Peers {
		p := &status.Peers[i]
		if p.ID == "" {
			c.log.Debug("collector: skipping peer without stable id", "hostname", p.HostName)
			continue
		}
		if _, dup := index[p.ID]; dup {
			continue
		}
		index[p.ID] = len(entries)
		entries = append(entries, pollEntry{id: p.ID, local: p, online: p.Online, ip: primaryIP(p.TailscaleIPs)})
	}
	for i := range c.apiDevices {
		ad := &c.apiDevices[i]
		if ad.NodeID == "" {
			continue
		}
		if j, ok := index[ad.NodeID]; ok {
			entries[j].api = ad
			continue
		}
		online := !ad.LastSeen.IsZero() && now.Sub(ad.LastSeen) < apiOnlineWindow
		index[ad.NodeID] = len(entries)
		entries = append(entries, pollEntry{id: ad.NodeID, api: ad, online: online, ip: primaryIP(ad.Addresses)})
	}
	return entries
}

// mergeDevice builds the model.Device for one entry. The local netmap wins
// for identity, online state, path, byte counters, addresses, tags and
// routes present in the netmap; the control API fills the fields only it
// knows. prev is the device from the previous snapshot (nil when new) and
// rateDT the seconds since the previous successful poll (0 disables rate
// computation).
func (c *Collector) mergeDevice(now time.Time, status *source.LocalStatus, e *pollEntry, prev *model.Device, rateDT float64) model.Device {
	lp, ad := e.local, e.api
	d := model.Device{ID: e.id, IsSelf: e.isSelf, Online: e.online, UpdatedAt: now}

	if lp != nil {
		d.DNSName = strings.TrimSuffix(lp.DNSName, ".")
		d.Hostname = lp.HostName
		d.OS = lp.OS
		d.Addresses = sortAddresses(lp.TailscaleIPs)
		d.User = lp.UserLogin
		d.UserName = lp.UserDisplay
		d.Tags = nonNil(slices.Clone(lp.Tags))
		d.Active = lp.Active
		d.LastSeen = lp.LastSeen
		if !lp.LastHandshake.IsZero() {
			t := lp.LastHandshake
			d.LastHandshake = &t
		}
		d.Created = lp.Created
		d.IsExitNode = lp.ExitNode
		d.ExitNodeOption = lp.ExitNodeOpt
		d.PrimaryRoutes = nonNil(slices.Clone(lp.PrimaryRoutes))
		d.Expired = lp.Expired
		if lp.KeyExpiry != nil && !lp.KeyExpiry.IsZero() {
			t := *lp.KeyExpiry
			d.KeyExpiry = &t
		}
		d.IsExternal = lp.ShareeNode
		if lp.Location != nil {
			loc := *lp.Location
			d.Location = &loc
		}
		d.Connectivity.RxBytes = lp.RxBytes
		d.Connectivity.TxBytes = lp.TxBytes
		d.Connectivity.CurAddr = lp.CurAddr
		// A node the daemon lists in its netmap has been admitted to the
		// tailnet; the control API refines this below when available.
		d.Authorized = true
	}

	if ad != nil {
		if d.DNSName == "" {
			d.DNSName = strings.TrimSuffix(ad.Name, ".")
		}
		if d.Hostname == "" {
			d.Hostname = ad.Hostname
		}
		if d.OS == "" {
			d.OS = ad.OS
		}
		if len(d.Addresses) == 0 {
			d.Addresses = sortAddresses(ad.Addresses)
		}
		if lp == nil {
			d.User = ad.User
			d.Tags = nonNil(slices.Clone(ad.Tags))
		}
		if d.LastSeen.IsZero() {
			d.LastSeen = ad.LastSeen
		}
		if !ad.Created.IsZero() {
			d.Created = ad.Created
		}
		d.ClientVersion = ad.ClientVersion
		d.UpdateAvailable = ad.UpdateAvailable
		d.Authorized = ad.Authorized
		if d.KeyExpiry == nil && ad.Expires != nil && !ad.Expires.IsZero() {
			t := *ad.Expires
			d.KeyExpiry = &t
		}
		d.KeyExpiryDisabled = ad.KeyExpiryDisabled
		d.IsExternal = d.IsExternal || ad.IsExternal
		d.AdvertisedRoutes = nonNil(slices.Clone(ad.AdvertisedRoutes))
		d.EnabledRoutes = nonNil(slices.Clone(ad.EnabledRoutes))
		d.BlocksIncoming = ad.BlocksIncoming
		d.Connectivity.Endpoints = slices.Clone(ad.Endpoints)
		if len(ad.DERPLatencyMs) > 0 {
			d.Connectivity.DERPLatencyMs = maps.Clone(ad.DERPLatencyMs)
		}
		d.Connectivity.PreferredDERP = ad.PreferredDERP
		if ad.NATSupport != nil {
			n := *ad.NATSupport
			d.Connectivity.NATSupport = &n
		}
		if ad.MappingVariesByDestIP != nil {
			b := *ad.MappingVariesByDestIP
			d.Connectivity.MappingVariesByDestIP = &b
		}
		if hasExitRoute(ad.EnabledRoutes) {
			d.ExitNodeOption = true
		}
	}

	if e.isSelf {
		d.Online = status.BackendState == backendRunning
		d.Active = d.Online
		if d.ClientVersion == "" {
			d.ClientVersion = status.Version
		}
		if ad == nil && status.RunningLatest != nil {
			d.UpdateAvailable = !*status.RunningLatest
		}
		if len(d.Addresses) == 0 {
			d.Addresses = sortAddresses(status.TailscaleIPs)
		}
	}

	if d.User == taggedDevicesPseudoUser {
		d.User = ""
	}
	d.Name = baseName(d.DNSName, d.Hostname, string(d.ID))
	d.Addresses = nonNil(d.Addresses)
	d.Tags = nonNil(d.Tags)
	d.AdvertisedRoutes = nonNil(d.AdvertisedRoutes)
	d.EnabledRoutes = nonNil(d.EnabledRoutes)
	d.PrimaryRoutes = nonNil(d.PrimaryRoutes)

	if d.Online && lp != nil {
		d.LastSeen = now
	}
	if prev != nil {
		if d.LastSeen.IsZero() {
			d.LastSeen = prev.LastSeen
		}
		if d.Created.IsZero() {
			d.Created = prev.Created
		}
		if d.Model == "" {
			d.Model = prev.Model
		}
	}
	if prev != nil && !prev.FirstSeen.IsZero() {
		d.FirstSeen = prev.FirstSeen
	} else {
		d.FirstSeen = now
	}
	if !d.Expired && d.KeyExpiry != nil && !d.KeyExpiryDisabled && !d.KeyExpiry.After(now) {
		d.Expired = true
	}

	// Connectivity: the hub's own row has no path to itself.
	if e.isSelf {
		d.Connectivity.Path = model.PathNone
		d.Connectivity.CurAddr = ""
		d.Connectivity.RxBytes = 0
		d.Connectivity.TxBytes = 0
	} else {
		if lp != nil {
			d.Connectivity.Path, d.Connectivity.Relay = derivePath(d.Online, lp.CurAddr, lp.Relay)
		} else if d.Online {
			d.Connectivity.Path = model.PathUnknown
		} else {
			d.Connectivity.Path = model.PathNone
		}
		if prev != nil {
			if prev.Connectivity.LatencyMs != nil {
				v := *prev.Connectivity.LatencyMs
				d.Connectivity.LatencyMs = &v
			}
			if prev.Connectivity.LastPing != nil {
				t := *prev.Connectivity.LastPing
				d.Connectivity.LastPing = &t
			}
			if lp != nil && rateDT > 0 {
				d.Connectivity.RxRate = counterRateInt(d.Connectivity.RxBytes, prev.Connectivity.RxBytes, rateDT)
				d.Connectivity.TxRate = counterRateInt(d.Connectivity.TxBytes, prev.Connectivity.TxBytes, rateDT)
			}
		}
	}
	return d
}

// derivePath maps the netmap's view of a peer to a path. A current direct
// endpoint means direct; otherwise the peer's home DERP region means relay.
// Online peers with neither are unknown; offline peers have no path.
// CurAddr is checked first because tailscaled always reports a peer's home
// region in Relay, even while the connection is direct.
func derivePath(online bool, curAddr, relay string) (model.PathType, string) {
	switch {
	case !online:
		return model.PathNone, ""
	case curAddr != "":
		return model.PathDirect, ""
	case relay != "":
		return model.PathRelay, relay
	}
	return model.PathUnknown, ""
}

// pathFromPing refines the path from a successful disco ping: a direct
// endpoint without a DERP region is direct; a DERP region code is relay.
// ok is false when the reply carries neither.
func pathFromPing(reply *source.PingReply) (path model.PathType, relay string, ok bool) {
	if reply == nil {
		return "", "", false
	}
	switch {
	case reply.Endpoint != "" && reply.DERPRegionID == 0:
		return model.PathDirect, "", true
	case reply.DERPRegionCode != "":
		return model.PathRelay, reply.DERPRegionCode, true
	}
	return "", "", false
}

// applyPing records a successful ping on d (latency, last ping time and a
// refined path). It reports whether the ping succeeded; failures leave d
// untouched.
func applyPing(now time.Time, d *model.Device, po pingOutcome) bool {
	if po.err != nil || po.reply == nil || po.reply.Err != "" {
		return false
	}
	lat := po.reply.LatencyMs
	d.Connectivity.LatencyMs = &lat
	at := now
	d.Connectivity.LastPing = &at
	if d.Online {
		if p, relay, ok := pathFromPing(po.reply); ok {
			d.Connectivity.Path = p
			d.Connectivity.Relay = relay
			if p == model.PathDirect && d.Connectivity.CurAddr == "" {
				d.Connectivity.CurAddr = po.reply.Endpoint
			}
		}
	}
	return true
}

// finishUptime fills d.Uptime from the cached ratios and the previous
// device's last transition time.
func (c *Collector) finishUptime(now time.Time, prev *model.Device, d *model.Device) {
	d.Uptime = model.UptimeStats{
		Pct24h: pctPtr(c.ratios.d24h, d.ID),
		Pct7d:  pctPtr(c.ratios.d7d, d.ID),
		Pct30d: pctPtr(c.ratios.d30d, d.ID),
	}
	var last *time.Time
	if prev != nil && prev.Uptime.LastChange != nil && prev.Online == d.Online {
		t := *prev.Uptime.LastChange
		last = &t
	} else {
		t := now
		last = &t
	}
	d.Uptime.LastChange = last
	if d.Online {
		secs := int64(now.Sub(*last).Seconds())
		if secs < 0 {
			secs = 0
		}
		d.Uptime.OnlineFor = &secs
	}
}

// markRemoved turns a device that vanished from every source into an
// offline row without a path, agent data or metrics.
func (c *Collector) markRemoved(now time.Time, d *model.Device) {
	d.Online = false
	d.Active = false
	d.Connectivity.Path = model.PathNone
	d.Connectivity.Relay = ""
	d.Connectivity.CurAddr = ""
	d.Connectivity.RxRate = 0
	d.Connectivity.TxRate = 0
	d.Metrics = nil
	d.Agent = model.AgentStatus{State: model.AgentUnknown, Version: d.Agent.Version, LastSuccess: d.Agent.LastSuccess, URL: d.Agent.URL}
	if !c.cfg.AgentEnabled {
		d.Agent.State = model.AgentDisabled
	}
	d.Uptime.OnlineFor = nil
}

// pctPtr converts a 0..1 ratio from the map into a 0..100 percentage
// pointer, or nil when the device has no data.
func pctPtr(m map[model.DeviceID]float64, id model.DeviceID) *float64 {
	v, ok := m[id]
	if !ok {
		return nil
	}
	p := v * 100
	return &p
}

// counterRateInt computes a bytes/second rate from two cumulative counters.
// A negative delta (counter reset) or non-positive interval yields 0.
func counterRateInt(cur, prev int64, dt float64) float64 {
	if dt <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

// baseName returns the first label of a DNS name, falling back to the
// hostname and finally to the given default.
func baseName(dnsName, hostname, def string) string {
	dnsName = strings.TrimSuffix(strings.TrimSpace(dnsName), ".")
	if dnsName != "" {
		label, _, _ := strings.Cut(dnsName, ".")
		if label != "" {
			return label
		}
	}
	if h := strings.TrimSpace(hostname); h != "" {
		return h
	}
	return def
}

// sortAddresses returns a copy of addrs with IPv4 addresses first, keeping
// the relative order otherwise. Empty strings are dropped.
func sortAddresses(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" && isIPv4(a) {
			out = append(out, a)
		}
	}
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" && !isIPv4(a) {
			out = append(out, a)
		}
	}
	return out
}

// primaryIP returns the first IPv4 address, else the first address, else "".
func primaryIP(addrs []string) string {
	sorted := sortAddresses(addrs)
	if len(sorted) == 0 {
		return ""
	}
	return sorted[0]
}

// isIPv4 reports whether s parses as an IPv4 (or IPv4-mapped) address.
func isIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Unmap().Is4()
}

// hasExitRoute reports whether routes contain a default route.
func hasExitRoute(routes []string) bool {
	for _, r := range routes {
		if r == "0.0.0.0/0" || r == "::/0" {
			return true
		}
	}
	return false
}

// isSubnetRouter reports whether the device routes a subnet: it is primary
// for a route in the netmap, or has a non-default enabled route in the
// control API.
func isSubnetRouter(d *model.Device) bool {
	if len(d.PrimaryRoutes) > 0 {
		return true
	}
	for _, r := range d.EnabledRoutes {
		if r != "0.0.0.0/0" && r != "::/0" {
			return true
		}
	}
	return false
}

// nonNil returns s, or an empty non-nil slice when s is nil, so JSON shows
// [] rather than null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// sortDevices orders devices by name (case-insensitive), then by ID.
func sortDevices(devs []model.Device) {
	slices.SortStableFunc(devs, func(a, b model.Device) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
}
