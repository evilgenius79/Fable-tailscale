package sysmetrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

// recordingHandler is a slog.Handler that keeps every record.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Message == msg {
			n++
		}
	}
	return n
}

var errProbe = errors.New("probe failed")

// fakeProbes returns deterministic probes covering every edge the
// collectors handle: clamping, capping, dedupe, filtering, sorting.
func fakeProbes() probes {
	return probes{
		hostInfo: func(context.Context) (*host.InfoStat, error) {
			return &host.InfoStat{
				Hostname: "box", OS: "linux", Platform: "ubuntu", PlatformVersion: "24.04",
				KernelVersion: "6.1.0", BootTime: 1_700_000_000, Uptime: 12345,
			}, nil
		},
		cpuPercent: func(_ context.Context, perCPU bool) ([]float64, error) {
			if !perCPU {
				return []float64{42.5}, nil
			}
			out := make([]float64, MaxPerCore+6)
			for i := range out {
				out[i] = float64(i)
			}
			out[1] = 150 // clamped to 100
			out[2] = -3  // clamped to 0
			return out, nil
		},
		cpuCounts: func(context.Context) (int, error) { return 8, nil },
		cpuInfo: func(context.Context) ([]cpu.InfoStat, error) {
			return []cpu.InfoStat{{ModelName: "  Fake   CPU  9000 "}, {ModelName: "other"}}, nil
		},
		loadAvg: func(context.Context) (*load.AvgStat, error) {
			return &load.AvgStat{Load1: 1.5, Load5: 1.0, Load15: 0.5}, nil
		},
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 100, Used: 40, Available: 60, UsedPercent: 40}, nil
		},
		swapMemory: func(context.Context) (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 10, Used: 2}, nil
		},
		partitions: func(context.Context) ([]disk.PartitionStat, error) {
			return []disk.PartitionStat{
				{Device: "/dev/sdb1", Mountpoint: "/data", Fstype: "xfs"},
				{Device: "proc", Mountpoint: "/proc", Fstype: "proc"},
				{Device: "tmpfs", Mountpoint: "/run", Fstype: "tmpfs"},
				{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
				{Device: "/dev/sda2", Mountpoint: "/boot", Fstype: "ext4"}, // usage fails
				{Device: "overlay", Mountpoint: "/var/lib/docker/overlay2/x", Fstype: "overlay"},
				{Device: "/dev/sdb1", Mountpoint: "/data", Fstype: "xfs"}, // duplicate
				{Device: "nas:/x", Mountpoint: "/mnt/nas", Fstype: "nfs4"},
			}, nil
		},
		usage: func(_ context.Context, path string) (*disk.UsageStat, error) {
			if path == "/boot" {
				return nil, errProbe
			}
			return &disk.UsageStat{Total: 1000, Used: 250, UsedPercent: 25}, nil
		},
		ioCounters: func(context.Context) ([]gnet.IOCountersStat, error) {
			return []gnet.IOCountersStat{
				{Name: "lo", BytesRecv: 1, BytesSent: 1},
				{Name: "eth0", BytesRecv: 100, BytesSent: 200, PacketsRecv: 3, PacketsSent: 4, Errin: 1, Errout: 2},
				{Name: "tailscale0", BytesRecv: 5, BytesSent: 6},
				{Name: "docker0"},
				{Name: "eth0", BytesRecv: 999}, // duplicate ignored
				{Name: ""},
			}, nil
		},
		interfaces: func(context.Context) (gnet.InterfaceStatList, error) {
			return gnet.InterfaceStatList{
				{Name: "lo", Addrs: gnet.InterfaceAddrList{{Addr: "127.0.0.1/8"}}},
				{Name: "eth0", Addrs: gnet.InterfaceAddrList{{Addr: "192.168.1.2/24"}}},
				{Name: "tailscale0", Addrs: gnet.InterfaceAddrList{{Addr: "100.101.102.103/32"}, {Addr: "fd7a:115c:a1e0::1/128"}}},
			}, nil
		},
		temperatures: func(context.Context) ([]sensors.TemperatureStat, error) {
			return []sensors.TemperatureStat{
				{SensorKey: "nvme", Temperature: 38.5},
				{SensorKey: "coretemp_core_0", Temperature: 45, Critical: 100},
				{SensorKey: "coretemp_core_0", Temperature: 46}, // duplicate
				{SensorKey: "acpitz", Temperature: 0},           // zero dropped
				{SensorKey: "", Temperature: 30},                // unnamed dropped
				{SensorKey: "bogus", Temperature: 1e9},          // implausible
			}, errors.New("partial warnings") // gopsutil-style partial error
		},
		pids: func(context.Context) ([]int32, error) { return []int32{1, 2, 3}, nil },
	}
}

