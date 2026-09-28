package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// baseTime is the fixed instant most tests run at (aligned to 15s).
var baseTime = time.Date(2026, 9, 28, 14, 30, 0, 0, time.UTC)

// clock is a settable fake clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newSim builds a Sim with a fake clock at the given instant.
func newSim(t testing.TB, seed int64, at time.Time) (*Sim, *clock) {
	if t != nil {
		t.Helper()
	}
	c := &clock{t: at}
	s := New(seed, testLogger())
	s.now = c.now
	return s, c
}

// dev finds a fleet member by current name. It does not take s.mu so it
// can be used by tests that already hold the lock; callers must not race it
// with admin mutations.
func (s *Sim) dev(t *testing.T, name string) *device {
	t.Helper()
	for _, d := range s.devs {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("no device named %q", name)
	return nil
}

func peerByName(t *testing.T, st *source.LocalStatus, name string) *source.LocalPeer {
	t.Helper()
	for i := range st.Peers {
		if strings.HasPrefix(st.Peers[i].DNSName, name+".") {
			return &st.Peers[i]
		}
	}
	return nil
}

func apiByName(t *testing.T, devs []source.APIDevice, name string) *source.APIDevice {
	t.Helper()
	for i := range devs {
		if strings.HasPrefix(devs[i].Name, name+".") {
			return &devs[i]
		}
	}
	return nil
}

func TestNewDefaults(t *testing.T) {
	s := New(1, nil)
	if s.log == nil || s.now == nil {
		t.Fatal("nil logger or clock")
	}
	if got := len(s.devs); got != 16 {
		t.Fatalf("fleet size = %d, want 16", got)
	}
	if s.self == nil || s.self.name != SelfName {
		t.Fatalf("self = %+v", s.self)
	}
	ips := s.DeviceIPs()
	if len(ips) != 16 {
		t.Fatalf("DeviceIPs len = %d, want 16", len(ips))
	}
	for i := 1; i <= 16; i++ {
		ip := fmt.Sprintf("100.64.0.%d", i)
		want := model.DeviceID(fmt.Sprintf("nDEMO%02dCNTRL", i))
		if got := ips[ip]; got != want {
			t.Errorf("DeviceIPs[%s] = %q, want %q", ip, got, want)
		}
	}
}

// reportString renders a report for equality checks.
func reportString(r *agentproto.Report) string {
	b, err := json.Marshal(r)
	if err != nil {
		return "marshal error: " + err.Error()
	}
	return string(b)
}

// snapshot captures everything the three interfaces return at one instant.
type snapshot struct {
	status  *source.LocalStatus
	devices []source.APIDevice
	reports map[string]string // ip -> report JSON-ish string or error text
	pings   map[string]source.PingReply
}

func snapshotOf(t *testing.T, s *Sim) snapshot {
	t.Helper()
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	devs, err := s.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	snap := snapshot{status: st, devices: devs, reports: map[string]string{}, pings: map[string]source.PingReply{}}
	for ip := range s.DeviceIPs() {
		rep, err := s.Fetch(ctx, ip, 0)
		if err != nil {
			snap.reports[ip] = "error: " + err.Error()
		} else {
			snap.reports[ip] = reportString(rep)
		}
		if pr, err := s.Ping(ctx, ip, 5*time.Second); err == nil {
			snap.pings[ip] = *pr
		}
	}
	return snap
}

func TestDeterminism(t *testing.T) {
	a, ca := newSim(t, 99, baseTime)
	b, cb := newSim(t, 99, baseTime)

	// Lockstep polls for a few minutes.
	for i := 0; i < 6; i++ {
		sa, sb := snapshotOf(t, a), snapshotOf(t, b)
		if !reflect.DeepEqual(sa.status, sb.status) {
			t.Fatalf("step %d: Status differs", i)
		}
		if !reflect.DeepEqual(sa.devices, sb.devices) {
			t.Fatalf("step %d: Devices differ", i)
		}
		if !reflect.DeepEqual(sa.reports, sb.reports) {
			t.Fatalf("step %d: agent reports differ", i)
		}
		if !reflect.DeepEqual(sa.pings, sb.pings) {
			t.Fatalf("step %d: pings differ", i)
		}
		ca.advance(15 * time.Second)
		cb.advance(15 * time.Second)
	}

	// A third Sim that never saw the intermediate instants must agree too:
	// the output is a function of (seed, time), not of call history.
	c, _ := newSim(t, 99, ca.now())
	sa, sc := snapshotOf(t, a), snapshotOf(t, c)
	if !reflect.DeepEqual(sa.status, sc.status) {
		t.Fatal("history-independent Status differs")
	}
	if !reflect.DeepEqual(sa.reports, sc.reports) {
		t.Fatal("history-independent agent reports differ")
	}

	// Different seeds produce different data.
	d, _ := newSim(t, 100, ca.now())
	sd := snapshotOf(t, d)
	if reflect.DeepEqual(sa.status, sd.status) {
		t.Fatal("different seeds produced identical Status")
	}
}

func TestFleetShape(t *testing.T) {
	s, _ := newSim(t, 5, baseTime)
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	devs, err := s.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 16 {
		t.Fatalf("Devices = %d, want 16", len(devs))
	}
	if len(st.Peers) != 14 {
		t.Fatalf("Peers = %d, want 14 (fleet minus self and the unauthorized node)", len(st.Peers))
	}
	if st.Self.DNSName != "tailwatch-hub."+MagicDNSSuffix || !st.Self.Online || len(st.Self.Tags) != 1 || st.Self.UserLogin != "" {
		t.Errorf("Self = %+v", st.Self)
	}
	if st.TailnetName != TailnetName || st.MagicDNSSuffix != MagicDNSSuffix || st.BackendState != "Running" {
		t.Errorf("status header = %+v", *st)
	}
	if s.Tailnet() != APITailnet || !s.Configured() {
		t.Errorf("Tailnet()/Configured() = %q/%v", s.Tailnet(), s.Configured())
	}

	tests := []struct {
		name  string
		check func(t *testing.T, p *source.LocalPeer, a *source.APIDevice)
	}{
		{"nas", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.UserLogin != "" || len(p.Tags) != 1 || p.Tags[0] != "tag:server" {
				t.Errorf("nas should be tagged with no user: %+v", p)
			}
			if !p.Online || p.CurAddr == "" || !a.KeyExpiryDisabled || a.Expires != nil {
				t.Errorf("nas online/direct/key-expiry-disabled: %+v %+v", p, a)
			}
			rep, err := s.Fetch(ctx, p.TailscaleIPs[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			big := 0
			for _, d := range rep.Disks {
				if d.Total >= 8<<40 && !d.Primary {
					big++
				}
			}
			if big != 2 {
				t.Errorf("nas big disks = %d, want 2 (%+v)", big, rep.Disks)
			}
		}},
		{"pi-hole", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			rep, err := s.Fetch(ctx, p.TailscaleIPs[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Host.Arch != "arm64" || len(rep.Temperatures) == 0 {
				t.Errorf("pi-hole arch/temps: %+v %+v", rep.Host, rep.Temperatures)
			}
			if rep.Temperatures[0].Celsius < 35 || rep.Temperatures[0].Celsius > 75 {
				t.Errorf("pi temp = %v", rep.Temperatures[0].Celsius)
			}
		}},
		{"homelab", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if !reflect.DeepEqual(p.PrimaryRoutes, []string{"192.168.1.0/24"}) || !p.ExitNodeOpt || p.ExitNode {
				t.Errorf("homelab routes/exit: %+v", p)
			}
			if len(a.AdvertisedRoutes) != 3 || len(a.EnabledRoutes) != 3 {
				t.Errorf("homelab api routes: %+v", a)
			}
		}},
		{"cloud-vm", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.Relay != "fra" || p.CurAddr != "" || !p.ExitNode || !p.ExitNodeOpt {
				t.Errorf("cloud-vm relay/exit: %+v", p)
			}
			if a.PreferredDERP != "fra" || len(a.DERPLatencyMs) < 3 || len(a.DERPLatencyMs) > 4 {
				t.Errorf("cloud-vm derp: %+v", a)
			}
			if p.Location == nil || p.Location.CountryCode != "DE" {
				t.Errorf("cloud-vm location: %+v", p.Location)
			}
		}},
		{"alice-mbp", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.OS != "macOS" || p.CurAddr == "" || p.UserLogin != "alice@example.com" || p.UserDisplay == "" {
				t.Errorf("alice-mbp: %+v", p)
			}
		}},
		{"alice-iphone", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.OS != "iOS" || p.Relay != "nyc" || p.CurAddr != "" {
				t.Errorf("alice-iphone: %+v", p)
			}
		}},
		{"alice-ipad", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.Online || baseTime.Sub(p.LastSeen) < 3*day || !a.BlocksIncoming {
				t.Errorf("alice-ipad should be offline for days: online=%v lastSeen=%v", p.Online, p.LastSeen)
			}
			if p.RxBytes != 0 && p.TxBytes != 0 && baseTime.Sub(p.LastSeen) < 3*day {
				t.Errorf("ipad counters: %d/%d", p.RxBytes, p.TxBytes)
			}
		}},
		{"bob-desktop", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.OS != "windows" || p.CurAddr == "" || p.UserLogin != "bob@example.com" {
				t.Errorf("bob-desktop: %+v", p)
			}
		}},
		{"bob-pixel", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.OS != "android" || p.Relay != "sfo" || p.CurAddr != "" {
				t.Errorf("bob-pixel: %+v", p)
			}
		}},
		{"office-printer-gw", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if p.KeyExpiry == nil || a.Expires == nil {
				t.Fatalf("printer key expiry missing")
			}
			if until := p.KeyExpiry.Sub(baseTime); until < 60*time.Hour || until > 84*time.Hour {
				t.Errorf("printer key expires in %v, want ~3d", until)
			}
			if a.KeyExpiryDisabled {
				t.Error("printer key expiry should not be disabled")
			}
		}},
		{"old-laptop", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if a.ClientVersion != OldClientVersion || !a.UpdateAvailable || p.OS != "windows" {
				t.Errorf("old-laptop: %+v", a)
			}
		}},
		{"shared-node", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			if !a.IsExternal || !p.ShareeNode || p.UserLogin != "carol@othercorp.example" {
				t.Errorf("shared-node: %+v %+v", p, a)
			}
		}},
		{"media-box", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			rep, err := s.Fetch(ctx, p.TailscaleIPs[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Disks[0].Percent < 95 || rep.Disks[0].Percent > 97 || !rep.Disks[0].Primary {
				t.Errorf("media-box disk = %+v", rep.Disks[0])
			}
		}},
		{"dev-box", func(t *testing.T, p *source.LocalPeer, a *source.APIDevice) {
			rep, err := s.Fetch(ctx, p.TailscaleIPs[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Host.OS != "freebsd" || rep.Memory.Percent < 88 || rep.Memory.Percent > 97 {
				t.Errorf("dev-box: %+v mem=%v", rep.Host, rep.Memory.Percent)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := peerByName(t, st, tc.name)
			a := apiByName(t, devs, tc.name)
			if p == nil || a == nil {
				t.Fatalf("%s: peer=%v api=%v", tc.name, p != nil, a != nil)
			}
			if p.ID != a.NodeID || p.DNSName != a.Name || !reflect.DeepEqual(p.TailscaleIPs, a.Addresses) {
				t.Errorf("local/api identity mismatch: %+v vs %+v", p, a)
			}
			tc.check(t, p, a)
		})
	}

	t.Run("workshop-pi", func(t *testing.T) {
		if peerByName(t, st, "workshop-pi") != nil {
			t.Error("unauthorized workshop-pi must not be in the netmap")
		}
		a := apiByName(t, devs, "workshop-pi")
		if a == nil || a.Authorized {
			t.Fatalf("workshop-pi api = %+v", a)
		}
	})

	t.Run("tailwatch-hub", func(t *testing.T) {
		a := apiByName(t, devs, "tailwatch-hub")
		if a == nil || a.NodeID != st.Self.ID || !a.KeyExpiryDisabled {
			t.Fatalf("hub api = %+v", a)
		}
	})
}

func TestWhoIs(t *testing.T) {
	s, _ := newSim(t, 1, baseTime)
	ctx := context.Background()
	for _, remote := range []string{"", "garbage", "100.64.0.2:51234", "[fd7a:115c:a1e0::9]:443", "10.1.1.1:80"} {
		t.Run(remote, func(t *testing.T) {
			w, err := s.WhoIs(ctx, remote)
			if err != nil {
				t.Fatal(err)
			}
			if w.LoginName != WhoIsLogin || w.DisplayName != WhoIsDisplay || w.IsTagged || len(w.Tags) != 0 {
				t.Errorf("identity = %+v", w)
			}
			if w.NodeName != WhoIsNode+"."+MagicDNSSuffix || w.NodeIP != "100.64.0.6" || w.NodeID != "nDEMO06CNTRL" {
				t.Errorf("node = %s/%s/%s, want alice-mbp", w.NodeName, w.NodeIP, w.NodeID)
			}
		})
	}
	// Renaming the node keeps the attribution.
	if err := s.SetName(ctx, "nDEMO06CNTRL", "alice-laptop"); err != nil {
		t.Fatal(err)
	}
	w, err := s.WhoIs(ctx, "")
	if err != nil || w.NodeID != "nDEMO06CNTRL" {
		t.Fatalf("after rename: %+v %v", w, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.WhoIs(cctx, ""); err == nil {
		t.Error("WhoIs with cancelled context should fail")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s, c := newSim(t, 11, baseTime)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				switch (i + j) % 5 {
				case 0:
					if _, err := s.Status(ctx); err != nil {
						t.Error(err)
					}
				case 1:
					if _, err := s.Devices(ctx); err != nil {
						t.Error(err)
					}
				case 2:
					_, _ = s.Fetch(ctx, "100.64.0.2", 0)
				case 3:
					_, _ = s.Ping(ctx, "100.64.0.7", time.Second)
				case 4:
					c.advance(time.Second)
					_ = s.SetAuthorized(ctx, "nDEMO13CNTRL", j%2 == 0)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestContextCancelled(t *testing.T) {
	s, _ := newSim(t, 1, baseTime)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Status(ctx); err == nil {
		t.Error("Status should fail")
	}
	if _, err := s.Devices(ctx); err == nil {
		t.Error("Devices should fail")
	}
	if _, err := s.Fetch(ctx, "100.64.0.2", 0); err == nil {
		t.Error("Fetch should fail")
	}
	if _, err := s.Ping(ctx, "100.64.0.2", 0); err == nil {
		t.Error("Ping should fail")
	}
	if err := s.SetAuthorized(ctx, "nDEMO13CNTRL", true); err == nil {
		t.Error("SetAuthorized should fail")
	}
}
