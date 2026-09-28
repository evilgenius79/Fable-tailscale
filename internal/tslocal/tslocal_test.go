package tslocal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// fakeAPI is an in-memory localAPI.
type fakeAPI struct {
	status func(ctx context.Context) (*ipnstate.Status, error)
	whois  func(ctx context.Context, addr string) (*apitype.WhoIsResponse, error)
	ping   func(ctx context.Context, ip netip.Addr, pt tailcfg.PingType) (*ipnstate.PingResult, error)
	calls  int
}

func (f *fakeAPI) Status(ctx context.Context) (*ipnstate.Status, error) {
	f.calls++
	return f.status(ctx)
}

func (f *fakeAPI) WhoIs(ctx context.Context, addr string) (*apitype.WhoIsResponse, error) {
	f.calls++
	return f.whois(ctx, addr)
}

func (f *fakeAPI) Ping(ctx context.Context, ip netip.Addr, pt tailcfg.PingType) (*ipnstate.PingResult, error) {
	f.calls++
	return f.ping(ctx, ip, pt)
}

func newTestClient(api localAPI, socket string) *Client {
	return &Client{
		api:    api,
		socket: socket,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }
func mustAddr(s string) netip.Addr     { return netip.MustParseAddr(s) }

var (
	tCreated   = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	tWrite     = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	tSeen      = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	tHandshake = time.Date(2026, 9, 28, 9, 59, 0, 0, time.UTC)
	tExpiry    = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
)

// testStatus builds a representative daemon status with three peers whose
// map iteration order is random, so sorting is exercised.
func testStatus() (*ipnstate.Status, map[string]key.NodePublic) {
	keys := map[string]key.NodePublic{
		"zeta":   key.NewNode().Public(),
		"alpha":  key.NewNode().Public(),
		"shared": key.NewNode().Public(),
	}
	tags := views.SliceOf([]string{"tag:server", "tag:prod"})
	routes := views.SliceOf([]netip.Prefix{mustPrefix("10.0.0.0/24"), mustPrefix("192.168.1.0/24")})
	allowed := views.SliceOf([]netip.Prefix{mustPrefix("100.64.0.2/32"), mustPrefix("10.0.0.0/24")})
	exp := tExpiry
	st := &ipnstate.Status{
		Version:      "1.102.5-t1234",
		BackendState: "Running",
		TailscaleIPs: []netip.Addr{mustAddr("100.64.0.1"), mustAddr("fd7a:115c:a1e0::1")},
		Self: &ipnstate.PeerStatus{
			ID:       "self1",
			DNSName:  "hub.example.ts.net.",
			HostName: "hub",
			OS:       "linux",
			UserID:   1,
			Online:   false,
		},
		Health:         []string{"warning: something"},
		MagicDNSSuffix: "legacy.ts.net",
		CurrentTailnet: &ipnstate.TailnetStatus{Name: "alice@example.com", MagicDNSSuffix: "example.ts.net"},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			keys["zeta"]: {
				ID:             "zeta1",
				PublicKey:      keys["zeta"],
				HostName:       "Zeta-Laptop",
				DNSName:        "zeta.example.ts.net.",
				OS:             "macOS",
				UserID:         1,
				TailscaleIPs:   []netip.Addr{mustAddr("100.64.0.2"), mustAddr("fd7a:115c:a1e0::2")},
				Addrs:          []string{"1.2.3.4:41641", "192.168.0.7:41641"},
				CurAddr:        "1.2.3.4:41641",
				RxBytes:        100,
				TxBytes:        200,
				Created:        tCreated,
				LastWrite:      tWrite,
				LastSeen:       tSeen,
				LastHandshake:  tHandshake,
				Online:         true,
				Active:         true,
				ExitNode:       true,
				ExitNodeOption: true,
				KeyExpiry:      &exp,
				Location: &tailcfg.Location{
					Country: "Canada", CountryCode: "CA", City: "Squamish",
					CityCode: "YSE", Latitude: 49.7, Longitude: -123.15, Priority: 3,
				},
			},
			keys["alpha"]: {
				ID:            "alpha1",
				PublicKey:     keys["alpha"],
				HostName:      "alpha",
				DNSName:       "alpha.example.ts.net.",
				OS:            "linux",
				UserID:        2,
				Tags:          &tags,
				PrimaryRoutes: &routes,
				AllowedIPs:    &allowed,
				Relay:         "nyc",
				Online:        true,
				Expired:       true,
			},
			keys["shared"]: {
				ID:              "shared1",
				PublicKey:       keys["shared"],
				HostName:        "shared",
				DNSName:         "shared.other.ts.net.",
				OS:              "windows",
				UserID:          3,
				AltSharerUserID: 4,
				ShareeNode:      true,
			},
		},
		User: map[tailcfg.UserID]tailcfg.UserProfile{
			1: {ID: 1, LoginName: "alice@example.com", DisplayName: "Alice"},
			2: {ID: 2, LoginName: "tagged-devices", DisplayName: "Tagged Devices"},
			3: {ID: 3, LoginName: "bob@other.com", DisplayName: "Bob"},
			4: {ID: 4, LoginName: "carol@example.com", DisplayName: "Carol"},
		},
		ClientVersion: &tailcfg.ClientVersion{RunningLatest: false, LatestVersion: "1.103.0"},
	}
	return st, keys
}

