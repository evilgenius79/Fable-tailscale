package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

func TestRunVersionAndUsageErrors(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"--version"}, envMap(nil), &out, &errOut); code != exitOK || !strings.Contains(out.String(), "tailwatch-agent ") {
		t.Fatalf("--version: code=%d out=%q", code, out.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"--auth", "bogus"}, envMap(nil), &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "--auth") {
		t.Fatalf("bad flag: code=%d err=%q", code, errOut.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"-h"}, envMap(nil), &out, &errOut); code != exitOK || !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("help: code=%d err=%q", code, errOut.String())
	}
}

func TestNewLogger(t *testing.T) {
	var sb strings.Builder
	l := newLogger("warn", true, &sb)
	l.Info("hidden")
	l.Warn("shown", "k", "v")
	if strings.Contains(sb.String(), "hidden") || !strings.Contains(sb.String(), `"msg":"shown"`) {
		t.Fatalf("logger output: %q", sb.String())
	}
	sb.Reset()
	newLogger("debug", false, &sb).Debug("dbg")
	if !strings.Contains(sb.String(), "level=DEBUG") {
		t.Fatalf("text logger output: %q", sb.String())
	}
}

func TestLocalStateCachesAndDerives(t *testing.T) {
	clock := newFakeClock()
	var fail bool
	fs := &fakeStatus{fn: func(n int) (*source.LocalStatus, error) {
		if fail {
			return nil, errors.New("down")
		}
		return &source.LocalStatus{
			Version: "1.102.5", BackendState: "Running",
			TailscaleIPs:   []string{"100.64.0.1", "fd7a:115c:a1e0::1"},
			Health:         []string{"warn"},
			MagicDNSSuffix: "example.ts.net",
			Self:           source.LocalPeer{UserLogin: "alice@example.com"},
		}, nil
	}}
	st := newLocalState(fs, 60*time.Second, quietLog())
	st.now = clock.now
	ctx := context.Background()

	if got := st.ownerLogin(ctx); got != "alice@example.com" || fs.calls != 1 {
		t.Fatalf("owner = %q calls=%d", got, fs.calls)
	}
	if got := st.identity(ctx); got != (localIdentity{Owner: "alice@example.com", MagicDNSSuffix: "example.ts.net"}) || fs.calls != 1 {
		t.Fatalf("identity = %+v calls=%d", got, fs.calls)
	}
	st.ownerLogin(ctx)
	clock.advance(30 * time.Second)
	st.ownerLogin(ctx)
	if fs.calls != 1 {
		t.Fatalf("status not cached: calls=%d", fs.calls)
	}
	clock.advance(31 * time.Second)
	fail = true
	if got := st.ownerLogin(ctx); got != "alice@example.com" || fs.calls != 2 {
		t.Fatalf("stale value should survive a failed refresh: owner=%q calls=%d", got, fs.calls)
	}

	fail = false
	info, err := st.tailscaleInfo(ctx)
	if err != nil || fs.calls != 3 {
		t.Fatalf("tailscaleInfo should always refresh: err=%v calls=%d", err, fs.calls)
	}
	if info.Version != "1.102.5" || info.BackendState != "Running" || len(info.IPs) != 2 || len(info.Health) != 1 {
		t.Fatalf("info = %+v", info)
	}
	info.IPs[0] = "mutated"
	if st.st.TailscaleIPs[0] == "mutated" {
		t.Fatal("tailscaleInfo shares slices with the cache")
	}

	// A cold cache with a failing daemon yields empty owner and an error.
	fail = true
	cold := newLocalState(fs, time.Minute, quietLog())
	if got := cold.ownerLogin(ctx); got != "" {
		t.Fatalf("cold owner = %q", got)
	}
	if got := cold.identity(ctx); got != (localIdentity{}) {
		t.Fatalf("cold identity = %+v", got)
	}
	if _, err := cold.tailscaleInfo(ctx); err == nil {
		t.Fatal("expected error from cold failing state")
	}
}
