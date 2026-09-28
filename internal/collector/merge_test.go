package collector

import (
	"slices"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestMergePrecedence(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.api.configured = true
	expires := baseTime.Add(30 * 24 * time.Hour)
	mapping := false
	h.api.setDevices(source.APIDevice{
		ID: "123", NodeID: "p1", Name: "laptop.tail.ts.net.", Hostname: "laptop-from-api", User: "bob@example.com",
		OS: "macos-from-api", ClientVersion: "1.84.0", UpdateAvailable: true,
		Addresses: []string{"100.99.99.99"}, Created: baseTime.Add(-60 * 24 * time.Hour), LastSeen: baseTime,
		Expires: &expires, Authorized: true, IsExternal: false, Tags: []string{"tag:api"},
		AdvertisedRoutes: []string{"10.0.0.0/24", "0.0.0.0/0"}, EnabledRoutes: []string{"10.0.0.0/24"},
		BlocksIncoming: true, Endpoints: []string{"1.2.3.4:41641"},
		DERPLatencyMs: map[string]float64{"nyc": 12.5}, PreferredDERP: "nyc",
		NATSupport: &model.NATSupport{UDP: true, UPnP: true}, MappingVariesByDestIP: &mapping,
	})
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2",
		ips("fd7a:115c:a1e0::2", "100.64.0.2"), hostname("Laptop-Local"), osName("macOS"),
		curAddr("1.2.3.4:41641"), relay("nyc"), bytes(1000, 2000), primaryRoutes("10.0.0.0/24"))))
	h.poll()

	d := h.device("p1")
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Name", d.Name, "laptop"},
		{"DNSName", d.DNSName, "laptop.tail.ts.net"},
		{"Hostname (local wins)", d.Hostname, "Laptop-Local"},
		{"OS (local wins)", d.OS, "macOS"},
		{"Addresses v4 first (local wins)", d.Addresses, []string{"100.64.0.2", "fd7a:115c:a1e0::2"}},
		{"User (local wins)", d.User, "alice@example.com"},
		{"Tags (local wins)", d.Tags, []string{}},
		{"Online", d.Online, true},
		{"Path direct from CurAddr", d.Connectivity.Path, model.PathDirect},
		{"Relay empty when direct", d.Connectivity.Relay, ""},
		{"CurAddr", d.Connectivity.CurAddr, "1.2.3.4:41641"},
		{"RxBytes", d.Connectivity.RxBytes, int64(1000)},
		{"TxBytes", d.Connectivity.TxBytes, int64(2000)},
		{"ClientVersion (api)", d.ClientVersion, "1.84.0"},
		{"UpdateAvailable (api)", d.UpdateAvailable, true},
		{"Authorized (api)", d.Authorized, true},
		{"KeyExpiry (api expires)", d.KeyExpiry != nil && d.KeyExpiry.Equal(expires), true},
		{"Expired", d.Expired, false},
		{"AdvertisedRoutes (api)", d.AdvertisedRoutes, []string{"10.0.0.0/24", "0.0.0.0/0"}},
		{"EnabledRoutes (api)", d.EnabledRoutes, []string{"10.0.0.0/24"}},
		{"PrimaryRoutes (local)", d.PrimaryRoutes, []string{"10.0.0.0/24"}},
		{"ExitNodeOption (no default enabled route)", d.ExitNodeOption, false},
		{"BlocksIncoming (api)", d.BlocksIncoming, true},
		{"Endpoints (api)", d.Connectivity.Endpoints, []string{"1.2.3.4:41641"}},
		{"PreferredDERP (api)", d.Connectivity.PreferredDERP, "nyc"},
		{"DERP latency (api)", d.Connectivity.DERPLatencyMs["nyc"], 12.5},
		{"NATSupport (api)", d.Connectivity.NATSupport != nil && d.Connectivity.NATSupport.UDP, true},
		{"MappingVaries (api)", d.Connectivity.MappingVariesByDestIP != nil && !*d.Connectivity.MappingVariesByDestIP, true},
		{"Created (api)", d.Created, baseTime.Add(-60 * 24 * time.Hour)},
		{"FirstSeen now", d.FirstSeen, baseTime},
		{"LastSeen now when online", d.LastSeen, baseTime},
		{"Rates zero on first poll", d.Connectivity.RxRate + d.Connectivity.TxRate, 0.0},
		{"Agent disabled? no: unreachable (no report)", d.Agent.State, model.AgentUnreachable},
	}
	for _, c := range checks {
		if !equalAny(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	self := h.device("self")
	if !self.IsSelf || !self.Online || self.Connectivity.Path != model.PathNone || self.ClientVersion != "1.86.0" {
		t.Errorf("self device: %+v", self)
	}
	if self.Name != "hub" || self.Addresses[0] != "100.64.0.1" {
		t.Errorf("self identity: name=%q addrs=%v", self.Name, self.Addresses)
	}

	hub := h.c.Hub()
	if hub.Tailnet != "example.com" || hub.SelfName != "hub" || hub.SelfID != "self" || !hub.ControlAPI ||
		hub.MagicDNSSuffix != "tail.ts.net" || hub.TailscaleVer != "1.86.0" || hub.BackendState != "Running" ||
		hub.LastAPIPoll == nil || !hub.LastAPIPoll.Equal(baseTime) || !hub.LastPoll.Equal(baseTime) || hub.LastError != "" ||
		hub.PollIntervalSec != 15 || hub.Version != "test" || !slices.Equal(hub.SelfIPs, []string{"100.64.0.1", "fd7a:115c:a1e0::1"}) {
		t.Errorf("hub info: %+v", hub)
	}
}

func TestAPIOnlyDevice(t *testing.T) {
	cases := []struct {
		name       string
		lastSeen   time.Time
		wantOnline bool
		wantPath   model.PathType
	}{
		{"seen 1 minute ago", baseTime.Add(-time.Minute), true, model.PathUnknown},
		{"seen 10 minutes ago", baseTime.Add(-10 * time.Minute), false, model.PathNone},
		{"never seen", time.Time{}, false, model.PathNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, defaultCfg())
			h.api.configured = true
			h.api.setDevices(source.APIDevice{
				NodeID: "hidden", Name: "hidden.tail.ts.net", Hostname: "hidden-host", User: "carol@example.com",
				OS: "windows", ClientVersion: "1.80.0", Addresses: []string{"fd7a::9", "100.64.0.9"}, LastSeen: tc.lastSeen,
				Authorized: true, Tags: []string{"tag:hidden"}, EnabledRoutes: []string{"0.0.0.0/0", "::/0"},
			})
			h.poll()
			d := h.device("hidden")
			if d.Online != tc.wantOnline || d.Connectivity.Path != tc.wantPath {
				t.Fatalf("online=%v path=%v, want %v/%v", d.Online, d.Connectivity.Path, tc.wantOnline, tc.wantPath)
			}
			if d.Name != "hidden" || d.Hostname != "hidden-host" || d.OS != "windows" || d.User != "carol@example.com" ||
				!slices.Equal(d.Tags, []string{"tag:hidden"}) || !slices.Equal(d.Addresses, []string{"100.64.0.9", "fd7a::9"}) {
				t.Errorf("api-only fields: %+v", d)
			}
			if !d.ExitNodeOption {
				t.Errorf("default enabled route should mark exit node option")
			}
			if isSubnetRouter(&d) {
				t.Errorf("default routes alone must not count as subnet router")
			}
			if d.Agent.State != model.AgentUnknown {
				t.Errorf("agent state for api-only device: %v", d.Agent.State)
			}
			if h.agents.callCount("100.64.0.9") != 0 {
				t.Errorf("api-only devices must not be fetched from")
			}
		})
	}
}

