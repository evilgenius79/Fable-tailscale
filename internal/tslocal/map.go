package tslocal

import (
	"cmp"
	"errors"
	"net/netip"
	"slices"
	"strings"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// backendRunning is the ipn.State string for a connected daemon.
const backendRunning = "Running"

// TaggedDeviceLogin is the login name reported for identities that are
// tagged nodes rather than users (see model.Identity.Login).
const TaggedDeviceLogin = "tagged-device"

// taggedDevicesPseudoUser is the login name of the pseudo user that the
// control plane assigns to tagged nodes.
const taggedDevicesPseudoUser = "tagged-devices"

// MapStatus converts a daemon status into a source.LocalStatus. It is
// nil-safe for every optional field of ipnstate.Status. Peers are sorted by
// DNS name (then ID, then public key) for deterministic output.
func MapStatus(st *ipnstate.Status) *source.LocalStatus {
	if st == nil {
		return &source.LocalStatus{}
	}
	ls := &source.LocalStatus{
		Version:        st.Version,
		BackendState:   st.BackendState,
		TailscaleIPs:   addrStrings(st.TailscaleIPs),
		Health:         slices.Clone(st.Health),
		MagicDNSSuffix: st.MagicDNSSuffix,
	}
	if st.CurrentTailnet != nil {
		ls.TailnetName = st.CurrentTailnet.Name
		if st.CurrentTailnet.MagicDNSSuffix != "" {
			ls.MagicDNSSuffix = st.CurrentTailnet.MagicDNSSuffix
		}
	}
	if st.Self != nil {
		ls.Self = MapPeer(st, st.Self)
		if len(ls.Self.TailscaleIPs) == 0 {
			ls.Self.TailscaleIPs = addrStrings(st.TailscaleIPs)
		}
	}
	ls.Self.Online = st.BackendState == backendRunning
	if len(st.Peer) > 0 {
		ls.Peers = make([]source.LocalPeer, 0, len(st.Peer))
		for _, ps := range st.Peer {
			if ps == nil {
				continue
			}
			ls.Peers = append(ls.Peers, MapPeer(st, ps))
		}
		slices.SortStableFunc(ls.Peers, comparePeers)
	}
	if cv := st.ClientVersion; cv != nil {
		running := cv.RunningLatest
		ls.RunningLatest = &running
		ls.LatestVersion = cv.LatestVersion
	}
	return ls
}

// comparePeers orders peers by DNS name, then stable ID, then public key.
func comparePeers(a, b source.LocalPeer) int {
	if c := strings.Compare(a.DNSName, b.DNSName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	return strings.Compare(a.PublicKey, b.PublicKey)
}

// MapPeer converts one ipnstate.PeerStatus into a source.LocalPeer. st is
// used to resolve user profiles and may be nil; ps may be nil (zero value is
// returned). Tagged nodes report an empty owner login, per the LocalPeer
// contract.
func MapPeer(st *ipnstate.Status, ps *ipnstate.PeerStatus) source.LocalPeer {
	if ps == nil {
		return source.LocalPeer{}
	}
	p := source.LocalPeer{
		ID:            model.DeviceID(ps.ID),
		HostName:      ps.HostName,
		DNSName:       strings.TrimSuffix(ps.DNSName, "."),
		OS:            ps.OS,
		TailscaleIPs:  addrStrings(ps.TailscaleIPs),
		Addrs:         slices.Clone(ps.Addrs),
		CurAddr:       ps.CurAddr,
		Relay:         ps.Relay,
		RxBytes:       ps.RxBytes,
		TxBytes:       ps.TxBytes,
		Created:       ps.Created,
		LastWrite:     ps.LastWrite,
		LastSeen:      ps.LastSeen,
		LastHandshake: ps.LastHandshake,
		Online:        ps.Online,
		Active:        ps.Active,
		ExitNode:      ps.ExitNode,
		ExitNodeOpt:   ps.ExitNodeOption,
		Expired:       ps.Expired,
		ShareeNode:    ps.ShareeNode,
		Location:      mapLocation(ps.Location),
	}
	if !ps.PublicKey.IsZero() {
		p.PublicKey = ps.PublicKey.String()
	}
	if ps.Tags != nil {
		p.Tags = ps.Tags.AsSlice()
	}
	p.PrimaryRoutes = prefixViewStrings(ps.PrimaryRoutes)
	p.AllowedIPs = prefixViewStrings(ps.AllowedIPs)
	if ps.KeyExpiry != nil {
		t := *ps.KeyExpiry
		p.KeyExpiry = &t
	}

	// Owner: shared-in nodes are attributed to the user who shared them when
	// that profile is known; otherwise to the node's user. Tagged nodes have
	// no owner (the control plane assigns them a pseudo user).
	tagged := ps.Tags != nil && ps.Tags.Len() > 0
	if !tagged && st != nil {
		var up tailcfg.UserProfile
		var ok bool
		if ps.AltSharerUserID != 0 {
			up, ok = st.User[ps.AltSharerUserID]
		}
		if !ok {
			up, ok = st.User[ps.UserID]
		}
		if ok && up.LoginName != taggedDevicesPseudoUser {
			p.UserLogin = up.LoginName
			p.UserDisplay = up.DisplayName
		}
	}
	return p
}

// MapWhoIs converts a LocalAPI WhoIs response into a source.WhoIs. It
// returns an error when resp or resp.Node is nil. Tagged nodes report
// TaggedDeviceLogin as their login name unless the daemon supplied a real
// user login.
func MapWhoIs(resp *apitype.WhoIsResponse) (*source.WhoIs, error) {
	if resp == nil || resp.Node == nil {
		return nil, errors.New("whois response has no node")
	}
	n := resp.Node
	w := &source.WhoIs{
		NodeID:   model.DeviceID(n.StableID),
		NodeName: strings.TrimSuffix(n.Name, "."),
		Tags:     slices.Clone(n.Tags),
		IsTagged: n.IsTagged(),
	}
	if len(n.Addresses) > 0 {
		w.NodeIP = n.Addresses[0].Addr().String()
	}
	if up := resp.UserProfile; up != nil {
		w.LoginName = up.LoginName
		w.DisplayName = up.DisplayName
		w.ProfilePic = up.ProfilePicURL
	}
	if w.IsTagged && (w.LoginName == "" || w.LoginName == taggedDevicesPseudoUser) {
		w.LoginName = TaggedDeviceLogin
	}
	return w, nil
}

// MapPing converts a daemon ping result into a source.PingReply. A nil
// result maps to the zero value.
func MapPing(res *ipnstate.PingResult) source.PingReply {
	if res == nil {
		return source.PingReply{}
	}
	return source.PingReply{
		LatencyMs:      res.LatencySeconds * 1000,
		Endpoint:       res.Endpoint,
		DERPRegionID:   res.DERPRegionID,
		DERPRegionCode: res.DERPRegionCode,
		NodeName:       res.NodeName,
		Err:            res.Err,
	}
}

// mapLocation converts an optional tailcfg.Location.
func mapLocation(l *tailcfg.Location) *model.Location {
	if l == nil {
		return nil
	}
	return &model.Location{
		Country:     l.Country,
		CountryCode: l.CountryCode,
		City:        l.City,
		Latitude:    l.Latitude,
		Longitude:   l.Longitude,
	}
}

// addrStrings stringifies IP addresses, returning nil for an empty input.
func addrStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.IsValid() {
			out = append(out, a.String())
		}
	}
	return out
}

// prefixViewStrings stringifies an optional view of prefixes, returning nil
// when the view is absent.
func prefixViewStrings(v *views.Slice[netip.Prefix]) []string {
	if v == nil || v.IsNil() {
		return nil
	}
	out := make([]string, 0, v.Len())
	for _, p := range v.All() {
		if p.IsValid() {
			out = append(out, p.String())
		}
	}
	return out
}