func TestMapPeer(t *testing.T) {
	st, keys := testStatus()

	tests := []struct {
		name  string
		st    *ipnstate.Status
		ps    *ipnstate.PeerStatus
		check func(t *testing.T, p source.LocalPeer)
	}{
		{
			name: "full peer",
			st:   st,
			ps:   st.Peer[keys["zeta"]],
			check: func(t *testing.T, p source.LocalPeer) {
				want := source.LocalPeer{
					ID:            model.DeviceID("zeta1"),
					PublicKey:     keys["zeta"].String(),
					HostName:      "Zeta-Laptop",
					DNSName:       "zeta.example.ts.net",
					OS:            "macOS",
					UserLogin:     "alice@example.com",
					UserDisplay:   "Alice",
					TailscaleIPs:  []string{"100.64.0.2", "fd7a:115c:a1e0::2"},
					Addrs:         []string{"1.2.3.4:41641", "192.168.0.7:41641"},
					CurAddr:       "1.2.3.4:41641",
					RxBytes:       100,
					TxBytes:       200,
					Created:       tCreated,
					LastWrite:     tWrite,
					LastSeen:      tSeen,
					LastHandshake: tHandshake,
					Online:        true,
					Active:        true,
					ExitNode:      true,
					ExitNodeOpt:   true,
					KeyExpiry:     &tExpiry,
					Location: &model.Location{
						Country: "Canada", CountryCode: "CA", City: "Squamish",
						Latitude: 49.7, Longitude: -123.15,
					},
				}
				if p.KeyExpiry == nil || !p.KeyExpiry.Equal(tExpiry) {
					t.Fatalf("KeyExpiry = %v, want %v", p.KeyExpiry, tExpiry)
				}
				if p.Location == nil || *p.Location != *want.Location {
					t.Fatalf("Location = %+v, want %+v", p.Location, want.Location)
				}
				// Compare the rest field by field via %+v after neutralising pointers.
				p.KeyExpiry, want.KeyExpiry = nil, nil
				p.Location, want.Location = nil, nil
				if got, w := fmt.Sprintf("%+v", p), fmt.Sprintf("%+v", want); got != w {
					t.Fatalf("peer mismatch:\n got %s\nwant %s", got, w)
				}
				if !strings.HasPrefix(p.PublicKey, "nodekey:") {
					t.Fatalf("PublicKey %q lacks nodekey: prefix", p.PublicKey)
				}
			},
		},
		{
			name: "tagged peer has tags, routes and no owner",
			st:   st,
			ps:   st.Peer[keys["alpha"]],
			check: func(t *testing.T, p source.LocalPeer) {
				if got, want := fmt.Sprint(p.Tags), "[tag:server tag:prod]"; got != want {
					t.Errorf("Tags = %s, want %s", got, want)
				}
				if got, want := fmt.Sprint(p.PrimaryRoutes), "[10.0.0.0/24 192.168.1.0/24]"; got != want {
					t.Errorf("PrimaryRoutes = %s, want %s", got, want)
				}
				if got, want := fmt.Sprint(p.AllowedIPs), "[100.64.0.2/32 10.0.0.0/24]"; got != want {
					t.Errorf("AllowedIPs = %s, want %s", got, want)
				}
				if p.UserLogin != "" || p.UserDisplay != "" {
					t.Errorf("tagged peer owner = %q/%q, want empty", p.UserLogin, p.UserDisplay)
				}
				if p.Relay != "nyc" || !p.Expired || p.Location != nil || p.KeyExpiry != nil {
					t.Errorf("unexpected fields: relay=%q expired=%v loc=%v exp=%v", p.Relay, p.Expired, p.Location, p.KeyExpiry)
				}
			},
		},
		{
			name: "shared node attributed to sharer",
			st:   st,
			ps:   st.Peer[keys["shared"]],
			check: func(t *testing.T, p source.LocalPeer) {
				if p.UserLogin != "carol@example.com" || p.UserDisplay != "Carol" {
					t.Errorf("owner = %q/%q, want carol", p.UserLogin, p.UserDisplay)
				}
				if !p.ShareeNode || p.DNSName != "shared.other.ts.net" {
					t.Errorf("ShareeNode=%v DNSName=%q", p.ShareeNode, p.DNSName)
				}
				if p.Tags != nil || p.PrimaryRoutes != nil || p.AllowedIPs != nil {
					t.Errorf("nil views should map to nil slices: %v %v %v", p.Tags, p.PrimaryRoutes, p.AllowedIPs)
				}
			},
		},
		{
			name: "shared node with unknown sharer falls back to owner",
			st:   st,
			ps: &ipnstate.PeerStatus{
				ID: "x", UserID: 3, AltSharerUserID: 99,
			},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.UserLogin != "bob@other.com" {
					t.Errorf("UserLogin = %q, want bob@other.com", p.UserLogin)
				}
			},
		},
		{
			name: "unknown user id yields empty owner",
			st:   st,
			ps:   &ipnstate.PeerStatus{ID: "x", UserID: 42},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.UserLogin != "" {
					t.Errorf("UserLogin = %q, want empty", p.UserLogin)
				}
			},
		},
		{
			name: "pseudo tagged-devices user without tags is blanked",
			st:   st,
			ps:   &ipnstate.PeerStatus{ID: "x", UserID: 2},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.UserLogin != "" {
					t.Errorf("UserLogin = %q, want empty", p.UserLogin)
				}
			},
		},
		{
			name: "nil status still maps node fields",
			st:   nil,
			ps:   st.Peer[keys["zeta"]],
			check: func(t *testing.T, p source.LocalPeer) {
				if p.ID != "zeta1" || p.DNSName != "zeta.example.ts.net" || p.UserLogin != "" {
					t.Errorf("got %+v", p)
				}
			},
		},
		{
			name: "nil peer is zero value",
			st:   st,
			ps:   nil,
			check: func(t *testing.T, p source.LocalPeer) {
				if fmt.Sprintf("%+v", p) != fmt.Sprintf("%+v", source.LocalPeer{}) {
					t.Errorf("got %+v, want zero", p)
				}
			},
		},
		{
			name: "zero public key maps to empty string",
			st:   st,
			ps:   &ipnstate.PeerStatus{ID: "nokey"},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.PublicKey != "" {
					t.Errorf("PublicKey = %q, want empty", p.PublicKey)
				}
			},
		},
		{
			name: "peer relay replaces home DERP as relay",
			st:   st,
			ps:   &ipnstate.PeerStatus{ID: "pr", Relay: "nyc", PeerRelay: "203.0.113.5:41641:vni:7", Online: true},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.Relay != "peer-relay 203.0.113.5:41641:vni:7" || p.CurAddr != "" {
					t.Errorf("relay = %q curaddr = %q, want peer relay label and empty curaddr", p.Relay, p.CurAddr)
				}
			},
		},
		{
			name: "direct peer keeps home DERP even with a peer relay",
			st:   st,
			ps:   &ipnstate.PeerStatus{ID: "pd", Relay: "nyc", PeerRelay: "203.0.113.5:41641:vni:7", CurAddr: "1.2.3.4:41641"},
			check: func(t *testing.T, p source.LocalPeer) {
				if p.Relay != "nyc" || p.CurAddr != "1.2.3.4:41641" {
					t.Errorf("relay = %q curaddr = %q", p.Relay, p.CurAddr)
				}
			},
		},
		{
			name: "empty tags view yields empty non-nil tags",
			st:   st,
			ps: func() *ipnstate.PeerStatus {
				empty := views.SliceOf([]string{})
				return &ipnstate.PeerStatus{ID: "e", Tags: &empty}
			}(),
			check: func(t *testing.T, p source.LocalPeer) {
				if p.Tags == nil || len(p.Tags) != 0 {
					t.Errorf("Tags = %#v, want empty non-nil", p.Tags)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, MapPeer(tc.st, tc.ps))
		})
	}
}