func TestRates(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2", bytes(1000, 2000))))
	h.poll()
	if d := h.device("p1"); d.Connectivity.RxRate != 0 || d.Connectivity.TxRate != 0 {
		t.Fatalf("first poll rates: %v/%v", d.Connectivity.RxRate, d.Connectivity.TxRate)
	}

	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2", bytes(2000, 2500))))
	h.pollAfter(10 * time.Second)
	d := h.device("p1")
	if d.Connectivity.RxRate != 100 || d.Connectivity.TxRate != 50 {
		t.Fatalf("rates after 10s: rx=%v tx=%v, want 100/50", d.Connectivity.RxRate, d.Connectivity.TxRate)
	}
	sm, err := h.st.LatestSample(t.Context(), "p1")
	if err != nil {
		t.Fatalf("latest sample: %v", err)
	}
	if sm.TSRxRate != 100 || sm.TSTxRate != 50 || sm.TSRxBytes != 2000 || sm.TSTxBytes != 2500 || !sm.Online {
		t.Errorf("sample: %+v", sm)
	}
	if snap := h.c.Snapshot(); snap.Overview.TotalRxRate != 100 || snap.Overview.TotalTxRate != 50 || snap.Overview.TotalRxBytes != 2000 {
		t.Errorf("overview totals: %+v", snap.Overview)
	}

	// Counter reset (tailscaled restart) must not produce a negative rate.
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2", bytes(500, 100))))
	h.pollAfter(10 * time.Second)
	if d := h.device("p1"); d.Connectivity.RxRate != 0 || d.Connectivity.TxRate != 0 {
		t.Fatalf("rates after reset: rx=%v tx=%v, want 0/0", d.Connectivity.RxRate, d.Connectivity.TxRate)
	}
	if self := h.device("self"); self.Connectivity.RxRate != 0 || self.Connectivity.RxBytes != 0 {
		t.Errorf("self must have zero rates and bytes")
	}
}