// failingProbes returns probes that all fail.
func failingProbes() probes {
	return probes{
		hostInfo:      func(context.Context) (*host.InfoStat, error) { return nil, errProbe },
		cpuPercent:    func(context.Context, bool) ([]float64, error) { return nil, errProbe },
		cpuCounts:     func(context.Context) (int, error) { return 0, errProbe },
		cpuInfo:       func(context.Context) ([]cpu.InfoStat, error) { return nil, errProbe },
		loadAvg:       func(context.Context) (*load.AvgStat, error) { return nil, errProbe },
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) { return nil, errProbe },
		swapMemory:    func(context.Context) (*mem.SwapMemoryStat, error) { return nil, errProbe },
		partitions:    func(context.Context) ([]disk.PartitionStat, error) { return nil, errProbe },
		usage:         func(context.Context, string) (*disk.UsageStat, error) { return nil, errProbe },
		ioCounters:    func(context.Context) ([]gnet.IOCountersStat, error) { return nil, errProbe },
		interfaces:    func(context.Context) (gnet.InterfaceStatList, error) { return nil, errProbe },
		temperatures:  func(context.Context) ([]sensors.TemperatureStat, error) { return nil, errProbe },
		pids:          func(context.Context) ([]int32, error) { return nil, errProbe },
	}
}

func newTestSampler(p probes, h slog.Handler) *Sampler {
	if h == nil {
		h = slog.NewTextHandler(io.Discard, nil)
	}
	s := NewSampler(time.Second, "v-test", slog.New(h))
	s.probes = p
	return s
}

var fixedNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func TestNewSamplerDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero", 0, DefaultInterval},
		{"negative", -time.Second, DefaultInterval},
		{"too small", 10 * time.Millisecond, MinInterval},
		{"ok", 7 * time.Second, 7 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSampler(tc.in, "v", nil)
			if s.interval != tc.want {
				t.Fatalf("interval = %s, want %s", s.interval, tc.want)
			}
			if s.log == nil {
				t.Fatal("nil logger not defaulted")
			}
		})
	}
}

func TestLatestNotReadyBeforeSample(t *testing.T) {
	s := newTestSampler(fakeProbes(), nil)
	if _, ok := s.Latest(); ok {
		t.Fatal("Latest reported ok before any sample")
	}
}

func TestSampleAssemblesReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mount filtering expectations are for Unix paths")
	}
	s := newTestSampler(fakeProbes(), nil)
	s.now = func() time.Time { return fixedNow }
	s.SetTailscaleInfo(func(context.Context) (*agentproto.TailscaleInfo, error) {
		return &agentproto.TailscaleInfo{Version: "1.102.5", BackendState: "Running", IPs: []string{"100.101.102.103"}, Health: []string{"w"}}, nil
	})
	s.sample(context.Background())
	rep, ok := s.Latest()
	if !ok {
		t.Fatal("Latest not ok after sample")
	}

	if rep.ProtocolVersion != agentproto.ProtocolVersion || rep.AgentVersion != "v-test" {
		t.Fatalf("version fields = %q/%q", rep.ProtocolVersion, rep.AgentVersion)
	}
	if !rep.SampledAt.Equal(fixedNow) {
		t.Fatalf("SampledAt = %s, want %s", rep.SampledAt, fixedNow)
	}

	wantHost := agentproto.Host{
		Hostname: "box", OS: runtime.GOOS, Platform: "ubuntu", PlatformVersion: "24.04", Kernel: "6.1.0",
		Arch: runtime.GOARCH, BootTime: time.Unix(1_700_000_000, 0).UTC(), UptimeSeconds: 12345,
	}
	if rep.Host != wantHost {
		t.Fatalf("Host = %+v, want %+v", rep.Host, wantHost)
	}

	c := rep.CPU
	if c.Percent != 42.5 || c.Count != 8 || c.Model != "Fake CPU 9000" || c.Load1 != 1.5 || c.Load5 != 1.0 || c.Load15 != 0.5 {
		t.Fatalf("CPU = %+v", c)
	}
	if len(c.PerCore) != MaxPerCore {
		t.Fatalf("PerCore len = %d, want %d", len(c.PerCore), MaxPerCore)
	}
	if c.PerCore[1] != 100 || c.PerCore[2] != 0 || c.PerCore[3] != 3 {
		t.Fatalf("PerCore clamping wrong: %v", c.PerCore[:4])
	}

	wantMem := agentproto.Memory{Total: 100, Used: 40, Available: 60, Percent: 40, SwapTotal: 10, SwapUsed: 2}
	if rep.Memory != wantMem {
		t.Fatalf("Memory = %+v, want %+v", rep.Memory, wantMem)
	}

	wantDisks := []agentproto.Disk{
		{Mount: "/", Device: "/dev/sda1", FSType: "ext4", Total: 1000, Used: 250, Percent: 25, Primary: true},
		{Mount: "/data", Device: "/dev/sdb1", FSType: "xfs", Total: 1000, Used: 250, Percent: 25},
	}
	if !slices.Equal(rep.Disks, wantDisks) {
		t.Fatalf("Disks = %+v, want %+v", rep.Disks, wantDisks)
	}

	var names []string
	for _, i := range rep.Net.Interfaces {
		names = append(names, i.Name)
	}
	if want := []string{"docker0", "eth0", "lo", "tailscale0"}; !slices.Equal(names, want) {
		t.Fatalf("interface names = %v, want %v", names, want)
	}
	for _, i := range rep.Net.Interfaces {
		if i.Physical != (i.Name == "eth0") {
			t.Errorf("%s physical = %v", i.Name, i.Physical)
		}
	}
	eth := rep.Net.Interfaces[1]
	if eth.RxBytes != 100 || eth.TxBytes != 200 || eth.RxPackets != 3 || eth.TxPackets != 4 || eth.RxErrors != 1 || eth.TxErrors != 2 {
		t.Fatalf("eth0 counters = %+v", eth)
	}
	if rep.Net.TailscaleIf != "tailscale0" {
		t.Fatalf("TailscaleIf = %q", rep.Net.TailscaleIf)
	}

	wantTemps := []agentproto.Temperature{
		{Sensor: "coretemp_core_0", Celsius: 45, Critical: 100},
		{Sensor: "nvme", Celsius: 38.5},
	}
	if !slices.Equal(rep.Temperatures, wantTemps) {
		t.Fatalf("Temperatures = %+v, want %+v", rep.Temperatures, wantTemps)
	}
	if rep.Processes != 3 {
		t.Fatalf("Processes = %d", rep.Processes)
	}
	if rep.Tailscale == nil || rep.Tailscale.Version != "1.102.5" || rep.Tailscale.BackendState != "Running" ||
		!slices.Equal(rep.Tailscale.IPs, []string{"100.101.102.103"}) || !slices.Equal(rep.Tailscale.Health, []string{"w"}) {
		t.Fatalf("Tailscale = %+v", rep.Tailscale)
	}
}

func TestFailingProbesStillProduceReport(t *testing.T) {
	h := &recordingHandler{}
	s := newTestSampler(failingProbes(), h)
	s.SetTailscaleInfo(func(context.Context) (*agentproto.TailscaleInfo, error) { return nil, errProbe })
	s.sample(context.Background())
	s.sample(context.Background())

	rep, ok := s.Latest()
	if !ok {
		t.Fatal("no report produced")
	}
	if rep.ProtocolVersion != agentproto.ProtocolVersion || rep.SampledAt.IsZero() {
		t.Fatalf("header fields missing: %+v", rep)
	}
	if rep.Host.OS != runtime.GOOS || rep.Host.Arch != runtime.GOARCH || rep.Host.Hostname != "" {
		t.Fatalf("Host = %+v", rep.Host)
	}
	if !reflect.DeepEqual(rep.CPU, agentproto.CPU{}) || rep.Memory != (agentproto.Memory{}) {
		t.Fatalf("CPU/Memory not zero: %+v %+v", rep.CPU, rep.Memory)
	}
	if rep.Disks != nil || rep.Net.Interfaces != nil || rep.Net.TailscaleIf != "" || rep.Temperatures != nil || rep.Processes != 0 || rep.Tailscale != nil {
		t.Fatalf("sections not zero: %+v", rep)
	}
	// Each failing collector logs exactly once across two samples: host,
	// cpu percent, cpu per-core, cpu counts, cpu info, load, vmem, swap,
	// partitions, io counters, interfaces, temperatures, pids, tailscale.
	if got := h.count("collector failed"); got != 14 {
		t.Fatalf("collector failed logged %d times, want 14 (once per collector)", got)
	}
}

