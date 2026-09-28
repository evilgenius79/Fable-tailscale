package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func authConfig(t *testing.T, args ...string) *config.Config {
	t.Helper()
	cfg, err := config.Load(append([]string{"--listen", "100.64.0.1:8484"}, args...), nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func whoisTable() map[string]*source.WhoIs {
	return map[string]*source.WhoIs{
		"100.64.0.10": {NodeID: "n-alice", NodeName: "alice-mbp.tail.ts.net.", NodeIP: "100.64.0.10", LoginName: "Alice@Example.com", DisplayName: "Alice", ProfilePic: "https://p/alice.png"},
		"100.64.0.11": {NodeID: "n-bob", NodeName: "bob-pc.tail.ts.net", NodeIP: "100.64.0.11", LoginName: "bob@example.com", DisplayName: "Bob"},
		"100.64.0.20": {NodeID: "n-srv", NodeName: "srv.tail.ts.net", NodeIP: "100.64.0.20", LoginName: "tagged-device", IsTagged: true, Tags: []string{"tag:Server", "tag:monitor"}},
		"100.64.0.21": {NodeID: "n-iot", NodeName: "iot.tail.ts.net", NodeIP: "100.64.0.21", LoginName: "tagged-device", IsTagged: true, Tags: []string{"tag:iot"}},
		"100.64.0.30": {NodeID: "n-empty", NodeName: "x", NodeIP: "100.64.0.30"},
	}
}

func TestTailscaleAuthRoles(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		ip       string
		wantRole model.Role
		wantErr  error
	}{
		{"admin by login case-insensitive", []string{"--admins", "alice@example.com"}, "100.64.0.10", model.RoleAdmin, nil},
		{"default viewers wildcard", nil, "100.64.0.11", model.RoleViewer, nil},
		{"wildcard includes tagged", nil, "100.64.0.21", model.RoleViewer, nil},
		{"admin by tag", []string{"--admin-tags", "tag:server"}, "100.64.0.20", model.RoleAdmin, nil},
		{"viewer by tag only", []string{"--viewers", "nobody@example.com", "--viewer-tags", "tag:monitor"}, "100.64.0.20", model.RoleViewer, nil},
		{"tagged node does not match login list", []string{"--viewers", "tagged-device"}, "100.64.0.21", "", ErrForbidden},
		{"viewer by login", []string{"--viewers", "bob@example.com"}, "100.64.0.11", model.RoleViewer, nil},
		{"no role", []string{"--viewers", "carol@example.com"}, "100.64.0.11", "", ErrForbidden},
		{"admin wins over viewer", []string{"--admins", "bob@example.com", "--viewers", "bob@example.com"}, "100.64.0.11", model.RoleAdmin, nil},
		{"unknown address", nil, "100.64.0.99", "", ErrUnauthenticated},
		{"empty identity", nil, "100.64.0.30", "", ErrUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			local := &fakeLocal{whois: whoisTable()}
			a := NewTailscaleAuth(local, authConfig(t, tc.args...))
			id, err := a.Authenticate(context.Background(), tc.ip+":40000")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if id.Role != tc.wantRole || id.AuthMode != "tailscale" || id.NodeIP != tc.ip {
				t.Errorf("identity = %+v", id)
			}
			if tc.ip == "100.64.0.10" {
				if id.Login != "alice@example.com" || id.NodeName != "alice-mbp.tail.ts.net" || id.ProfilePic == "" || id.Tags == nil {
					t.Errorf("identity fields = %+v", id)
				}
			}
			if tc.ip == "100.64.0.20" && (id.Login != "tagged-device" || len(id.Tags) != 2 || id.Tags[0] != "tag:server") {
				t.Errorf("tagged identity = %+v", id)
			}
		})
	}
}