func TestPathDerivationAndPings(t *testing.T) {
	cases := []struct {
		name      string
		opts      []peerOpt
		ping      *source.PingReply
		pingErr   bool
		wantPath  model.PathType
		wantRelay string
		wantLat   float64 // -1 = nil
		wantCur   string
	}{
		{"offline → none", []peerOpt{offline(), relay("nyc")}, nil, false, model.PathNone, "", -1, ""},
		{"curaddr → direct even with home relay", []peerOpt{curAddr("5.6.7.8:1"), relay("nyc")}, nil, false, model.PathDirect, "", -1, "5.6.7.8:1"},
		{"relay only → relay", []peerOpt{relay("fra")}, nil, false, model.PathRelay, "fra", -1, ""},
		{"online without either → unknown", nil, nil, false, model.PathUnknown, "", -1, ""},
		{"ping direct refines relay", []peerOpt{relay("nyc")}, &source.PingReply{LatencyMs: 8.5, Endpoint: "9.9.9.9:41641"}, false, model.PathDirect, "", 8.5, "9.9.9.9:41641"},
		{"ping via derp refines direct", []peerOpt{curAddr("5.6.7.8:1")}, &source.PingReply{LatencyMs: 40, DERPRegionID: 4, DERPRegionCode: "fra"}, false, model.PathRelay, "fra", 40, "5.6.7.8:1"},
		{"ping reply error keeps state", []peerOpt{relay("nyc")}, &source.PingReply{Err: "timeout"}, false, model.PathRelay, "nyc", -1, ""},
		{"ping transport error keeps state", []peerOpt{relay("nyc")}, nil, true, model.PathRelay, "nyc", -1, ""},
		{"ping without endpoint or derp keeps derived path", []peerOpt{relay("nyc")}, &source.PingReply{LatencyMs: 3}, false, model.PathRelay, "nyc", 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultCfg()
			cfg.PingInterval = 30 * time.Second
			h := newHarness(t, cfg)
			h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2", tc.opts...)))
			if tc.ping != nil {
				h.local.setPing("100.64.0.2", *tc.ping)
			}
			if tc.pingErr {
				h.local.setPingErr("100.64.0.2", errTest)
			}
			h.poll()
			d := h.device("p1")
			if d.Connectivity.Path != tc.wantPath || d.Connectivity.Relay != tc.wantRelay {
				t.Errorf("path=%v relay=%q, want %v/%q", d.Connectivity.Path, d.Connectivity.Relay, tc.wantPath, tc.wantRelay)
			}
			if got := floatVal(d.Connectivity.LatencyMs); got != tc.wantLat {
				t.Errorf("latency=%v, want %v", got, tc.wantLat)
			}
			if d.Connectivity.CurAddr != tc.wantCur {
				t.Errorf("curAddr=%q, want %q", d.Connectivity.CurAddr, tc.wantCur)
			}
			if tc.wantLat >= 0 && (d.Connectivity.LastPing == nil || !d.Connectivity.LastPing.Equal(baseTime)) {
				t.Errorf("LastPing not recorded")
			}
			if d.Online && h.local.pingsTo("100.64.0.2") != 1 {
				t.Errorf("online peer pinged %d times, want 1", h.local.pingsTo("100.64.0.2"))
			}
			if !d.Online && h.local.pingsTo("100.64.0.2") != 0 {
				t.Errorf("offline peer must not be pinged")
			}
			if h.local.pingsTo("100.64.0.1") != 0 {
				t.Errorf("self must not be pinged")
			}
			sm, err := h.st.LatestSample(t.Context(), "p1")
			if err != nil {
				t.Fatalf("latest sample: %v", err)
			}
			if got := floatVal(sm.LatencyMs); got != tc.wantLat {
				t.Errorf("sample latency=%v, want %v", got, tc.wantLat)
			}
			switch tc.wantPath {
			case model.PathDirect:
				if sm.Direct == nil || !*sm.Direct || sm.Relay != "" {
					t.Errorf("sample direct flag: %+v", sm)
				}
			case model.PathRelay:
				if sm.Direct == nil || *sm.Direct || sm.Relay != tc.wantRelay {
					t.Errorf("sample relay flag: %+v", sm)
				}
			default:
				if sm.Direct != nil {
					t.Errorf("sample direct must be nil for %v", tc.wantPath)
				}
			}
		})
	}
}