func TestLatestReturnsDeepCopy(t *testing.T) {
	s := newTestSampler(fakeProbes(), nil)
	s.SetTailscaleInfo(func(context.Context) (*agentproto.TailscaleInfo, error) {
		return &agentproto.TailscaleInfo{IPs: []string{"100.64.0.1"}}, nil
	})
	s.sample(context.Background())
	a, _ := s.Latest()
	a.CPU.PerCore[0] = 999
	a.Disks[0].Mount = "/mutated"
	a.Net.Interfaces[0].Name = "mutated"
	a.Temperatures[0].Sensor = "mutated"
	a.Tailscale.IPs[0] = "mutated"
	b, _ := s.Latest()
	if b.CPU.PerCore[0] == 999 || b.Disks[0].Mount == "/mutated" || b.Net.Interfaces[0].Name == "mutated" ||
		b.Temperatures[0].Sensor == "mutated" || b.Tailscale.IPs[0] == "mutated" {
		t.Fatal("Latest shares memory with the stored report")
	}
}

func TestTailscaleInfoThrottledAndSticky(t *testing.T) {
	s := newTestSampler(fakeProbes(), nil)
	now := fixedNow
	s.now = func() time.Time { return now }
	var calls int
	var fail bool
	s.SetTailscaleInfo(func(context.Context) (*agentproto.TailscaleInfo, error) {
		calls++
		if fail {
			return nil, errProbe
		}
		return &agentproto.TailscaleInfo{Version: "v" + string(rune('0'+calls))}, nil
	})
	ctx := context.Background()

	s.sample(ctx)
	s.sample(ctx)
	now = now.Add(30 * time.Second)
	s.sample(ctx)
	if calls != 1 {
		t.Fatalf("calls = %d within 60s, want 1", calls)
	}
	rep, _ := s.Latest()
	if rep.Tailscale == nil || rep.Tailscale.Version != "v1" {
		t.Fatalf("Tailscale = %+v", rep.Tailscale)
	}

	now = now.Add(31 * time.Second)
	fail = true
	s.sample(ctx)
	if calls != 2 {
		t.Fatalf("calls = %d after 61s, want 2", calls)
	}
	rep, _ = s.Latest()
	if rep.Tailscale == nil || rep.Tailscale.Version != "v1" {
		t.Fatalf("previous value not kept on error: %+v", rep.Tailscale)
	}

	s.SetTailscaleInfo(nil)
	s.sample(ctx)
	if rep, _ := s.Latest(); rep.Tailscale != nil {
		t.Fatalf("Tailscale should be nil after clearing: %+v", rep.Tailscale)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	s := newTestSampler(fakeProbes(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := s.Latest(); ok {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("no sample within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunRespectsCancelBeforeFirstSample(t *testing.T) {
	s := newTestSampler(fakeProbes(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly on a cancelled context")
	}
}

func TestKeepPartition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mount prefixes")
	}
	tests := []struct {
		mount, fstype string
		want          bool
	}{
		{"/", "ext4", true},
		{"/home", "xfs", true},
		{"/boot", "ext4", true},
		{"/boot/efi", "vfat", false},
		{"/mnt/data", "btrfs", true},
		{"/tank", "zfs", true},
		{"/", "apfs", true},
		{"/", "ntfs3", true},
		{"/proc", "proc", false},
		{"/proc/sys/fs/binfmt_misc", "binfmt_misc", false},
		{"/sys/fs/cgroup", "cgroup2", false},
		{"/dev", "devtmpfs", false},
		{"/dev/shm", "tmpfs", false},
		{"/run/user/1000", "tmpfs", false},
		{"/tmp", "tmpfs", false},
		{"/snap/core/1", "squashfs", false},
		{"/var/lib/docker/overlay2/abc/merged", "overlay", false},
		{"/var/lib/containers/storage", "xfs", false},
		{"/System/Volumes/Data", "apfs", false},
		{"/mnt/nas", "nfs4", false},
		{"/mnt/share", "cifs", false},
		{"/mnt/ssh", "fuse.sshfs", false},
		{"/host_mnt", "fuse.osxfs", true},
		{"/media/usb", "fuseblk", true},
		{"/mnt/gvfs", "fuse.gvfsd-fuse", false},
		{"/mnt/other", "fuse.something", false},
		{"", "ext4", false},
		{"/procfoo", "ext4", true}, // prefix must be a path component
	}
	for _, tc := range tests {
		t.Run(tc.mount+"|"+tc.fstype, func(t *testing.T) {
			if got := KeepPartition(tc.mount, tc.fstype); got != tc.want {
				t.Fatalf("KeepPartition(%q,%q) = %v, want %v", tc.mount, tc.fstype, got, tc.want)
			}
		})
	}
}

func TestIsPrimaryMount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix root")
	}
	if !IsPrimaryMount("/") || IsPrimaryMount("/data") || IsPrimaryMount("") {
		t.Fatal("IsPrimaryMount wrong for Unix paths")
	}
}

func TestIsPhysicalInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"eth0", true}, {"enp3s0", true}, {"en0", true}, {"wlan0", true}, {"wlp2s0", true},
		{"Ethernet", true}, {"Wi-Fi", true}, {"eno1", true}, {"bond0", true}, {"ens5", true},
		{"lo", false}, {"lo0", false}, {"Loopback Pseudo-Interface 1", false},
		{"tailscale0", false}, {"Tailscale", false}, {"utun4", false}, {"wg0", false},
		{"docker0", false}, {"br-1a2b3c", false}, {"veth1234", false}, {"virbr0", false},
		{"vmnet8", false}, {"ts0", false}, {"zt0", false}, {"tun0", false}, {"tap0", false},
		{"cni0", false}, {"flannel.1", false}, {"podman0", false}, {"bridge0", false},
		{"awdl0", false}, {"llw0", false}, {"vEthernet (WSL)", false}, {"ifb0", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPhysicalInterface(tc.name); got != tc.want {
				t.Fatalf("IsPhysicalInterface(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestDetectTailscaleInterface(t *testing.T) {
	tests := []struct {
		name   string
		ifaces []InterfaceAddrs
		want   string
	}{
		{"linux", []InterfaceAddrs{
			{Name: "lo", Addrs: []string{"127.0.0.1/8"}},
			{Name: "eth0", Addrs: []string{"192.168.1.2/24"}},
			{Name: "tailscale0", Addrs: []string{"100.101.102.103/32", "fd7a:115c:a1e0::1/128"}},
		}, "tailscale0"},
		{"macos picks utun with cgnat, not other utuns", []InterfaceAddrs{
			{Name: "utun0", Addrs: []string{"fe80::1/64"}},
			{Name: "utun3", Addrs: []string{"10.8.0.2/24"}},
			{Name: "utun4", Addrs: []string{"100.64.5.6/32"}},
		}, "utun4"},
		{"windows name", []InterfaceAddrs{{Name: "Tailscale", Addrs: []string{"100.64.1.1/32"}}}, "Tailscale"},
		{"ipv6 only", []InterfaceAddrs{{Name: "tailscale0", Addrs: []string{"fd7a:115c:a1e0:ab12::1/128"}}}, "tailscale0"},
		{"bare addr strings", []InterfaceAddrs{{Name: "tailscale0", Addrs: []string{"100.100.100.100"}}}, "tailscale0"},
		{"custom tun name falls back by address", []InterfaceAddrs{
			{Name: "eth0", Addrs: []string{"10.0.0.2/24"}},
			{Name: "myts", Addrs: []string{"100.64.0.9/32"}},
		}, "myts"},
		{"name without ts address ignored", []InterfaceAddrs{{Name: "tailscale0", Addrs: []string{"10.0.0.1/24"}}}, ""},
		{"cgnat on non-ts name outside range", []InterfaceAddrs{{Name: "eth0", Addrs: []string{"100.128.0.1/24"}}}, ""},
		{"garbage addrs", []InterfaceAddrs{{Name: "tailscale0", Addrs: []string{"", "nope", "100.64.0.1/33"}}}, ""},
		{"none", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectTailscaleInterface(tc.ifaces); got != tc.want {
				t.Fatalf("DetectTailscaleInterface = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIfaceAddrs(t *testing.T) {
	got := ifaceAddrs(gnet.InterfaceStatList{{Name: "a", Addrs: gnet.InterfaceAddrList{{Addr: "1.2.3.4/24"}}}, {Name: "b"}})
	want := []InterfaceAddrs{{Name: "a", Addrs: []string{"1.2.3.4/24"}}, {Name: "b"}}
	if len(got) != 2 || got[0].Name != "a" || !slices.Equal(got[0].Addrs, want[0].Addrs) || got[1].Name != "b" || got[1].Addrs != nil {
		t.Fatalf("ifaceAddrs = %+v, want %+v", got, want)
	}
	if ifaceAddrs(nil) != nil {
		t.Fatal("nil input should give nil")
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[float64]float64{-1: 0, 0: 0, 50: 50, 100: 100, 101: 100} {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%v) = %v, want %v", in, got, want)
		}
	}
	if clampPercent(nan()) != 0 || nonNegative(nan()) != 0 || nonNegative(-2) != 0 || nonNegative(3) != 3 {
		t.Error("NaN/negative handling wrong")
	}
	for in, want := range map[float64]bool{0: false, -273: false, 25: true, 99.5: true, 500: false} {
		if got := plausibleCelsius(in); got != want {
			t.Errorf("plausibleCelsius(%v) = %v, want %v", in, got, want)
		}
	}
	if trimSpaces("  a   b\tc ") != "a b c" {
		t.Error("trimSpaces")
	}
}

func nan() float64 {
	z := 0.0
	return z / z
}

// TestRealHostSample runs the real gopsutil probes twice on the test host.
// It asserts structure and monotonic counters only; hardware-dependent
// values are logged, not asserted.
func TestRealHostSample(t *testing.T) {
	if testing.Short() {
		t.Skip("real host probes")
	}
	s := NewSampler(time.Second, "v-real", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.prime(ctx)
	s.sample(ctx)
	first, ok := s.Latest()
	if !ok {
		t.Fatal("no report")
	}
	time.Sleep(50 * time.Millisecond)
	s.sample(ctx)
	second, _ := s.Latest()

	if first.ProtocolVersion != agentproto.ProtocolVersion || first.AgentVersion != "v-real" {
		t.Fatalf("header = %q/%q", first.ProtocolVersion, first.AgentVersion)
	}
	if !second.SampledAt.After(first.SampledAt) {
		t.Fatalf("SampledAt not monotonic: %s then %s", first.SampledAt, second.SampledAt)
	}
	if first.Host.OS != runtime.GOOS || first.Host.Arch != runtime.GOARCH {
		t.Fatalf("Host OS/Arch = %q/%q", first.Host.OS, first.Host.Arch)
	}
	if first.CPU.Percent < 0 || first.CPU.Percent > 100 || len(first.CPU.PerCore) > MaxPerCore {
		t.Fatalf("CPU out of range: %+v", first.CPU)
	}
	if first.Memory.Total > 0 && first.Memory.Used > first.Memory.Total {
		t.Fatalf("memory used > total: %+v", first.Memory)
	}
	for _, d := range first.Disks {
		if d.Percent < 0 || d.Percent > 100 || d.Used > d.Total || d.Mount == "" {
			t.Fatalf("bad disk entry: %+v", d)
		}
	}
	prev := make(map[string]agentproto.Interface, len(first.Net.Interfaces))
	for _, i := range first.Net.Interfaces {
		prev[i.Name] = i
	}
	for _, i := range second.Net.Interfaces {
		p, ok := prev[i.Name]
		if !ok {
			continue
		}
		if i.RxBytes < p.RxBytes || i.TxBytes < p.TxBytes || i.RxPackets < p.RxPackets || i.TxPackets < p.TxPackets {
			t.Fatalf("counters went backwards on %s: %+v -> %+v", i.Name, p, i)
		}
	}
	for _, tp := range first.Temperatures {
		if tp.Sensor == "" || tp.Celsius == 0 {
			t.Fatalf("bad temperature entry: %+v", tp)
		}
	}
	t.Logf("host=%s/%s cpus=%d model=%q mem=%d disks=%d ifaces=%d tsif=%q temps=%d procs=%d",
		first.Host.Platform, first.Host.PlatformVersion, first.CPU.Count, first.CPU.Model, first.Memory.Total,
		len(first.Disks), len(first.Net.Interfaces), first.Net.TailscaleIf, len(first.Temperatures), first.Processes)
}