func TestTailscaleAuthCache(t *testing.T) {
	local := &fakeLocal{whois: whoisTable()}
	a := NewTailscaleAuth(local, authConfig(t, "--admins", "alice@example.com")).(*tailscaleAuth)
	now := baseTime
	a.now = func() time.Time { return now }
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := a.Authenticate(ctx, "100.64.0.10:1"); err != nil {
			t.Fatal(err)
		}
	}
	if local.whoisCalls() != 1 {
		t.Errorf("whois calls = %d, want 1 (cached)", local.whoisCalls())
	}
	// Different port, same IP: still cached. Different IP: new lookup.
	a.Authenticate(ctx, "100.64.0.10:2")
	a.Authenticate(ctx, "100.64.0.11:1")
	if local.whoisCalls() != 2 {
		t.Errorf("whois calls = %d, want 2", local.whoisCalls())
	}
	// Forbidden identities are cached for the full TTL too.
	a.Authenticate(ctx, "100.64.0.21:1")
	a.Authenticate(ctx, "100.64.0.21:1")
	if local.whoisCalls() != 3 {
		t.Errorf("whois calls = %d, want 3", local.whoisCalls())
	}
	// Negative results expire quickly.
	a.Authenticate(ctx, "100.64.0.99:1")
	a.Authenticate(ctx, "100.64.0.99:1")
	if local.whoisCalls() != 4 {
		t.Errorf("whois calls = %d, want 4", local.whoisCalls())
	}
	now = now.Add(identityNegativeTTL + time.Millisecond)
	a.Authenticate(ctx, "100.64.0.99:1")
	if local.whoisCalls() != 5 {
		t.Errorf("negative entry not expired: calls = %d", local.whoisCalls())
	}
	// Positive entries survive until the TTL.
	a.Authenticate(ctx, "100.64.0.10:1")
	if local.whoisCalls() != 5 {
		t.Errorf("positive entry expired early: calls = %d", local.whoisCalls())
	}
	now = now.Add(identityCacheTTL)
	a.Authenticate(ctx, "100.64.0.10:1")
	if local.whoisCalls() != 6 {
		t.Errorf("positive entry not refreshed: calls = %d", local.whoisCalls())
	}
	// Returned identities are copies.
	id1, _ := a.Authenticate(ctx, "100.64.0.10:1")
	id1.Login = "mallory"
	id2, _ := a.Authenticate(ctx, "100.64.0.10:1")
	if id2.Login != "alice@example.com" {
		t.Error("cache entry aliased")
	}
	// A cancelled context is not cached.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.Authenticate(cctx, "100.64.0.50:1"); err == nil {
		t.Error("expected error for unknown ip")
	}
	// Bad remote address.
	if _, err := a.Authenticate(ctx, "nonsense"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("bad addr err = %v", err)
	}
}

func TestNoAuth(t *testing.T) {
	id, err := NewNoAuth().Authenticate(context.Background(), "127.0.0.1:5000")
	if err != nil {
		t.Fatal(err)
	}
	if id.Login != "demo@example.com" || id.DisplayName != "Demo User" || id.NodeName != "alice-mbp" || id.Role != model.RoleAdmin || id.AuthMode != "none" || id.NodeIP != "127.0.0.1" {
		t.Errorf("identity = %+v", id)
	}
}

func TestRemoteIP(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"100.64.0.1:1234", "100.64.0.1", true},
		{"100.64.0.1", "100.64.0.1", true},
		{"[fd7a::1]:80", "fd7a::1", true},
		{"[fe80::1%eth0]:80", "fe80::1", true},
		{"[::ffff:100.64.0.1]:80", "100.64.0.1", true},
		{"nonsense", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		ip, ok := remoteIP(tc.in)
		if ok != tc.ok || (ok && ip.String() != tc.want) {
			t.Errorf("remoteIP(%q) = %v, %v; want %q, %v", tc.in, ip, ok, tc.want, tc.ok)
		}
	}
}

func TestIdentityFromContext(t *testing.T) {
	if _, ok := IdentityFromContext(context.Background()); ok {
		t.Error("empty context has an identity")
	}
	ctx := withIdentity(context.Background(), &model.Identity{Login: "x"})
	if id, ok := IdentityFromContext(ctx); !ok || id.Login != "x" {
		t.Error("identity not round-tripped")
	}
}
