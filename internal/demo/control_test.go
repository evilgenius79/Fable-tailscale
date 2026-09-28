package demo

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestDevicesFields(t *testing.T) {
	s, _ := newSim(t, 51, baseTime)
	ctx := context.Background()
	devs, err := s.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range devs {
		if seen[d.ID] || seen[string(d.NodeID)] {
			t.Errorf("duplicate id %s/%s", d.ID, d.NodeID)
		}
		seen[d.ID], seen[string(d.NodeID)] = true, true
		if d.ID == "" || d.NodeID == "" || d.Hostname == "" || d.OS == "" || d.ClientVersion == "" {
			t.Errorf("incomplete device %+v", d)
		}
		if len(d.DERPLatencyMs) < 3 || len(d.DERPLatencyMs) > 4 {
			t.Errorf("%s derp regions = %v", d.Name, d.DERPLatencyMs)
		}
		if _, ok := d.DERPLatencyMs[d.PreferredDERP]; !ok {
			t.Errorf("%s preferred derp %q not in %v", d.Name, d.PreferredDERP, d.DERPLatencyMs)
		}
		for code, ms := range d.DERPLatencyMs {
			if _, ok := derpRegionIDs[code]; !ok || ms <= 0 {
				t.Errorf("%s derp %s=%v", d.Name, code, ms)
			}
			if ms < d.DERPLatencyMs[d.PreferredDERP] {
				t.Errorf("%s preferred %s is not the lowest (%s=%v)", d.Name, d.PreferredDERP, code, ms)
			}
		}
		if d.NATSupport == nil || d.MappingVariesByDestIP == nil {
			t.Errorf("%s nat fields missing", d.Name)
		}
		if d.LastSeen.After(baseTime) || d.Created.After(d.LastSeen) {
			t.Errorf("%s times: created %v lastSeen %v", d.Name, d.Created, d.LastSeen)
		}
		if d.KeyExpiryDisabled != (d.Expires == nil) {
			t.Errorf("%s expiry: disabled=%v expires=%v", d.Name, d.KeyExpiryDisabled, d.Expires)
		}
		if len(d.Tags) > 0 && d.User != "" {
			t.Errorf("%s tagged device has user %q", d.Name, d.User)
		}
	}
	// Servers have key expiry disabled; the shared node is external.
	for _, name := range []string{"tailwatch-hub", "nas", "homelab", "cloud-vm"} {
		if a := apiByName(t, devs, name); a == nil || !a.KeyExpiryDisabled {
			t.Errorf("%s should have key expiry disabled", name)
		}
	}
	if a := apiByName(t, devs, "old-laptop"); a == nil || a.ClientVersion != OldClientVersion || !a.UpdateAvailable {
		t.Errorf("old-laptop = %+v", a)
	}
}