func TestMapPeerCopiesPointers(t *testing.T) {
	exp := tExpiry
	ps := &ipnstate.PeerStatus{ID: "a", KeyExpiry: &exp, Addrs: []string{"x"}}
	p := MapPeer(nil, ps)
	exp = exp.Add(time.Hour)
	ps.Addrs[0] = "mutated"
	if !p.KeyExpiry.Equal(tExpiry) {
		t.Errorf("KeyExpiry aliased the source: %v", p.KeyExpiry)
	}
	if p.Addrs[0] != "x" {
		t.Errorf("Addrs aliased the source: %v", p.Addrs)
	}
}

func TestMapStatus(t *testing.T) {
	t.Run("full status", func(t *testing.T) {
		st, _ := testStatus()
		ls := MapStatus(st)
		if ls.Version != "1.102.5-t1234" || ls.BackendState != "Running" {
			t.Errorf("version/state = %q/%q", ls.Version, ls.BackendState)
		}
		if got := fmt.Sprint(ls.TailscaleIPs); got != "[100.64.0.1 fd7a:115c:a1e0::1]" {
			t.Errorf("TailscaleIPs = %s", got)
		}
		if ls.TailnetName != "alice@example.com" || ls.MagicDNSSuffix != "example.ts.net" {
			t.Errorf("tailnet = %q suffix = %q", ls.TailnetName, ls.MagicDNSSuffix)
		}
		if !ls.Self.Online || ls.Self.ID != "self1" || ls.Self.DNSName != "hub.example.ts.net" {
			t.Errorf("Self = %+v", ls.Self)
		}
		if got := fmt.Sprint(ls.Self.TailscaleIPs); got != "[100.64.0.1 fd7a:115c:a1e0::1]" {
			t.Errorf("Self.TailscaleIPs fallback = %s", got)
		}
		if ls.Self.UserLogin != "alice@example.com" {
			t.Errorf("Self.UserLogin = %q", ls.Self.UserLogin)
		}
		if len(ls.Health) != 1 || ls.Health[0] != "warning: something" {
			t.Errorf("Health = %v", ls.Health)
		}
		var names []string
		for _, p := range ls.Peers {
			names = append(names, p.DNSName)
		}
		if got := fmt.Sprint(names); got != "[alpha.example.ts.net shared.other.ts.net zeta.example.ts.net]" {
			t.Errorf("peer order = %s", got)
		}
		if ls.RunningLatest == nil || *ls.RunningLatest || ls.LatestVersion != "1.103.0" {
			t.Errorf("client version = %v/%q", ls.RunningLatest, ls.LatestVersion)
		}
	})

	t.Run("sorting is deterministic across iterations", func(t *testing.T) {
		st, _ := testStatus()
		first := MapStatus(st).Peers
		for i := 0; i < 20; i++ {
			if got := MapStatus(st).Peers; !reflect.DeepEqual(got, first) {
				t.Fatalf("iteration %d differs:\n got %+v\nwant %+v", i, got, first)
			}
		}
	})

	t.Run("self offline when backend not running", func(t *testing.T) {
		st, _ := testStatus()
		st.BackendState = "Stopped"
		st.Self.Online = true
		if MapStatus(st).Self.Online {
			t.Error("Self.Online should be false when BackendState != Running")
		}
	})

	t.Run("legacy suffix fallback and nil optionals", func(t *testing.T) {
		st, _ := testStatus()
		st.CurrentTailnet = nil
		st.ClientVersion = nil
		st.Self = nil
		st.Health = nil
		ls := MapStatus(st)
		if ls.MagicDNSSuffix != "legacy.ts.net" || ls.TailnetName != "" {
			t.Errorf("suffix = %q tailnet = %q", ls.MagicDNSSuffix, ls.TailnetName)
		}
		if ls.RunningLatest != nil || ls.LatestVersion != "" {
			t.Errorf("client version should be unset: %v %q", ls.RunningLatest, ls.LatestVersion)
		}
		if ls.Self.ID != "" || !ls.Self.Online {
			t.Errorf("Self with nil source = %+v", ls.Self)
		}
		if ls.Health != nil {
			t.Errorf("Health = %v, want nil", ls.Health)
		}
	})

	t.Run("empty suffix in current tailnet falls back", func(t *testing.T) {
		st, _ := testStatus()
		st.CurrentTailnet.MagicDNSSuffix = ""
		if got := MapStatus(st).MagicDNSSuffix; got != "legacy.ts.net" {
			t.Errorf("suffix = %q", got)
		}
	})

	t.Run("nil peer entries are skipped", func(t *testing.T) {
		st, _ := testStatus()
		st.Peer[key.NewNode().Public()] = nil
		if got := len(MapStatus(st).Peers); got != 3 {
			t.Errorf("peers = %d, want 3", got)
		}
	})

	t.Run("no peers yields nil slice", func(t *testing.T) {
		st, _ := testStatus()
		st.Peer = nil
		if got := MapStatus(st).Peers; got != nil {
			t.Errorf("Peers = %v, want nil", got)
		}
	})

	t.Run("nil status", func(t *testing.T) {
		ls := MapStatus(nil)
		if ls == nil || ls.BackendState != "" || ls.Peers != nil {
			t.Errorf("got %+v", ls)
		}
	})

	t.Run("health is cloned", func(t *testing.T) {
		st, _ := testStatus()
		ls := MapStatus(st)
		st.Health[0] = "mutated"
		if ls.Health[0] == "mutated" {
			t.Error("Health aliased the source slice")
		}
	})
}