func TestPingScheduleAndConcurrency(t *testing.T) {
	cfg := defaultCfg()
	cfg.PingInterval = 30 * time.Second
	cfg.PingConcurrency = 2
	h := newHarness(t, cfg)
	var peers []source.LocalPeer
	for i := 2; i < 8; i++ {
		ip := "100.64.0." + string(rune('0'+i))
		peers = append(peers, peer("p"+string(rune('0'+i)), "dev"+string(rune('0'+i)), ip))
		h.local.setPing(ip, source.PingReply{LatencyMs: float64(i)})
	}
	h.local.setStatus(hubStatus(peers...))
	h.poll()
	if got := len(h.local.pingCalls); got != 6 {
		t.Fatalf("first poll pings=%d, want 6", got)
	}
	if h.local.maxFlight > 2 {
		t.Errorf("max concurrent pings %d exceeds semaphore 2", h.local.maxFlight)
	}
	h.local.resetPings()
	h.pollAfter(15 * time.Second)
	if got := len(h.local.pingCalls); got != 0 {
		t.Errorf("poll at 15s pinged %d times, want 0", got)
	}
	h.pollAfter(15 * time.Second)
	if got := len(h.local.pingCalls); got != 6 {
		t.Errorf("poll at 30s pinged %d times, want 6", got)
	}
	// Latency is carried between pings but only sampled when pinged.
	d := h.device("p2")
	if floatVal(d.Connectivity.LatencyMs) != 2 {
		t.Errorf("latency carried: %v", floatVal(d.Connectivity.LatencyMs))
	}
	h.local.resetPings()
	h.pollAfter(15 * time.Second)
	sm, err := h.st.LatestSample(t.Context(), "p2")
	if err != nil {
		t.Fatalf("latest sample: %v", err)
	}
	if sm.LatencyMs != nil {
		t.Errorf("sample on a non-ping poll must not carry latency")
	}
	if d := h.device("p2"); floatVal(d.Connectivity.LatencyMs) != 2 {
		t.Errorf("device latency must persist between pings")
	}

	t.Run("disabled", func(t *testing.T) {
		h := newHarness(t, defaultCfg()) // PingInterval 0
		h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
		h.poll()
		h.pollAfter(time.Minute)
		if len(h.local.pingCalls) != 0 {
			t.Errorf("pings must be disabled with PingInterval 0")
		}
	})
}