func TestAdminMutations(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(t *testing.T, s *Sim)
	}{
		{"authorize adds to netmap", func(t *testing.T, s *Sim) {
			d := s.dev(t, "workshop-pi")
			if err := s.SetAuthorized(ctx, d.legacyID, true); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			p := peerByName(t, st, "workshop-pi")
			if p == nil || !p.Online {
				t.Fatalf("workshop-pi not an online peer after authorize: %+v", p)
			}
			devs, _ := s.Devices(ctx)
			if a := apiByName(t, devs, "workshop-pi"); a == nil || !a.Authorized {
				t.Fatalf("api authorized = %+v", a)
			}
			if r, err := s.Ping(ctx, d.ip4, 0); err != nil || r.Err != "" {
				t.Fatalf("ping after authorize: %v %+v", err, r)
			}
			// Its agent still rejects the hub: that is a separate problem.
			var se interface{ Unwrap() error }
			if _, err := s.Fetch(ctx, d.ip4, 0); err == nil || !errors.As(err, &se) {
				t.Fatalf("fetch after authorize = %v", err)
			}
		}},
		{"deauthorize removes from netmap", func(t *testing.T, s *Sim) {
			d := s.dev(t, "nas")
			if err := s.SetAuthorized(ctx, string(d.id), false); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			if peerByName(t, st, "nas") != nil {
				t.Fatal("nas still in netmap")
			}
			if _, err := s.Fetch(ctx, d.ip4, 0); err == nil {
				t.Fatal("fetch should fail for a deauthorized node")
			}
			if _, err := s.Ping(ctx, d.ip4, 0); !errors.Is(err, source.ErrNotFound) {
				t.Fatalf("ping = %v", err)
			}
		}},
		{"tags replace user", func(t *testing.T, s *Sim) {
			d := s.dev(t, "pi-hole")
			if err := s.SetTags(ctx, string(d.id), []string{"tag:iot", "tag:dns"}); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			p := peerByName(t, st, "pi-hole")
			if !reflect.DeepEqual(p.Tags, []string{"tag:iot", "tag:dns"}) || p.UserLogin != "" || p.UserDisplay != "" {
				t.Fatalf("after tagging: %+v", p)
			}
			if err := s.SetTags(ctx, string(d.id), nil); err != nil {
				t.Fatal(err)
			}
			st, _ = s.Status(ctx)
			p = peerByName(t, st, "pi-hole")
			if len(p.Tags) != 0 || p.UserLogin != "alice@example.com" {
				t.Fatalf("after untagging: %+v", p)
			}
			if err := s.SetTags(ctx, string(d.id), []string{"iot"}); err == nil {
				t.Fatal("tag without prefix accepted")
			}
			if err := s.SetTags(ctx, string(d.id), []string{"tag:Bad Tag"}); err == nil {
				t.Fatal("tag with spaces accepted")
			}
		}},
		{"key expiry", func(t *testing.T, s *Sim) {
			d := s.dev(t, "office-printer-gw")
			if err := s.SetKeyExpiryDisabled(ctx, d.legacyID, true); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			devs, _ := s.Devices(ctx)
			p, a := peerByName(t, st, "office-printer-gw"), apiByName(t, devs, "office-printer-gw")
			if p.KeyExpiry != nil || a.Expires != nil || !a.KeyExpiryDisabled {
				t.Fatalf("expiry not disabled: %+v %+v", p.KeyExpiry, a)
			}
			if err := s.SetKeyExpiryDisabled(ctx, d.legacyID, false); err != nil {
				t.Fatal(err)
			}
			devs, _ = s.Devices(ctx)
			if a := apiByName(t, devs, "office-printer-gw"); a.Expires == nil || a.KeyExpiryDisabled {
				t.Fatalf("expiry not re-enabled: %+v", a)
			}
		}},
		{"routes", func(t *testing.T, s *Sim) {
			d := s.dev(t, "homelab")
			if err := s.SetRoutes(ctx, string(d.id), []string{"10.0.0.0/8", "0.0.0.0/0"}); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			devs, _ := s.Devices(ctx)
			p, a := peerByName(t, st, "homelab"), apiByName(t, devs, "homelab")
			if !reflect.DeepEqual(a.EnabledRoutes, []string{"10.0.0.0/8", "0.0.0.0/0"}) {
				t.Fatalf("enabled = %v", a.EnabledRoutes)
			}
			if !reflect.DeepEqual(p.PrimaryRoutes, []string{"10.0.0.0/8"}) || !p.ExitNodeOpt {
				t.Fatalf("primary = %v exitOpt = %v", p.PrimaryRoutes, p.ExitNodeOpt)
			}
			if len(a.AdvertisedRoutes) != 3 {
				t.Fatalf("advertised changed: %v", a.AdvertisedRoutes)
			}
			if err := s.SetRoutes(ctx, string(d.id), nil); err != nil {
				t.Fatal(err)
			}
			st, _ = s.Status(ctx)
			p = peerByName(t, st, "homelab")
			if len(p.PrimaryRoutes) != 0 || p.ExitNodeOpt {
				t.Fatalf("routes not cleared: %+v", p)
			}
			if err := s.SetRoutes(ctx, string(d.id), []string{"10.0.0.0/8", "not-a-cidr"}); err == nil {
				t.Fatal("invalid route accepted")
			}
		}},
		{"rename", func(t *testing.T, s *Sim) {
			d := s.dev(t, "bob-desktop")
			if err := s.SetName(ctx, string(d.id), " Bob-Tower "); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			p := peerByName(t, st, "bob-tower")
			if p == nil || p.DNSName != "bob-tower."+MagicDNSSuffix || p.HostName != "DESKTOP-7GK2Q1" {
				t.Fatalf("renamed peer = %+v", p)
			}
			if peerByName(t, st, "bob-desktop") != nil {
				t.Fatal("old name still present")
			}
			if err := s.SetName(ctx, string(d.id), "nas"); err == nil {
				t.Fatal("duplicate name accepted")
			}
			for _, bad := range []string{"", "-bad", "bad-", "has space", "has.dot", "x_y"} {
				if err := s.SetName(ctx, string(d.id), bad); err == nil {
					t.Errorf("invalid name %q accepted", bad)
				}
			}
			if ips := s.DeviceIPs(); ips[d.ip4] != d.id {
				t.Fatal("DeviceIPs changed after rename")
			}
		}},
		{"delete", func(t *testing.T, s *Sim) {
			d := s.dev(t, "alice-ipad")
			if err := s.DeleteDevice(ctx, string(d.id)); err != nil {
				t.Fatal(err)
			}
			st, _ := s.Status(ctx)
			devs, _ := s.Devices(ctx)
			if peerByName(t, st, "alice-ipad") != nil || apiByName(t, devs, "alice-ipad") != nil {
				t.Fatal("deleted device still listed")
			}
			if len(devs) != 15 || len(s.DeviceIPs()) != 15 {
				t.Fatalf("devices = %d ips = %d", len(devs), len(s.DeviceIPs()))
			}
			if _, err := s.Ping(ctx, d.ip4, 0); !errors.Is(err, source.ErrNotFound) {
				t.Fatalf("ping deleted = %v", err)
			}
			if _, err := s.Fetch(ctx, d.ip4, 0); err == nil {
				t.Fatal("fetch deleted should fail")
			}
			if err := s.DeleteDevice(ctx, string(d.id)); !errors.Is(err, source.ErrNotFound) {
				t.Fatalf("second delete = %v", err)
			}
			if err := s.SetAuthorized(ctx, string(d.id), true); !errors.Is(err, source.ErrNotFound) {
				t.Fatalf("mutate deleted = %v", err)
			}
		}},
		{"delete self refused", func(t *testing.T, s *Sim) {
			err := s.DeleteDevice(ctx, string(s.self.id))
			if !errors.Is(err, ErrSelfDelete) {
				t.Fatalf("err = %v", err)
			}
			st, _ := s.Status(ctx)
			if st.Self.ID != s.self.id {
				t.Fatal("self missing")
			}
		}},
		{"unknown device", func(t *testing.T, s *Sim) {
			for _, id := range []string{"", "nope", "nDEMO99CNTRL", "1800000000099"} {
				if err := s.SetAuthorized(ctx, id, true); !errors.Is(err, source.ErrNotFound) {
					t.Errorf("id %q: err = %v", id, err)
				}
			}
		}},
		{"legacy and node ids both work", func(t *testing.T, s *Sim) {
			d := s.dev(t, "media-box")
			if err := s.SetKeyExpiryDisabled(ctx, d.legacyID, true); err != nil {
				t.Fatal(err)
			}
			if err := s.SetKeyExpiryDisabled(ctx, string(d.id), false); err != nil {
				t.Fatal(err)
			}
		}},
	}
	// The subtests touch disjoint devices and run in order, so one Sim
	// serves them all (a fresh Sim per subtest would dominate test time).
	s, _ := newSim(t, 61, baseTime)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, s)
		})
	}
}