func TestMapWhoIs(t *testing.T) {
	node := func(tags ...string) *tailcfg.Node {
		return &tailcfg.Node{
			ID:        7,
			StableID:  "nodeABC",
			Name:      "laptop.example.ts.net.",
			Addresses: []netip.Prefix{mustPrefix("100.64.0.9/32"), mustPrefix("fd7a:115c:a1e0::9/128")},
			Tags:      tags,
		}
	}
	profile := &tailcfg.UserProfile{ID: 1, LoginName: "alice@example.com", DisplayName: "Alice", ProfilePicURL: "https://pic/alice.png"}

	tests := []struct {
		name    string
		resp    *apitype.WhoIsResponse
		want    *source.WhoIs
		wantErr bool
	}{
		{name: "nil response", resp: nil, wantErr: true},
		{name: "nil node", resp: &apitype.WhoIsResponse{UserProfile: profile}, wantErr: true},
		{
			name: "user node",
			resp: &apitype.WhoIsResponse{Node: node(), UserProfile: profile},
			want: &source.WhoIs{
				NodeID: "nodeABC", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.9",
				LoginName: "alice@example.com", DisplayName: "Alice", ProfilePic: "https://pic/alice.png",
			},
		},
		{
			name: "tagged node without profile",
			resp: &apitype.WhoIsResponse{Node: node("tag:server")},
			want: &source.WhoIs{
				NodeID: "nodeABC", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.9",
				Tags: []string{"tag:server"}, IsTagged: true, LoginName: "tagged-device",
			},
		},
		{
			name: "tagged node with pseudo user profile",
			resp: &apitype.WhoIsResponse{
				Node:        node("tag:server", "tag:prod"),
				UserProfile: &tailcfg.UserProfile{ID: 2, LoginName: "tagged-devices", DisplayName: "Tagged Devices"},
			},
			want: &source.WhoIs{
				NodeID: "nodeABC", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.9",
				Tags: []string{"tag:server", "tag:prod"}, IsTagged: true,
				LoginName: "tagged-device", DisplayName: "Tagged Devices",
			},
		},
		{
			name: "tagged node with empty login profile",
			resp: &apitype.WhoIsResponse{Node: node("tag:x"), UserProfile: &tailcfg.UserProfile{}},
			want: &source.WhoIs{
				NodeID: "nodeABC", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.9",
				Tags: []string{"tag:x"}, IsTagged: true, LoginName: "tagged-device",
			},
		},
		{
			name: "tagged node with real user login is preserved",
			resp: &apitype.WhoIsResponse{Node: node("tag:x"), UserProfile: profile},
			want: &source.WhoIs{
				NodeID: "nodeABC", NodeName: "laptop.example.ts.net", NodeIP: "100.64.0.9",
				Tags: []string{"tag:x"}, IsTagged: true,
				LoginName: "alice@example.com", DisplayName: "Alice", ProfilePic: "https://pic/alice.png",
			},
		},
		{
			name: "node without addresses or trailing dot",
			resp: &apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "n2", Name: "bare"}, UserProfile: profile},
			want: &source.WhoIs{
				NodeID: "n2", NodeName: "bare",
				LoginName: "alice@example.com", DisplayName: "Alice", ProfilePic: "https://pic/alice.png",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MapWhoIs(tc.resp)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g, w := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", tc.want); g != w {
				t.Fatalf("\n got %s\nwant %s", g, w)
			}
		})
	}
}

