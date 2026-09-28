package demo

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// tailscaledVersion is the hub's own tailscaled version string.
const tailscaledVersion = ClientVersion + "-t1a2b3c4d5"

// Status implements source.LocalSource. It reports the hub node and every
// authorized, non-deleted peer.
func (s *Sim) Status(ctx context.Context) (*source.LocalStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: status: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tick()

	latest := true
	st := &source.LocalStatus{
		Version:        tailscaledVersion,
		BackendState:   "Running",
		TailscaleIPs:   s.self.addresses(),
		TailnetName:    TailnetName,
		MagicDNSSuffix: MagicDNSSuffix,
		RunningLatest:  &latest,
		LatestVersion:  ClientVersion,
		Self:           s.localPeer(s.self, t),
	}
	for _, d := range s.devs {
		if d.deleted || d == s.self || !d.authorized {
			continue
		}
		st.Peers = append(st.Peers, s.localPeer(d, t))
	}
	return st, nil
}

// localPeer builds the LocalAPI view of one device. s.mu must be held.
func (s *Sim) localPeer(d *device, t time.Time) source.LocalPeer {
	in := s.instantAt(d, t, false)
	p := source.LocalPeer{
		ID:            d.id,
		PublicKey:     d.pubKey,
		HostName:      d.hostname,
		DNSName:       d.dnsName(),
		OS:            d.os,
		UserLogin:     d.user,
		UserDisplay:   displayName(d.user),
		Tags:          append([]string(nil), d.tags...),
		TailscaleIPs:  d.addresses(),
		Addrs:         d.endpoints(),
		Relay:         d.relay,
		Created:       s.created(d),
		Online:        in.online,
		Active:        in.active,
		ExitNode:      d != s.self && d.exitNodeInUse && d.exitEnabled() && in.online,
		ExitNodeOpt:   d.exitNodeOpt() && d.exitEnabled(),
		PrimaryRoutes: d.primaryRoutes(),
		AllowedIPs:    d.allowedIPs(),
		Expired:       in.expired,
		KeyExpiry:     s.keyExpiryPtr(d),
		ShareeNode:    d.external,
		Location:      d.location,
		RxBytes:       int64(in.counters[flowPeerRx]),
		TxBytes:       int64(in.counters[flowPeerTx]),
	}
	if d == s.self {
		p.Online = true
		p.Active = false
		p.LastSeen = t
		p.RxBytes, p.TxBytes = 0, 0
		return p
	}
	if in.online {
		p.LastSeen = t
		p.LastHandshake = d.lastHandshake(t)
		p.LastWrite = p.LastHandshake
		if in.active {
			p.LastWrite = t.Add(-time.Duration(unit(mix(d.h, chHandshake, 1))*8) * time.Second)
		}
		p.CurAddr = d.curAddr()
	} else {
		p.LastSeen = in.since
		p.LastHandshake = in.since
		p.LastWrite = in.since
	}
	return p
}

// WhoIs implements source.LocalSource. The simulated tailnet attributes every
// request, whatever its remote address, to the fixed identity
// "demo@example.com" (Demo User) connecting from alice-mbp. It is only
// consulted when the hub is not running with auth mode "none".
func (s *Sim) WhoIs(ctx context.Context, remoteAddr string) (*source.WhoIs, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: whois: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick()
	// Match on the fixed profile name so an admin rename keeps the
	// attribution; fall back to the hub if the node was deleted.
	node := s.self
	for _, d := range s.devs {
		if !d.deleted && d.profile.name == WhoIsNode {
			node = d
			break
		}
	}
	return &source.WhoIs{
		NodeID:      node.id,
		NodeName:    node.dnsName(),
		NodeIP:      node.ip4,
		Tags:        append([]string(nil), node.tags...),
		IsTagged:    len(node.tags) > 0,
		LoginName:   WhoIsLogin,
		DisplayName: WhoIsDisplay,
	}, nil
}

// Ping implements source.LocalSource. The reply is consistent with Status:
// a direct peer answers from its current endpoint, a relayed peer via its
// DERP region, and an offline peer yields a "no route" reply (Err set, no
// Go error, as tailscaled does). Unknown addresses wrap source.ErrNotFound.
func (s *Sim) Ping(ctx context.Context, ip string, timeout time.Duration) (*source.PingReply, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: ping: %w", err)
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return nil, fmt.Errorf("demo: ping: invalid ip %q: %w", ip, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tick()

	d := s.lookupIP(addr.Unmap().String())
	if d == nil || !d.authorized {
		return nil, fmt.Errorf("demo: ping %s: no such peer: %w", addr, source.ErrNotFound)
	}
	if d == s.self {
		return nil, fmt.Errorf("demo: ping %s: cannot ping the local node", addr)
	}
	in := s.instantAt(d, t, false)
	reply := &source.PingReply{NodeName: d.dnsName()}
	if !in.online {
		reply.Err = fmt.Sprintf("no route to %s: peer offline", addr)
		return reply, nil
	}
	if timeout > 0 && time.Duration(in.latency*float64(time.Millisecond)) >= timeout {
		reply.Err = "timeout"
		return reply, nil
	}
	reply.LatencyMs = in.latency
	if d.direct {
		reply.Endpoint = d.curAddr()
	} else {
		reply.DERPRegionCode = d.relay
		reply.DERPRegionID = derpRegionIDs[d.relay]
	}
	return reply, nil
}
