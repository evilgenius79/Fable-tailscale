package demo

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentclient"
	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

// Average packet sizes used to derive packet counters from byte counters.
const (
	avgRxPacket = 880.0
	avgTxPacket = 610.0
)

// unreachable builds the error a real dial to ip:port would produce, so
// agentclient.IsUnreachable reports true for it.
func unreachable(ip netip.Addr, port int, errno syscall.Errno) error {
	op := &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, uint16(port))),
		Err:  &fetchErr{errno},
	}
	return fmt.Errorf("demo: agent %s: %w", ip, op)
}

// fetchErr wraps a syscall errno and additionally satisfies net.Error with
// Timeout()==true so any connectivity classifier treats it as unreachable.
type fetchErr struct{ errno syscall.Errno }

// Error implements error.
func (e *fetchErr) Error() string { return e.errno.Error() }

// Unwrap exposes the errno so errors.Is(err, syscall.ECONNREFUSED) holds.
func (e *fetchErr) Unwrap() error { return e.errno }

// Timeout implements net.Error.
func (e *fetchErr) Timeout() bool { return true }

// Temporary implements the legacy net.Error method.
func (e *fetchErr) Temporary() bool { return true }

// Fetch implements source.AgentClient. Devices that run an agent (linux,
// macOS, windows and freebsd nodes other than shared-node and workshop-pi)
// return a full report. Phones and the shared-in node fail the way an
// unreachable agent does (a dial error wrapping syscall.ECONNREFUSED that
// also satisfies net.Error); offline, unknown, deleted and unauthorized
// nodes fail with EHOSTUNREACH; workshop-pi's agent rejects the hub with an
// *agentclient.StatusError carrying HTTP 403.
func (s *Sim) Fetch(ctx context.Context, ip string, port int) (*agentproto.Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: agent: %w", err)
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return nil, fmt.Errorf("demo: agent: invalid ip %q: %w", ip, err)
	}
	addr = addr.Unmap()
	if port == 0 {
		port = agentproto.DefaultPort
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("demo: agent: invalid port %d", port)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tick()

	d := s.lookupIP(addr.String())
	if d == nil {
		return nil, unreachable(addr, port, syscall.EHOSTUNREACH)
	}
	if d.agent == agentForbidden {
		return nil, fmt.Errorf("demo: agent %s: %w", addr, &agentclient.StatusError{
			Code: http.StatusForbidden,
			Body: "forbidden: hub is not on this agent's allow-list",
		})
	}
	in := s.instantAt(d, t, true)
	if !in.online || (!d.authorized && d != s.self) {
		return nil, unreachable(addr, port, syscall.EHOSTUNREACH)
	}
	if d.agent != agentYes {
		return nil, unreachable(addr, port, syscall.ECONNREFUSED)
	}
	return s.report(d, t, in), nil
}

// report builds the agent report for an online device. s.mu must be held.
func (s *Sim) report(d *device, t time.Time, in instant) *agentproto.Report {
	p := &d.profile
	sampled := t.Truncate(5 * time.Second)
	if sampled.Before(in.boot) {
		sampled = t
	}
	rep := &agentproto.Report{
		ProtocolVersion: agentproto.ProtocolVersion,
		AgentVersion:    AgentVersion,
		SampledAt:       sampled,
		Host: agentproto.Host{
			Hostname:        p.hostname,
			OS:              p.goos,
			Platform:        p.platform,
			PlatformVersion: p.platVer,
			Kernel:          p.kernel,
			Arch:            p.arch,
			BootTime:        in.boot,
			UptimeSeconds:   in.uptime,
		},
		CPU: agentproto.CPU{
			Percent: roundTo(in.cpu, 0.1),
			PerCore: in.perCore,
			Count:   p.cores,
			Model:   p.cpuModel,
			Load1:   roundTo(in.load1, 0.01),
			Load5:   roundTo(in.load5, 0.01),
			Load15:  roundTo(in.load15, 0.01),
		},
		Processes: in.procs,
		Tailscale: &agentproto.TailscaleInfo{
			Version:      d.clientVersion,
			BackendState: "Running",
			IPs:          d.addresses(),
		},
	}

	used := uint64(float64(p.memBytes) * in.mem / 100)
	rep.Memory = agentproto.Memory{
		Total:     p.memBytes,
		Used:      used,
		Available: p.memBytes - used,
		Percent:   roundTo(in.mem, 0.1),
		SwapTotal: p.swap,
	}
	if p.swap > 0 {
		rep.Memory.SwapUsed = uint64(float64(p.swap) * clamp((in.mem-60)/40, 0, 0.6))
	}

	for _, ds := range p.disks {
		pct := ds.pct
		if ds.primary {
			pct = in.disk
		}
		pct = roundTo(clamp(pct, 0, 100), 0.1)
		rep.Disks = append(rep.Disks, agentproto.Disk{
			Mount:   ds.mount,
			Device:  ds.device,
			FSType:  ds.fstype,
			Total:   ds.total,
			Used:    uint64(float64(ds.total) * pct / 100),
			Percent: pct,
			Primary: ds.primary,
		})
	}

	mk := func(name string, rx, tx float64, physical bool) agentproto.Interface {
		return agentproto.Interface{
			Name:      name,
			RxBytes:   uint64(rx),
			TxBytes:   uint64(tx),
			RxPackets: uint64(rx / avgRxPacket),
			TxPackets: uint64(tx / avgTxPacket),
			Physical:  physical,
		}
	}
	physRx, physTx := in.counters[flowPhysRx], in.counters[flowPhysTx]
	lo := "lo"
	if p.goos == "windows" {
		lo = "Loopback Pseudo-Interface 1"
	} else if p.goos == "freebsd" {
		lo = "lo0"
	}
	rep.Net = agentproto.Net{
		Interfaces: []agentproto.Interface{
			mk(lo, math.Floor(physRx*0.03), math.Floor(physRx*0.03), false),
			mk(p.physIf, physRx, physTx, true),
			mk(p.tsIf, in.counters[flowTSRx], in.counters[flowTSTx], false),
		},
		TailscaleIf: p.tsIf,
	}
	if p.presence == onlineBlocks {
		// The old laptop's flaky Wi-Fi drops the odd frame.
		rep.Net.Interfaces[1].RxErrors = uint64(physRx / 5e8)
	}

	for i, sensor := range p.sensors {
		temp := in.temp + 2.5*float64(i)*(unit(mix(d.h, chTemp, uint64(i)+1))-0.5)
		rep.Temperatures = append(rep.Temperatures, agentproto.Temperature{
			Sensor:   sensor,
			Celsius:  roundTo(temp, 0.1),
			Critical: 85,
		})
	}
	return rep
}