func TestMapWhoIsCopiesTags(t *testing.T) {
	n := &tailcfg.Node{StableID: "n", Tags: []string{"tag:a"}}
	w, err := MapWhoIs(&apitype.WhoIsResponse{Node: n})
	if err != nil {
		t.Fatal(err)
	}
	n.Tags[0] = "tag:mutated"
	if w.Tags[0] != "tag:a" {
		t.Errorf("Tags aliased the node: %v", w.Tags)
	}
}

func TestMapPing(t *testing.T) {
	got := MapPing(&ipnstate.PingResult{
		LatencySeconds: 0.0123, Endpoint: "1.2.3.4:41641", DERPRegionID: 1,
		DERPRegionCode: "nyc", NodeName: "zeta", Err: "",
	})
	if got.LatencyMs < 12.29 || got.LatencyMs > 12.31 {
		t.Errorf("LatencyMs = %v, want 12.3", got.LatencyMs)
	}
	if got.Endpoint != "1.2.3.4:41641" || got.DERPRegionID != 1 || got.DERPRegionCode != "nyc" || got.NodeName != "zeta" {
		t.Errorf("got %+v", got)
	}
	if z := MapPing(nil); z != (source.PingReply{}) {
		t.Errorf("nil result = %+v, want zero", z)
	}

	// A peer-relayed reply has neither endpoint nor DERP region; it maps to
	// a relay via the peer relay label so it is not mistaken for a
	// classification-free reply.
	pr := MapPing(&ipnstate.PingResult{LatencySeconds: 0.01, PeerRelay: "203.0.113.5:41641:vni:7", NodeName: "zeta"})
	if pr.Endpoint != "" || pr.DERPRegionID != 0 || pr.DERPRegionCode != "peer-relay 203.0.113.5:41641:vni:7" {
		t.Errorf("peer relay reply = %+v", pr)
	}
	// A DERP reply that also names a peer relay keeps the DERP region.
	dr := MapPing(&ipnstate.PingResult{PeerRelay: "203.0.113.5:41641:vni:7", DERPRegionID: 1, DERPRegionCode: "nyc"})
	if dr.DERPRegionID != 1 || dr.DERPRegionCode != "nyc" {
		t.Errorf("derp reply with peer relay = %+v", dr)
	}
}