func TestControlAPISchedule(t *testing.T) {
	h := newHarness(t, defaultCfg())
	h.api.configured = true
	h.api.setDevices(source.APIDevice{NodeID: "p1", Name: "laptop.tail.ts.net", ClientVersion: "1.80.0", Authorized: true})
	h.local.setStatus(hubStatus(peer("p1", "laptop", "100.64.0.2")))
	h.poll()
	if h.api.callCount() != 1 {
		t.Fatalf("api calls after first poll=%d, want 1", h.api.callCount())
	}
	h.pollAfter(15 * time.Second)
	h.pollAfter(15 * time.Second)
	if h.api.callCount() != 1 {
		t.Errorf("api polled before interval: %d", h.api.callCount())
	}
	h.pollAfter(30 * time.Second)
	if h.api.callCount() != 2 {
		t.Errorf("api not polled at interval: %d", h.api.callCount())
	}

	// Failures keep the cached data and surface in LastError.
	h.api.setErr(errTest)
	h.pollAfter(60 * time.Second)
	if h.api.callCount() != 3 {
		t.Errorf("api calls=%d, want 3", h.api.callCount())
	}
	if d := h.device("p1"); d.ClientVersion != "1.80.0" {
		t.Errorf("cached api data lost on error: %+v", d)
	}
	hub := h.c.Hub()
	if hub.LastError == "" || hub.LastAPIPoll == nil || !hub.LastAPIPoll.Equal(baseTime.Add(60*time.Second)) {
		t.Errorf("hub after api failure: %+v", hub)
	}
	h.api.setErr(nil)
	h.pollAfter(60 * time.Second)
	if hub := h.c.Hub(); hub.LastError != "" {
		t.Errorf("LastError not cleared after recovery: %q", hub.LastError)
	}

	t.Run("unconfigured api is never called", func(t *testing.T) {
		h := newHarness(t, defaultCfg())
		h.api.configured = false
		h.poll()
		if h.api.callCount() != 0 || h.c.Hub().ControlAPI {
			t.Errorf("unconfigured api used")
		}
	})
	t.Run("nil api", func(t *testing.T) {
		st := newHarness(t, defaultCfg()).st
		c := New(defaultCfg(), st, &fakeLocal{status: hubStatus()}, nil, &fakeAgents{}, quietLogger())
		if err := c.PollOnce(t.Context()); err != nil {
			t.Fatalf("poll with nil api: %v", err)
		}
		if c.Hub().ControlAPI {
			t.Errorf("ControlAPI must be false with nil api")
		}
	})
}

func TestNameFallbacks(t *testing.T) {
	cases := []struct {
		dns, host, id, want string
	}{
		{"laptop.tail.ts.net", "Laptop", "id", "laptop"},
		{"laptop.tail.ts.net.", "Laptop", "id", "laptop"},
		{"", "Laptop", "id", "Laptop"},
		{"", "", "id", "id"},
		{".", "  ", "id", "id"},
	}
	for _, tc := range cases {
		if got := baseName(tc.dns, tc.host, tc.id); got != tc.want {
			t.Errorf("baseName(%q,%q,%q)=%q, want %q", tc.dns, tc.host, tc.id, got, tc.want)
		}
	}
}

func TestExpiryAndTaggedUser(t *testing.T) {
	h := newHarness(t, defaultCfg())
	past := baseTime.Add(-time.Hour)
	h.local.setStatus(hubStatus(
		peer("p1", "srv", "100.64.0.2", tags("tag:server"), user("tagged-devices")),
		peer("p2", "old", "100.64.0.3", keyExpiry(past)),
	))
	h.poll()
	if d := h.device("p1"); d.User != "" || !slices.Equal(d.Tags, []string{"tag:server"}) {
		t.Errorf("tagged device user must be empty: %+v", d)
	}
	if d := h.device("p2"); !d.Expired {
		t.Errorf("device with past key expiry must be expired")
	}
}
