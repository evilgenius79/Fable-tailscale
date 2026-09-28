package sysmetrics

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"
)

// probes are the individual gopsutil calls the Sampler makes, held as
// function values so tests can substitute deterministic fakes for hardware.
type probes struct {
	hostInfo      func(ctx context.Context) (*host.InfoStat, error)
	cpuPercent    func(ctx context.Context, perCPU bool) ([]float64, error)
	cpuCounts     func(ctx context.Context) (int, error)
	cpuInfo       func(ctx context.Context) ([]cpu.InfoStat, error)
	loadAvg       func(ctx context.Context) (*load.AvgStat, error)
	virtualMemory func(ctx context.Context) (*mem.VirtualMemoryStat, error)
	swapMemory    func(ctx context.Context) (*mem.SwapMemoryStat, error)
	partitions    func(ctx context.Context) ([]disk.PartitionStat, error)
	usage         func(ctx context.Context, path string) (*disk.UsageStat, error)
	ioCounters    func(ctx context.Context) ([]gnet.IOCountersStat, error)
	interfaces    func(ctx context.Context) (gnet.InterfaceStatList, error)
	temperatures  func(ctx context.Context) ([]sensors.TemperatureStat, error)
	pids          func(ctx context.Context) ([]int32, error)
}

// defaultProbes wires every probe to its gopsutil *WithContext variant.
func defaultProbes() probes {
	return probes{
		hostInfo: host.InfoWithContext,
		cpuPercent: func(ctx context.Context, perCPU bool) ([]float64, error) {
			// Interval 0 measures utilisation since the previous call.
			return cpu.PercentWithContext(ctx, 0, perCPU)
		},
		cpuCounts: func(ctx context.Context) (int, error) {
			return cpu.CountsWithContext(ctx, true)
		},
		cpuInfo:       cpu.InfoWithContext,
		loadAvg:       load.AvgWithContext,
		virtualMemory: mem.VirtualMemoryWithContext,
		swapMemory:    mem.SwapMemoryWithContext,
		partitions: func(ctx context.Context) ([]disk.PartitionStat, error) {
			return disk.PartitionsWithContext(ctx, false)
		},
		usage: disk.UsageWithContext,
		ioCounters: func(ctx context.Context) ([]gnet.IOCountersStat, error) {
			return gnet.IOCountersWithContext(ctx, true)
		},
		interfaces:   gnet.InterfacesWithContext,
		temperatures: sensors.TemperaturesWithContext,
		pids:         process.PidsWithContext,
	}
}

// ifaceAddrs flattens gopsutil's interface list into name -> address
// strings (CIDR or bare IP) for DetectTailscaleInterface. Order is kept.
func ifaceAddrs(list gnet.InterfaceStatList) []InterfaceAddrs {
	if len(list) == 0 {
		return nil
	}
	out := make([]InterfaceAddrs, 0, len(list))
	for _, i := range list {
		ia := InterfaceAddrs{Name: i.Name}
		for _, a := range i.Addrs {
			ia.Addrs = append(ia.Addrs, a.Addr)
		}
		out = append(out, ia)
	}
	return out
}
