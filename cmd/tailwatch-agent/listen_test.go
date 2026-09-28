package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

type fakeStatus struct {
	calls int
	fn    func(n int) (*source.LocalStatus, error)
}

func (f *fakeStatus) Status(context.Context) (*source.LocalStatus, error) {
	f.calls++
	return f.fn(f.calls)
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestClassifyListenIP(t *testing.T) {
	tests := []struct {
		ip   string
		want listenKind
	}{
		{"100.64.0.1", listenTailscale},
		{"100.127.255.254", listenTailscale},
		{"100.128.0.1", listenOther},
		{"100.63.255.255", listenOther},
		{"fd7a:115c:a1e0::1", listenTailscale},
		{"fd7a:115c:a1e0:ab12:4843:cd96:6263:5a4e", listenTailscale},
		{"fd7a:115c:a1e1::1", listenOther},
		{"127.0.0.1", listenLoopback},
		{"127.5.5.5", listenLoopback},
		{"::1", listenLoopback},
		{"0.0.0.0", listenAny},
		{"::", listenAny},
		{"192.168.1.5", listenOther},
		{"10.0.0.1", listenOther},
		{"::ffff:100.64.0.1", listenTailscale},
		{"2001:db8::1", listenOther},
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			if got := classifyListenIP(netip.MustParseAddr(tc.ip)); got != tc.want {
				t.Fatalf("classifyListenIP(%s) = %s, want %s", tc.ip, got, tc.want)
			}
		})
	}
}

func TestResolveListenPolicy(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		insecure bool
		want     string
		wantErr  string
	}{
		{"tailscale v4", "100.64.0.1:41820", false, "100.64.0.1:41820", ""},
		{"tailscale v4 default port", "100.64.0.1", false, "100.64.0.1:41820", ""},
		{"tailscale v6", "[fd7a:115c:a1e0::1]:41820", false, "[fd7a:115c:a1e0::1]:41820", ""},
		{"loopback", "127.0.0.1:1", false, "127.0.0.1:1", ""},
		{"localhost", "localhost:1", false, "127.0.0.1:1", ""},
		{"lan refused", "192.168.1.5:41820", false, "", "refusing to listen on non-Tailscale address 192.168.1.5"},
		{"any refused", "0.0.0.0:41820", false, "", "refusing"},
		{"any v6 refused", "[::]:41820", false, "", "refusing"},
		{"lan allowed insecure", "192.168.1.5:41820", true, "192.168.1.5:41820", ""},
		{"any allowed insecure", "0.0.0.0:41820", true, "0.0.0.0:41820", ""},
		{"hostname refused even insecure", "example.com:41820", true, "", "IP literal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newListenResolver(&fakeStatus{fn: func(int) (*source.LocalStatus, error) { return nil, errors.New("unused") }}, quietLog())
			got, err := r.resolve(context.Background(), tc.listen, 41820, tc.insecure)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("addr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveAutoRetriesUntilIP(t *testing.T) {
	fs := &fakeStatus{fn: func(n int) (*source.LocalStatus, error) {
		switch n {
		case 1:
			return nil, errors.New("daemon starting")
		case 2:
			return &source.LocalStatus{BackendState: "NeedsLogin"}, nil
		case 3:
			return &source.LocalStatus{TailscaleIPs: []string{"fd7a:115c:a1e0::7"}}, nil
		default:
			return &source.LocalStatus{TailscaleIPs: []string{"fd7a:115c:a1e0::7", "100.64.0.7"}}, nil
		}
	}}
	r := newListenResolver(fs, quietLog())
	sleeps := 0
	r.sleep = func(context.Context, time.Duration) bool { sleeps++; return true }
	got, err := r.resolve(context.Background(), listenAuto, 41820, false)
	if err != nil {
		t.Fatal(err)
	}
	// Attempt 3 already has a Tailscale IPv6 address; do not wait for IPv4.
	if got != "[fd7a:115c:a1e0::7]:41820" || fs.calls != 3 || sleeps != 2 {
		t.Fatalf("addr=%q calls=%d sleeps=%d", got, fs.calls, sleeps)
	}
}

func TestResolveAutoTimesOut(t *testing.T) {
	fs := &fakeStatus{fn: func(int) (*source.LocalStatus, error) { return nil, errors.New("no daemon") }}
	r := newListenResolver(fs, quietLog())
	r.retry, r.timeout = time.Nanosecond, 0
	r.sleep = func(context.Context, time.Duration) bool { return true }
	_, err := r.resolve(context.Background(), listenAuto, 41820, false)
	if err == nil || !strings.Contains(err.Error(), "could not determine a Tailscale IP") || !strings.Contains(err.Error(), "no daemon") || !strings.Contains(err.Error(), ":41820") {
		t.Fatalf("err = %v", err)
	}
	if fs.calls < 1 {
		t.Fatal("status never called")
	}
}

func TestResolveAutoCancelled(t *testing.T) {
	fs := &fakeStatus{fn: func(int) (*source.LocalStatus, error) { return nil, errors.New("no daemon") }}
	r := newListenResolver(fs, quietLog())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.resolve(ctx, listenAuto, 41820, false)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestFirstIPv4(t *testing.T) {
	if ip, ok := firstIPv4([]string{"junk", "fd7a:115c:a1e0::1", "::ffff:100.64.0.3", "100.64.0.4"}); !ok || ip.String() != "100.64.0.3" {
		t.Fatalf("got %v %v", ip, ok)
	}
	if _, ok := firstIPv4([]string{"fd7a:115c:a1e0::1"}); ok {
		t.Fatal("v6-only should not match")
	}
	if ip, ok := firstTailscaleIP([]string{"fd7a:115c:a1e0::1"}); !ok || ip.String() != "fd7a:115c:a1e0::1" {
		t.Fatalf("v6 fallback got %v %v", ip, ok)
	}
	if ip, ok := firstTailscaleIP([]string{"fd7a:115c:a1e0::1", "100.64.0.9"}); !ok || ip.String() != "100.64.0.9" {
		t.Fatalf("v4 preferred got %v %v", ip, ok)
	}
}
