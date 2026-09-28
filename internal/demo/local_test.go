package demo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestPingConsistentWithStatus(t *testing.T) {
	s, _ := newSim(t, 31, baseTime)
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	online, offline := 0, 0
	for _, p := range st.Peers {
		for _, ip := range p.TailscaleIPs {
			r, err := s.Ping(ctx, ip, 5*time.Second)
			if err != nil {
				t.Fatalf("ping %s: %v", ip, err)
			}
			if r.NodeName != p.DNSName {
				t.Errorf("%s: node name %q", ip, r.NodeName)
			}
			if !p.Online {
				offline++
				if r.Err == "" || !strings.Contains(r.Err, "no route") || r.LatencyMs != 0 {
					t.Errorf("%s offline ping = %+v", p.DNSName, r)
				}
				continue
			}
			online++
			if r.Err != "" {
				t.Errorf("%s online ping error %q", p.DNSName, r.Err)
			}
			switch {
			case p.CurAddr != "":
				if r.Endpoint != p.CurAddr || r.DERPRegionCode != "" || r.DERPRegionID != 0 {
					t.Errorf("%s direct ping = %+v, status curAddr %s", p.DNSName, r, p.CurAddr)
				}
				if r.LatencyMs < 1 || r.LatencyMs > 40 {
					t.Errorf("%s direct latency %.1f", p.DNSName, r.LatencyMs)
				}
			default:
				if r.DERPRegionCode != p.Relay || r.DERPRegionID != derpRegionIDs[p.Relay] || r.Endpoint != "" {
					t.Errorf("%s relay ping = %+v, status relay %s", p.DNSName, r, p.Relay)
				}
				if r.LatencyMs < 30 || r.LatencyMs > 320 {
					t.Errorf("%s relay latency %.1f", p.DNSName, r.LatencyMs)
				}
			}
		}
	}
	if online == 0 || offline == 0 {
		t.Fatalf("online=%d offline=%d: expected both", online, offline)
	}
}

func TestPingErrors(t *testing.T) {
	s, _ := newSim(t, 31, baseTime)
	ctx := context.Background()
	tests := []struct {
		name      string
		ip        string
		timeout   time.Duration
		wantErr   error
		wantMsg   string
		wantReply string
	}{
		{"invalid", "nope", time.Second, nil, "invalid ip", ""},
		{"unknown", "100.64.0.200", time.Second, source.ErrNotFound, "", ""},
		{"unauthorized node", "100.64.0.13", time.Second, source.ErrNotFound, "", ""},
		{"self", "100.64.0.1", time.Second, nil, "local node", ""},
		{"timeout", "100.64.0.5", time.Millisecond, nil, "", "timeout"},
		{"no timeout means no limit", "100.64.0.5", 0, nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := s.Ping(ctx, tc.ip, tc.timeout)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantMsg)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if r.Err != tc.wantReply {
					t.Fatalf("reply err = %q, want %q", r.Err, tc.wantReply)
				}
			}
		})
	}
}

func TestStatusPeerFields(t *testing.T) {
	s, c := newSim(t, 41, baseTime)
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.RunningLatest == nil || !*st.RunningLatest || st.LatestVersion != ClientVersion {
		t.Errorf("version fields: %+v", st)
	}
	if len(st.TailscaleIPs) != 2 || st.TailscaleIPs[0] != "100.64.0.1" {
		t.Errorf("self ips = %v", st.TailscaleIPs)
	}
	for _, p := range st.Peers {
		if p.PublicKey == "" || !strings.HasPrefix(p.PublicKey, "nodekey:") || len(p.PublicKey) != len("nodekey:")+64 {
			t.Errorf("%s public key %q", p.DNSName, p.PublicKey)
		}
		if p.Created.After(baseTime) || p.Created.IsZero() {
			t.Errorf("%s created %v", p.DNSName, p.Created)
		}
		if len(p.TailscaleIPs) != 2 || len(p.AllowedIPs) < 2 {
			t.Errorf("%s ips %v allowed %v", p.DNSName, p.TailscaleIPs, p.AllowedIPs)
		}
		if p.Online {
			if !p.LastSeen.Equal(baseTime) || p.LastHandshake.After(baseTime) || baseTime.Sub(p.LastHandshake) > 2*time.Minute {
				t.Errorf("%s online timestamps: seen=%v hs=%v", p.DNSName, p.LastSeen, p.LastHandshake)
			}
		} else {
			if !p.LastSeen.Before(baseTime) || p.CurAddr != "" || p.Active {
				t.Errorf("%s offline fields: %+v", p.DNSName, p)
			}
		}
		if (p.UserLogin == "") != (len(p.Tags) > 0) && !p.ShareeNode {
			t.Errorf("%s user/tags: %q %v", p.DNSName, p.UserLogin, p.Tags)
		}
	}
	// The flapping phone eventually reports an offline window with LastSeen
	// at the start of that window.
	var sawOffline bool
	for i := 0; i < 24*60/15 && !sawOffline; i++ {
		c.advance(time.Minute)
		st, err = s.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p := peerByName(t, st, "alice-iphone")
		if p == nil {
			t.Fatal("iphone missing")
		}
		if !p.Online {
			sawOffline = true
			if now := c.now(); p.LastSeen.After(now) || now.Sub(p.LastSeen) > 6*time.Minute {
				t.Errorf("iphone offline lastSeen %v at %v", p.LastSeen, now)
			}
		}
	}
	if !sawOffline {
		t.Error("iphone never went offline in 96 minutes")
	}
}