func TestClientStatus(t *testing.T) {
	st, _ := testStatus()
	tests := []struct {
		name       string
		socket     string
		err        error
		status     *ipnstate.Status
		wantIs     []error
		wantNotIs  []error
		wantSubstr []string
		hintCount  int
	}{
		{
			name:   "success",
			status: st,
		},
		{
			name:       "socket missing",
			socket:     "/var/run/tailscale/tailscaled.sock",
			err:        fmt.Errorf("dial unix /var/run/tailscale/tailscaled.sock: %w", syscall.ENOENT),
			wantIs:     []error{ErrDaemonUnavailable, syscall.ENOENT},
			wantNotIs:  []error{ErrPermissionDenied},
			wantSubstr: []string{"/var/run/tailscale/tailscaled.sock", "tslocal: status"},
			hintCount:  1,
		},
		{
			name:       "fs.ErrNotExist",
			err:        fs.ErrNotExist,
			wantIs:     []error{ErrDaemonUnavailable},
			wantSubstr: []string{"platform default socket"},
			hintCount:  1,
		},
		{
			name:      "connection refused",
			err:       fmt.Errorf("dial: %w", syscall.ECONNREFUSED),
			wantIs:    []error{ErrDaemonUnavailable, syscall.ECONNREFUSED},
			hintCount: 1,
		},
		{
			name:      "permission denied errno",
			err:       fmt.Errorf("dial unix: %w", syscall.EACCES),
			wantIs:    []error{ErrPermissionDenied, syscall.EACCES},
			wantNotIs: []error{ErrDaemonUnavailable},
			hintCount: 1,
		},
		{
			name:      "permission denied fs",
			err:       fs.ErrPermission,
			wantIs:    []error{ErrPermissionDenied},
			hintCount: 1,
		},
		{
			name:      "localapi access denied",
			err:       &local.AccessDeniedError{},
			wantIs:    []error{ErrPermissionDenied},
			hintCount: 1,
		},
		{
			name:      "hint is not repeated",
			err:       fmt.Errorf("failed (%s): %w", SocketHint, syscall.ENOENT),
			wantIs:    []error{ErrDaemonUnavailable},
			hintCount: 1,
		},
		{
			name:      "context canceled passes through",
			err:       context.Canceled,
			wantIs:    []error{context.Canceled},
			wantNotIs: []error{ErrDaemonUnavailable, ErrPermissionDenied},
			hintCount: 0,
		},
		{
			name:      "generic error is wrapped",
			err:       errors.New("boom"),
			wantNotIs: []error{ErrDaemonUnavailable, ErrPermissionDenied},
			wantSubstr: []string{
				"tslocal: status: boom",
			},
			hintCount: 0,
		},
		{
			name:   "nil status",
			status: nil,
			wantIs: []error{ErrEmptyResponse},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{status: func(context.Context) (*ipnstate.Status, error) {
				return tc.status, tc.err
			}}
			c := newTestClient(api, tc.socket)
			got, err := c.Status(context.Background())
			if tc.err == nil && tc.status != nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got == nil || len(got.Peers) != 3 || got.BackendState != "Running" {
					t.Fatalf("got %+v", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
			for _, w := range tc.wantIs {
				if !errors.Is(err, w) {
					t.Errorf("errors.Is(%v, %v) = false; err = %v", err, w, err)
				}
			}
			for _, w := range tc.wantNotIs {
				if errors.Is(err, w) {
					t.Errorf("errors.Is(%v, %v) = true, want false", err, w)
				}
			}
			for _, s := range tc.wantSubstr {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err, s)
				}
			}
			if n := strings.Count(err.Error(), SocketHint); n != tc.hintCount {
				t.Errorf("hint count = %d, want %d: %v", n, tc.hintCount, err)
			}
		})
	}
}

