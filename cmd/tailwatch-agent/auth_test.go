package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestParseAuthMode(t *testing.T) {
	for in, want := range map[string]authMode{"whois": authWhois, " Token ": authToken, "BOTH": authBoth} {
		got, err := parseAuthMode(in)
		if err != nil || got != want {
			t.Errorf("parseAuthMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "none", "tokens"} {
		if _, err := parseAuthMode(in); err == nil {
			t.Errorf("parseAuthMode(%q) should fail", in)
		}
	}
	if !authWhois.usesWhoIs() || authWhois.usesToken() || authToken.usesWhoIs() || !authToken.usesToken() || !authBoth.usesWhoIs() || !authBoth.usesToken() {
		t.Error("mode predicates wrong")
	}
}

func TestNewPolicyValidation(t *testing.T) {
	if _, err := newPolicy(authToken, "", nil, nil, nil); err == nil {
		t.Error("token mode without token should fail")
	}
	if _, err := newPolicy(authBoth, "   ", nil, nil, nil); err == nil {
		t.Error("both mode with blank token should fail")
	}
	if _, err := newPolicy("nope", "", nil, nil, nil); err == nil {
		t.Error("bad mode should fail")
	}
	p, err := newPolicy(authWhois, "", []string{" Alice@X.com ", "alice@x.com", ""}, []string{"TAG:Hub"}, []string{"Hub.example.ts.net."})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.allowUsers) != 1 || len(p.allowTags) != 1 || len(p.allowNodes) != 1 {
		t.Fatalf("lists not normalised: %+v", p)
	}
	if _, ok := p.allowUsers["alice@x.com"]; !ok {
		t.Error("user not lower-cased")
	}
	if _, ok := p.allowTags["tag:hub"]; !ok {
		t.Error("tag not lower-cased")
	}
	if _, ok := p.allowNodes["hub.example.ts.net"]; !ok {
		t.Error("node not normalised")
	}
	if !p.hasAllowLists() {
		t.Error("hasAllowLists false")
	}
}

func TestTokenOK(t *testing.T) {
	p, err := newPolicy(authToken, "correct-horse-battery", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		presented string
		want      bool
	}{
		{"correct-horse-battery", true},
		{" correct-horse-battery\n", true},
		{"correct-horse-batter", false},
		{"correct-horse-battery-x", false},
		{"CORRECT-HORSE-BATTERY", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range tests {
		if got := p.tokenOK(tc.presented); got != tc.want {
			t.Errorf("tokenOK(%q) = %v, want %v", tc.presented, got, tc.want)
		}
	}
	whois, _ := newPolicy(authWhois, "", nil, nil, nil)
	if !whois.tokenOK("") {
		t.Error("whois mode should not require a token")
	}
}

func TestIdentityAllowed(t *testing.T) {
	user := func(login, node string, tags ...string) *source.WhoIs {
		return &source.WhoIs{LoginName: login, NodeName: node, Tags: tags, IsTagged: len(tags) > 0}
	}
	tagged := func(node string, tags ...string) *source.WhoIs {
		return &source.WhoIs{LoginName: "tagged-device", NodeName: node, Tags: tags, IsTagged: true}
	}
	type lists struct{ users, tags, nodes []string }
	tests := []struct {
		name  string
		lists lists
		owner string
		w     *source.WhoIs
		want  bool
	}{
		{"default: owner match", lists{}, "alice@example.com", user("alice@example.com", "laptop.example.ts.net"), true},
		{"default: owner match case-insensitive", lists{}, "Alice@Example.com", user("alice@example.com", "laptop"), true},
		{"default: other user denied", lists{}, "alice@example.com", user("bob@example.com", "laptop"), false},
		{"default: tag:tailwatch allowed", lists{}, "alice@example.com", tagged("hub", "tag:server", "tag:tailwatch"), true},
		{"default: tag:tailwatch case-insensitive", lists{}, "alice@example.com", tagged("hub", "Tag:TailWatch"), true},
		{"default: other tag denied", lists{}, "alice@example.com", tagged("hub", "tag:server"), false},
		{"default: tagged self, untagged caller denied", lists{}, "", user("alice@example.com", "laptop"), false},
		{"default: tagged self, empty login denied", lists{}, "", user("", "laptop"), false},
		{"default: tagged self, tag:tailwatch allowed", lists{}, "", tagged("hub", "tag:tailwatch"), true},
		{"default: login equal but tagged flag denied", lists{}, "alice@example.com", &source.WhoIs{LoginName: "alice@example.com", IsTagged: true, Tags: []string{"tag:x"}}, false},
		{"default: nil identity", lists{}, "alice@example.com", nil, false},
		{"lists: owner no longer implied", lists{users: []string{"bob@example.com"}}, "alice@example.com", user("alice@example.com", "laptop"), false},
		{"lists: tag:tailwatch no longer implied", lists{users: []string{"bob@example.com"}}, "alice@example.com", tagged("hub", "tag:tailwatch"), false},
		{"lists: user match", lists{users: []string{"Bob@Example.com"}}, "alice@example.com", user("bob@example.com", "laptop"), true},
		{"lists: tag match any", lists{tags: []string{"tag:hub"}}, "", tagged("hub", "tag:x", "TAG:HUB"), true},
		{"lists: tag no match", lists{tags: []string{"tag:hub"}}, "", tagged("hub", "tag:x"), false},
		{"lists: node base name matches fqdn", lists{nodes: []string{"hub"}}, "", tagged("hub.example.ts.net", "tag:x"), true},
		{"lists: node fqdn matches fqdn", lists{nodes: []string{"hub.example.ts.net"}}, "", user("carol@example.com", "Hub.Example.ts.net."), true},
		{"lists: node fqdn entry matches short name", lists{nodes: []string{"hub.example.ts.net"}}, "", user("carol@example.com", "hub"), true},
		{"lists: node prefix is not enough", lists{nodes: []string{"hub"}}, "", user("carol@example.com", "hub2.example.ts.net"), false},
		{"lists: node different suffix denied", lists{nodes: []string{"hub.example.ts.net"}}, "", user("carol@example.com", "hub.other.ts.net"), false},
		{"lists: empty node name", lists{nodes: []string{"hub"}}, "", user("carol@example.com", ""), false},
		{"lists: any list matches", lists{users: []string{"x"}, tags: []string{"tag:y"}, nodes: []string{"hub"}}, "", user("carol@example.com", "hub.example.ts.net"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := newPolicy(authWhois, "", tc.lists.users, tc.lists.tags, tc.lists.nodes)
			if err != nil {
				t.Fatal(err)
			}
			got, reason := p.identityAllowed(tc.w, tc.owner)
			if got != tc.want {
				t.Fatalf("identityAllowed = %v (%s), want %v", got, reason, tc.want)
			}
			if reason == "" {
				t.Fatal("empty reason")
			}
		})
	}
}

type fakeWhoIs struct {
	calls int
	fn    func(remoteAddr string) (*source.WhoIs, error)
}

func (f *fakeWhoIs) WhoIs(_ context.Context, remoteAddr string) (*source.WhoIs, error) {
	f.calls++
	return f.fn(remoteAddr)
}

func TestWhoisCache(t *testing.T) {
	c := newFakeClock()
	fail := false
	src := &fakeWhoIs{fn: func(addr string) (*source.WhoIs, error) {
		if fail {
			return nil, errors.New("down")
		}
		return &source.WhoIs{NodeName: "n-" + addr}, nil
	}}
	cache := newWhoisCache(src, 60*time.Second, c.now)
	ctx := context.Background()

	w1, err := cache.lookup(ctx, "100.64.0.1", "100.64.0.1:1111")
	if err != nil || w1.NodeName != "n-100.64.0.1:1111" {
		t.Fatalf("first lookup: %+v %v", w1, err)
	}
	if _, err := cache.lookup(ctx, "100.64.0.1", "100.64.0.1:2222"); err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatalf("calls = %d, want 1 (cached per IP)", src.calls)
	}
	if _, err := cache.lookup(ctx, "100.64.0.2", "100.64.0.2:1"); err != nil || src.calls != 2 {
		t.Fatalf("other IP should miss: calls=%d err=%v", src.calls, err)
	}
	c.advance(61 * time.Second)
	if _, err := cache.lookup(ctx, "100.64.0.1", "100.64.0.1:3333"); err != nil || src.calls != 3 {
		t.Fatalf("expired entry should refetch: calls=%d err=%v", src.calls, err)
	}
	fail = true
	if _, err := cache.lookup(ctx, "100.64.0.9", "100.64.0.9:1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := cache.lookup(ctx, "100.64.0.9", "100.64.0.9:1"); err == nil || src.calls != 5 {
		t.Fatalf("errors must not be cached: calls=%d err=%v", src.calls, err)
	}
	// Cached entries survive a source outage.
	if w, err := cache.lookup(ctx, "100.64.0.1", "100.64.0.1:1"); err != nil || w == nil {
		t.Fatalf("cached entry should be served during outage: %v", err)
	}

	empty := newWhoisCache(nil, 0, nil)
	if _, err := empty.lookup(ctx, "1.2.3.4", "1.2.3.4:1"); err == nil {
		t.Fatal("nil source should error, not panic")
	}
}
