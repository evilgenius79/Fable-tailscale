package demo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentclient"
	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

func TestFetchErrors(t *testing.T) {
	s, _ := newSim(t, 71, baseTime)
	ctx := context.Background()
	tests := []struct {
		name            string
		ip              string
		port            int
		wantUnreachable bool
		wantErrno       syscall.Errno
		wantStatus      int
		wantMsg         string
	}{
		{"iphone has no agent", "100.64.0.7", 0, true, syscall.ECONNREFUSED, 0, "connection refused"},
		{"pixel has no agent", "100.64.0.10", 0, true, syscall.ECONNREFUSED, 0, ""},
		{"shared node has no agent", "100.64.0.14", 0, true, syscall.ECONNREFUSED, 0, ""},
		{"ipad is offline", "100.64.0.8", 0, true, syscall.EHOSTUNREACH, 0, "no route"},
		{"unknown ip", "100.64.0.250", 0, true, syscall.EHOSTUNREACH, 0, ""},
		{"ipv6 phone", "fd7a:115c:a1e0::7", 0, true, syscall.ECONNREFUSED, 0, ""},
		{"workshop-pi forbids the hub", "100.64.0.13", 0, false, 0, http.StatusForbidden, "403"},
		{"invalid ip", "not-an-ip", 0, false, 0, 0, "invalid ip"},
		{"bad port", "100.64.0.2", 70000, false, 0, 0, "invalid port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := s.Fetch(ctx, tc.ip, tc.port)
			if err == nil || rep != nil {
				t.Fatalf("expected error, got report %+v", rep)
			}
			if got := agentclient.IsUnreachable(err); got != tc.wantUnreachable {
				t.Errorf("IsUnreachable = %v, want %v (%v)", got, tc.wantUnreachable, err)
			}
			if tc.wantErrno != 0 {
				if !errors.Is(err, tc.wantErrno) {
					t.Errorf("errors.Is(%v, %v) false", err, tc.wantErrno)
				}
				var oe *net.OpError
				if !errors.As(err, &oe) || oe.Op != "dial" {
					t.Errorf("not a dial OpError: %v", err)
				}
				var ne net.Error
				if !errors.As(err, &ne) || !ne.Timeout() {
					t.Errorf("does not satisfy net.Error with Timeout(): %v", err)
				}
			}
			if tc.wantStatus != 0 {
				var se *agentclient.StatusError
				if !errors.As(err, &se) || se.Code != tc.wantStatus {
					t.Errorf("status error = %v", err)
				}
				if !errors.Is(err, agentclient.ErrUnauthorized) {
					t.Errorf("403 should wrap ErrUnauthorized: %v", err)
				}
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err %q does not contain %q", err, tc.wantMsg)
			}
		})
	}
}