func TestClientStatusCanceledContext(t *testing.T) {
	api := &fakeAPI{status: func(context.Context) (*ipnstate.Status, error) {
		t.Fatal("api should not be called with a canceled context")
		return nil, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTestClient(api, "").Status(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestClientWhoIs(t *testing.T) {
	okResp := &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{StableID: "n1", Name: "laptop.example.ts.net.", Addresses: []netip.Prefix{mustPrefix("100.64.0.9/32")}},
		UserProfile: &tailcfg.UserProfile{LoginName: "alice@example.com", DisplayName: "Alice"},
	}
	tests := []struct {
		name      string
		addr      string
		resp      *apitype.WhoIsResponse
		err       error
		wantAddr  string // address the API must receive; "" = API must not be called
		wantIs    []error
		wantLogin string
	}{
		{name: "ip", addr: "100.64.0.9", resp: okResp, wantAddr: "100.64.0.9", wantLogin: "alice@example.com"},
		{name: "ip:port", addr: "100.64.0.9:51234", resp: okResp, wantAddr: "100.64.0.9:51234", wantLogin: "alice@example.com"},
		{name: "ipv6 bracketed", addr: "[fd7a:115c:a1e0::9]:443", resp: okResp, wantAddr: "[fd7a:115c:a1e0::9]:443", wantLogin: "alice@example.com"},
		{name: "ipv6 bare", addr: "fd7a:115c:a1e0::9", resp: okResp, wantAddr: "fd7a:115c:a1e0::9", wantLogin: "alice@example.com"},
		{name: "whitespace trimmed", addr: "  100.64.0.9 ", resp: okResp, wantAddr: "100.64.0.9", wantLogin: "alice@example.com"},
		{name: "empty", addr: "", wantAddr: "", wantIs: nil},
		{name: "hostname rejected", addr: "laptop.example.ts.net", wantAddr: ""},
		{name: "peer not found", addr: "100.64.0.99", err: local.ErrPeerNotFound, wantAddr: "100.64.0.99", wantIs: []error{source.ErrNotFound, local.ErrPeerNotFound}},
		{name: "daemon unavailable", addr: "100.64.0.9", err: syscall.ENOENT, wantAddr: "100.64.0.9", wantIs: []error{ErrDaemonUnavailable}},
		{name: "empty response", addr: "100.64.0.9", resp: &apitype.WhoIsResponse{}, wantAddr: "100.64.0.9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotAddr string
			api := &fakeAPI{whois: func(_ context.Context, addr string) (*apitype.WhoIsResponse, error) {
				gotAddr = addr
				return tc.resp, tc.err
			}}
			c := newTestClient(api, "")
			w, err := c.WhoIs(context.Background(), tc.addr)
			if tc.wantAddr == "" {
				if api.calls != 0 {
					t.Errorf("api called with %q, want no call", gotAddr)
				}
				if err == nil {
					t.Fatalf("expected error, got %+v", w)
				}
				return
			}
			if gotAddr != tc.wantAddr {
				t.Errorf("api received %q, want %q", gotAddr, tc.wantAddr)
			}
			if tc.wantLogin != "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if w.LoginName != tc.wantLogin || w.NodeName != "laptop.example.ts.net" || w.NodeIP != "100.64.0.9" {
					t.Errorf("got %+v", w)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got %+v", w)
			}
			for _, want := range tc.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("errors.Is(%v, %v) = false", err, want)
				}
			}
		})
	}
}