func TestFetchReport(t *testing.T) {
	s, _ := newSim(t, 71, baseTime)
	ctx := context.Background()
	tests := []struct {
		name  string
		ip    string
		check func(t *testing.T, r *agentproto.Report)
	}{
		{"hub", "100.64.0.1", func(t *testing.T, r *agentproto.Report) {
			if r.Host.Hostname != "tailwatch-hub" || r.Host.OS != "linux" || r.CPU.Count != 4 {
				t.Errorf("host = %+v cpu = %+v", r.Host, r.CPU)
			}
		}},
		{"nas", "100.64.0.2", func(t *testing.T, r *agentproto.Report) {
			if len(r.Disks) != 3 || !r.Disks[0].Primary || r.Disks[1].Primary {
				t.Errorf("disks = %+v", r.Disks)
			}
			if r.Host.Platform != "synology" || r.Host.UptimeSeconds == 0 {
				t.Errorf("host = %+v", r.Host)
			}
		}},
		{"pi-hole", "100.64.0.3", func(t *testing.T, r *agentproto.Report) {
			if len(r.Temperatures) != 1 || r.Temperatures[0].Sensor != "cpu_thermal" || r.Temperatures[0].Critical != 85 {
				t.Errorf("temps = %+v", r.Temperatures)
			}
		}},
		{"mac", "100.64.0.6", func(t *testing.T, r *agentproto.Report) {
			if r.Host.OS != "darwin" || r.Net.TailscaleIf != "utun4" {
				t.Errorf("mac = %+v net = %+v", r.Host, r.Net)
			}
		}},
		{"windows", "100.64.0.9", func(t *testing.T, r *agentproto.Report) {
			if r.Host.OS != "windows" || r.Disks[0].Mount != "C:" || r.Net.TailscaleIf != "Tailscale" || r.CPU.Count != 20 {
				t.Errorf("windows = %+v %+v %+v", r.Host, r.Disks, r.Net)
			}
		}},
		{"freebsd", "100.64.0.16", func(t *testing.T, r *agentproto.Report) {
			if r.Host.OS != "freebsd" || r.Net.Interfaces[0].Name != "lo0" {
				t.Errorf("freebsd = %+v %+v", r.Host, r.Net)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := s.Fetch(ctx, tc.ip, agentproto.DefaultPort)
			if err != nil {
				t.Fatal(err)
			}
			// Common invariants.
			if r.ProtocolVersion != agentproto.ProtocolVersion || r.AgentVersion == "" {
				t.Errorf("versions: %q %q", r.ProtocolVersion, r.AgentVersion)
			}
			if r.SampledAt.After(baseTime) || baseTime.Sub(r.SampledAt) > 5*time.Second {
				t.Errorf("sampledAt = %v", r.SampledAt)
			}
			if r.Host.BootTime.After(r.SampledAt) || uint64(baseTime.Sub(r.Host.BootTime)/time.Second) != r.Host.UptimeSeconds {
				t.Errorf("boot %v uptime %d", r.Host.BootTime, r.Host.UptimeSeconds)
			}
			if r.CPU.Percent < 0 || r.CPU.Percent > 100 || len(r.CPU.PerCore) != r.CPU.Count || r.CPU.Model == "" {
				t.Errorf("cpu = %+v", r.CPU)
			}
			if r.Memory.Total == 0 || r.Memory.Used+r.Memory.Available != r.Memory.Total || r.Memory.Percent <= 0 {
				t.Errorf("memory = %+v", r.Memory)
			}
			for _, d := range r.Disks {
				if d.Used > d.Total || d.Percent < 0 || d.Percent > 100 || d.Mount == "" {
					t.Errorf("disk = %+v", d)
				}
			}
			phys := 0
			for _, i := range r.Net.Interfaces {
				if i.Physical {
					phys++
				}
				if i.Name == r.Net.TailscaleIf && i.Physical {
					t.Errorf("tailscale interface marked physical")
				}
			}
			if phys != 1 || r.Net.TailscaleIf == "" {
				t.Errorf("interfaces = %+v", r.Net)
			}
			if r.Processes <= 0 || r.Tailscale == nil || r.Tailscale.BackendState != "Running" || len(r.Tailscale.IPs) != 2 {
				t.Errorf("procs %d tailscale %+v", r.Processes, r.Tailscale)
			}
			tc.check(t, r)
		})
	}
}

func TestFetchOfflineDeviceBecomesReachable(t *testing.T) {
	s, c := newSim(t, 71, baseTime)
	ctx := context.Background()
	d := s.dev(t, "old-laptop")
	var sawUp, sawDown bool
	for i := 0; i < 240*60/10 && !(sawUp && sawDown); i++ {
		_, err := s.Fetch(ctx, d.ip4, 0)
		if err == nil {
			sawUp = true
		} else {
			if !agentclient.IsUnreachable(err) {
				t.Fatalf("unexpected error class: %v", err)
			}
			sawDown = true
		}
		c.advance(10 * time.Minute)
	}
	if !sawUp || !sawDown {
		t.Fatalf("old-laptop up=%v down=%v over 240h", sawUp, sawDown)
	}
}