func TestClientPing(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		timeout     time.Duration
		res         *ipnstate.PingResult
		err         error
		wantCall    bool
		wantErr     bool
		wantIs      error
		wantReplErr string
		wantMs      float64
	}{
		{
			name: "direct reply", ip: "100.64.0.2", timeout: 2 * time.Second,
			res:      &ipnstate.PingResult{LatencySeconds: 0.005, Endpoint: "1.2.3.4:41641", NodeName: "zeta"},
			wantCall: true, wantMs: 5,
		},
		{
			name: "relayed reply", ip: "fd7a:115c:a1e0::2", timeout: 0,
			res:      &ipnstate.PingResult{LatencySeconds: 0.05, DERPRegionID: 1, DERPRegionCode: "nyc"},
			wantCall: true, wantMs: 50,
		},
		{
			name: "daemon-side error is returned in reply", ip: "100.64.0.3", timeout: time.Second,
			res:      &ipnstate.PingResult{Err: "no matching peer"},
			wantCall: true, wantReplErr: "no matching peer",
		},
		{
			name: "request failure is an error", ip: "100.64.0.3", timeout: time.Second,
			err:      fmt.Errorf("dial: %w", syscall.ENOENT),
			wantCall: true, wantErr: true, wantIs: ErrDaemonUnavailable,
		},
		{
			name: "deadline exceeded is an error", ip: "100.64.0.3", timeout: time.Second,
			err:      context.DeadlineExceeded,
			wantCall: true, wantErr: true, wantIs: context.DeadlineExceeded,
		},
		{
			name: "nil result", ip: "100.64.0.3", timeout: time.Second,
			wantCall: true, wantErr: true, wantIs: ErrEmptyResponse,
		},
		{name: "invalid ip", ip: "not-an-ip", timeout: time.Second, wantErr: true},
		{name: "ip with port rejected", ip: "100.64.0.2:80", timeout: time.Second, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotIP       netip.Addr
				gotType     tailcfg.PingType
				hadDeadline bool
				deadline    time.Time
			)
			before := time.Now()
			api := &fakeAPI{ping: func(ctx context.Context, ip netip.Addr, pt tailcfg.PingType) (*ipnstate.PingResult, error) {
				gotIP, gotType = ip, pt
				deadline, hadDeadline = ctx.Deadline()
				return tc.res, tc.err
			}}
			c := newTestClient(api, "")
			reply, err := c.Ping(context.Background(), tc.ip, tc.timeout)
			if (api.calls == 1) != tc.wantCall {
				t.Fatalf("api calls = %d, wantCall = %v", api.calls, tc.wantCall)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", reply)
				}
				if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
					t.Errorf("errors.Is(%v, %v) = false", err, tc.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotIP.String() != tc.ip || gotType != tailcfg.PingDisco {
				t.Errorf("api received ip=%v type=%q", gotIP, gotType)
			}
			if tc.timeout > 0 {
				if !hadDeadline {
					t.Error("context had no deadline")
				} else if d := deadline.Sub(before); d > tc.timeout+time.Second || d <= 0 {
					t.Errorf("deadline %v from start, want about %v", d, tc.timeout)
				}
			} else if hadDeadline {
				t.Error("context should have no deadline when timeout is 0")
			}
			if reply.Err != tc.wantReplErr {
				t.Errorf("reply.Err = %q, want %q", reply.Err, tc.wantReplErr)
			}
			if diff := reply.LatencyMs - tc.wantMs; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("LatencyMs = %v, want %v", reply.LatencyMs, tc.wantMs)
			}
			if tc.res != nil && (reply.Endpoint != tc.res.Endpoint || reply.DERPRegionCode != tc.res.DERPRegionCode || reply.DERPRegionID != tc.res.DERPRegionID || reply.NodeName != tc.res.NodeName) {
				t.Errorf("reply = %+v, source = %+v", reply, tc.res)
			}
		})
	}
}

func TestParseHostOrHostPort(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"100.64.0.1", "100.64.0.1", true},
		{"100.64.0.1:8080", "100.64.0.1", true},
		{"fd7a::1", "fd7a::1", true},
		{"[fd7a::1]:443", "fd7a::1", true},
		{"fd7a::1:443", "fd7a::1:443", true}, // ambiguous but a valid IPv6 literal
		{"host.example", "", false},
		{"", "", false},
		{"100.64.0.1:", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseHostOrHostPort(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, ok = %v", err, tc.ok)
			}
			if tc.ok && got.String() != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("custom socket", func(t *testing.T) {
		c := New("/tmp/ts.sock", nil)
		lc, ok := c.api.(*local.Client)
		if !ok {
			t.Fatalf("api is %T, want *local.Client", c.api)
		}
		if lc.Socket != "/tmp/ts.sock" || !lc.UseSocketOnly {
			t.Errorf("client = %+v", lc)
		}
		if c.log == nil {
			t.Error("nil logger not defaulted")
		}
	})
	t.Run("default socket", func(t *testing.T) {
		c := New("", slog.New(slog.NewTextHandler(io.Discard, nil)))
		lc := c.api.(*local.Client)
		if lc.Socket != "" || lc.UseSocketOnly {
			t.Errorf("client = %+v", lc)
		}
	})
}
