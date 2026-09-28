package demo

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
)

const day = 24 * time.Hour

// agentKind describes how a device answers agent fetches.
type agentKind int

const (
	agentYes       agentKind = iota // runs tailwatch-agent
	agentNone                       // no agent: connection refused
	agentForbidden                  // agent rejects the hub: HTTP 403
)

// onlineKind describes a device's presence pattern.
type onlineKind int

const (
	onlineAlways onlineKind = iota
	onlineNever             // offline for days
	onlineFlap              // offline for ~4 minutes every ~40 minutes
	onlineBlocks            // online ~10% of the time in 6-hour blocks
)

// Flow indexes into the per-device rate/counter vectors.
const (
	flowPeerRx = iota // bytes the hub receives from the peer over Tailscale
	flowPeerTx        // bytes the hub sends to the peer
	flowPhysRx        // the device's physical interface, received
	flowPhysTx        // the device's physical interface, sent
	flowTSRx          // the device's tailscale interface, received (all peers)
	flowTSTx          // the device's tailscale interface, sent
	nFlows
)

// Exit routes advertised by exit nodes.
var exitRoutes = []string{"0.0.0.0/0", "::/0"}

// DERP region codes -> numeric IDs (Tailscale's public DERP map).
var derpRegionIDs = map[string]int{
	"nyc": 1, "sfo": 2, "sin": 3, "fra": 4, "syd": 5, "blr": 6, "tok": 7,
	"lhr": 8, "dfw": 9, "sea": 10, "sao": 11, "ord": 12, "den": 13,
	"ams": 14, "jnb": 15, "hkg": 16, "lax": 17, "mia": 18, "hnl": 19,
	"tor": 20, "par": 21, "nue": 22, "nai": 23, "waw": 24, "mad": 25,
	"dxb": 26,
}

// users maps login names to display names.
var users = map[string]string{
	"alice@example.com":       "Alice Smith",
	"bob@example.com":         "Bob Jones",
	"carol@othercorp.example": "Carol Diaz",
}

// diskSpec is one simulated filesystem.
type diskSpec struct {
	mount, device, fstype string
	total                 uint64
	pct                   float64
	primary               bool
}

// profile is the static description of one simulated device.
type profile struct {
	name     string // initial MagicDNS base name
	hostname string
	os       string // model OS: linux, macOS, windows, iOS, android, freebsd
	goos     string // agent Host.OS: linux, darwin, windows, freebsd
	arch     string
	model    string
	platform string
	platVer  string
	kernel   string
	cpuModel string
	cores    int
	memBytes uint64
	swap     uint64
	disks    []diskSpec
	physIf   string
	tsIf     string
	sensors  []string // temperature sensor names
	tempBase float64

	user       string
	tags       []string
	agent      agentKind
	presence   onlineKind
	external   bool
	authorized bool
	direct     bool   // hub reaches the peer directly when online
	relay      string // home DERP region code
	lanIP      string
	pubIP      string
	port       int
	lanPeer    bool // shares the hub's LAN (direct endpoint is the LAN address)

	exitNodeInUse bool     // the hub currently routes through this exit node
	advertised    []string // advertised routes (exit routes and/or subnets)
	routesEnabled bool     // advertised routes are approved initially

	clientVersion   string
	updateAvailable bool
	keyExpiryDays   float64 // 0 = key expiry disabled
	createdDaysAgo  float64
	rebootPeriod    time.Duration
	blocksIncoming  bool
	mapVaries       bool
	nat             model.NATSupport
	derp            map[string]float64 // region code -> base latency ms
	location        *model.Location

	cpuBase, cpuAmp float64
	cpuSpiky        bool // 12-minute >90% plateau every 2 hours
	memPct          float64
	latency         float64 // base hub->peer RTT ms
	procs           int
	flows           [nFlows]float64 // average bytes/s per flow
	backup          bool            // nightly 02:00 UTC backup burst to the hub
}

// device is a fleet member: the static profile plus mutable admin state and
// the per-device counter integrator.
type device struct {
	profile
	idx      int
	h        uint64
	id       model.DeviceID
	legacyID string
	ip4, ip6 string
	pubKey   string

	// mutable administrative state
	name              string
	tags              []string
	user              string
	authorized        bool
	keyExpiryDisabled bool
	enabledRoutes     []string
	deleted           bool

	sig   signals
	flows integrator
}

// buildFleet instantiates the fleet for the given seed hash.
func buildFleet(seed uint64) []*device {
	profiles := fleetProfiles()
	devs := make([]*device, 0, len(profiles))
	for i, p := range profiles {
		d := &device{
			profile:           p,
			idx:               i,
			h:                 mix(seed, uint64(i)+1),
			id:                model.DeviceID(fmt.Sprintf("nDEMO%02dCNTRL", i+1)),
			legacyID:          strconv.FormatInt(1800000000000+int64(i+1), 10),
			ip4:               fmt.Sprintf("100.64.0.%d", i+1),
			ip6:               fmt.Sprintf("fd7a:115c:a1e0::%x", i+1),
			name:              p.name,
			tags:              append([]string(nil), p.tags...),
			user:              p.user,
			authorized:        p.authorized,
			keyExpiryDisabled: p.keyExpiryDays <= 0,
		}
		if len(d.tags) > 0 {
			d.user = ""
		}
		if p.routesEnabled {
			d.enabledRoutes = append([]string(nil), p.advertised...)
		}
		d.pubKey = "nodekey:" + fmt.Sprintf("%016x%016x%016x%016x",
			mix(d.h, chKey, 0), mix(d.h, chKey, 1), mix(d.h, chKey, 2), mix(d.h, chKey, 3))
		d.sig = newSignals(d.h, p.cores)
		d.flows.period = d.period()
		devs = append(devs, d)
	}
	return devs
}

// dnsName is the device's MagicDNS FQDN (without trailing dot).
func (d *device) dnsName() string { return d.name + "." + MagicDNSSuffix }

// addresses returns the Tailscale IPs, IPv4 first.
func (d *device) addresses() []string { return []string{d.ip4, d.ip6} }

// endpoints returns the candidate WireGuard endpoints.
func (d *device) endpoints() []string {
	var out []string
	if d.lanIP != "" {
		out = append(out, fmt.Sprintf("%s:%d", d.lanIP, d.port))
	}
	if d.pubIP != "" {
		out = append(out, fmt.Sprintf("%s:%d", d.pubIP, d.port))
	}
	return out
}

// curAddr is the direct endpoint the hub uses when the path is direct.
func (d *device) curAddr() string {
	if !d.direct {
		return ""
	}
	if d.lanPeer && d.lanIP != "" {
		return fmt.Sprintf("%s:%d", d.lanIP, d.port)
	}
	if d.pubIP != "" {
		return fmt.Sprintf("%s:%d", d.pubIP, d.port)
	}
	return ""
}

// exitNodeOpt reports whether the device advertises itself as an exit node.
func (d *device) exitNodeOpt() bool {
	for _, r := range d.advertised {
		if isExitRoute(r) {
			return true
		}
	}
	return false
}

// exitEnabled reports whether its exit routes are approved.
func (d *device) exitEnabled() bool {
	for _, r := range d.enabledRoutes {
		if isExitRoute(r) {
			return true
		}
	}
	return false
}

// primaryRoutes returns the enabled non-exit routes.
func (d *device) primaryRoutes() []string {
	var out []string
	for _, r := range d.enabledRoutes {
		if !isExitRoute(r) {
			out = append(out, r)
		}
	}
	return out
}

// allowedIPs returns the WireGuard allowed IPs for the peer.
func (d *device) allowedIPs() []string {
	out := []string{d.ip4 + "/32", d.ip6 + "/128"}
	out = append(out, d.primaryRoutes()...)
	if d.exitEnabled() {
		out = append(out, exitRoutes...)
	}
	return out
}

// hasSensors reports whether the agent reports temperatures.
func (d *device) hasSensors() bool { return len(d.sensors) > 0 }

// primaryDisk returns the primary filesystem spec.
func (d *device) primaryDisk() diskSpec {
	for _, ds := range d.disks {
		if ds.primary {
			return ds
		}
	}
	if len(d.disks) > 0 {
		return d.disks[0]
	}
	return diskSpec{mount: "/", fstype: "ext4", total: 64 << 30, pct: 40, primary: true}
}

// preferredDERP returns the region with the lowest base latency.
func (d *device) preferredDERP() string {
	best, bestMs := "", 0.0
	for code, ms := range d.derp {
		if best == "" || ms < bestMs || (ms == bestMs && code < best) {
			best, bestMs = code, ms
		}
	}
	return best
}

func isExitRoute(r string) bool { return r == "0.0.0.0/0" || r == "::/0" }

// displayName returns the display name for a login.
func displayName(login string) string {
	if login == "" {
		return ""
	}
	if n, ok := users[login]; ok {
		return n
	}
	// Derive something sensible for unknown logins.
	local := login
	if i := strings.IndexByte(login, '@'); i > 0 {
		local = login[:i]
	}
	if local == "" {
		return login
	}
	return strings.ToUpper(local[:1]) + local[1:]
}

// parseRoutes validates and canonicalises CIDR routes.
func parseRoutes(routes []string) ([]string, error) {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		p, err := netip.ParsePrefix(strings.TrimSpace(r))
		if err != nil {
			return nil, fmt.Errorf("invalid route %q: %w", r, err)
		}
		out = append(out, p.Masked().String())
	}
	return out, nil
}

// Common NAT profiles.
var (
	natHome   = model.NATSupport{HairPinning: true, IPv6: false, PCP: false, PMP: false, UDP: true, UPnP: true}
	natOffice = model.NATSupport{HairPinning: false, IPv6: true, PCP: false, PMP: false, UDP: true, UPnP: false}
	natCloud  = model.NATSupport{HairPinning: false, IPv6: true, PCP: false, PMP: false, UDP: true, UPnP: false}
	natMobile = model.NATSupport{HairPinning: false, IPv6: true, PCP: false, PMP: false, UDP: true, UPnP: false}
	natMac    = model.NATSupport{HairPinning: true, IPv6: true, PCP: true, PMP: true, UDP: true, UPnP: false}
)

// DERP latency tables by rough location.
var (
	derpUSEast = map[string]float64{"nyc": 9, "sfo": 68, "fra": 88, "lhr": 76}
	derpUSWest = map[string]float64{"sfo": 8, "nyc": 66, "lhr": 132, "fra": 145}
	derpEU     = map[string]float64{"fra": 2, "lhr": 11, "nyc": 84, "sfo": 148}
	derpUK     = map[string]float64{"lhr": 6, "fra": 15, "nyc": 72, "sfo": 140}
	derpTravel = map[string]float64{"nyc": 14, "sfo": 72, "lhr": 79, "fra": 91}
	derpBranch = map[string]float64{"nyc": 22, "sfo": 60, "lhr": 95}
)

// fleetProfiles is the fixed fleet. Order is stable: it determines node IDs
// and Tailscale IPs.
func fleetProfiles() []profile {
	return []profile{
		{
			name: SelfName, hostname: "tailwatch-hub", os: "linux", goos: "linux", arch: "amd64",
			platform: "debian", platVer: "12", kernel: "6.1.0-25-amd64",
			cpuModel: "Intel(R) N100", cores: 4, memBytes: 8 << 30, swap: 2 << 30,
			disks:  []diskSpec{{"/", "/dev/nvme0n1p2", "ext4", 60 << 30, 41, true}},
			physIf: "eth0", tsIf: "tailscale0",
			tags: []string{"tag:server"}, agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "192.168.1.10", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			clientVersion: ClientVersion, keyExpiryDays: 0, createdDaysAgo: 210, rebootPeriod: 30 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 10, cpuAmp: 8, memPct: 46, latency: 0, procs: 140,
			flows: [nFlows]float64{0, 0, 120e3, 40e3, 0, 0},
		},
		{
			name: "nas", hostname: "DS920", os: "linux", goos: "linux", arch: "amd64", model: "Synology DS920+",
			platform: "synology", platVer: "7.2", kernel: "4.4.302+",
			cpuModel: "Intel(R) Celeron(R) J4125", cores: 4, memBytes: 8 << 30, swap: 4 << 30,
			disks: []diskSpec{
				{"/", "/dev/md0", "ext4", 8 << 30, 63, true},
				{"/volume1", "/dev/mapper/cachedev_0", "btrfs", 14 << 40, 71, false},
				{"/volume2", "/dev/mapper/cachedev_1", "btrfs", 8 << 40, 38, false},
			},
			physIf: "eth0", tsIf: "tailscale0", sensors: []string{"hdd0", "hdd1"}, tempBase: 38,
			tags: []string{"tag:server"}, agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "192.168.1.20", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			clientVersion: ClientVersion, keyExpiryDays: 0, createdDaysAgo: 400, rebootPeriod: 90 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 8, cpuAmp: 6, memPct: 38, latency: 2.1, procs: 210,
			flows: [nFlows]float64{60e3, 25e3, 900e3, 600e3, 90e3, 40e3}, backup: true,
		},
		{
			name: "pi-hole", hostname: "pihole", os: "linux", goos: "linux", arch: "arm64", model: "Raspberry Pi 4 Model B Rev 1.5",
			platform: "raspbian", platVer: "12", kernel: "6.6.31+rpt-rpi-v8",
			cpuModel: "ARM Cortex-A72", cores: 4, memBytes: 4 << 30, swap: 100 << 20,
			disks:  []diskSpec{{"/", "/dev/mmcblk0p2", "ext4", 32 << 30, 27, true}},
			physIf: "eth0", tsIf: "tailscale0", sensors: []string{"cpu_thermal"}, tempBase: 52,
			user: "alice@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "192.168.1.30", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			clientVersion: ClientVersion, keyExpiryDays: 120, createdDaysAgo: 380, rebootPeriod: 45 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 6, cpuAmp: 4, memPct: 31, latency: 2.6, procs: 120,
			flows: [nFlows]float64{12e3, 8e3, 60e3, 25e3, 18e3, 12e3},
		},
		{
			name: "homelab", hostname: "homelab", os: "linux", goos: "linux", arch: "amd64",
			platform: "ubuntu", platVer: "24.04", kernel: "6.8.0-45-generic",
			cpuModel: "AMD Ryzen 7 5800X 8-Core Processor", cores: 16, memBytes: 64 << 30, swap: 8 << 30,
			disks: []diskSpec{
				{"/", "/dev/nvme0n1p2", "ext4", 1 << 40, 55, true},
				{"/data", "tank/data", "zfs", 8 << 40, 62, false},
			},
			physIf: "enp6s0", tsIf: "tailscale0", sensors: []string{"coretemp Package id 0"}, tempBase: 46,
			tags: []string{"tag:server"}, agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "192.168.1.40", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			advertised: []string{"0.0.0.0/0", "::/0", "192.168.1.0/24"}, routesEnabled: true,
			clientVersion: ClientVersion, keyExpiryDays: 0, createdDaysAgo: 500, rebootPeriod: 60 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 22, cpuAmp: 15, memPct: 58, latency: 2.3, procs: 480,
			flows: [nFlows]float64{150e3, 90e3, 2.5e6, 1.8e6, 260e3, 170e3},
		},
		{
			name: "cloud-vm", hostname: "cloud-vm", os: "linux", goos: "linux", arch: "amd64",
			platform: "ubuntu", platVer: "22.04", kernel: "5.15.0-119-generic",
			cpuModel: "Intel(R) Xeon(R) Platinum 8375C CPU @ 2.90GHz", cores: 2, memBytes: 4 << 30, swap: 0,
			disks:  []diskSpec{{"/", "/dev/vda1", "ext4", 80 << 30, 44, true}},
			physIf: "ens5", tsIf: "tailscale0",
			tags: []string{"tag:server"}, agent: agentYes, presence: onlineAlways, authorized: true,
			direct: false, relay: "fra", pubIP: "198.51.100.7", port: 41641,
			exitNodeInUse: true, advertised: []string{"0.0.0.0/0", "::/0"}, routesEnabled: true,
			clientVersion: ClientVersion, keyExpiryDays: 0, createdDaysAgo: 300, rebootPeriod: 120 * day,
			nat: natCloud, derp: derpEU,
			location: &model.Location{Country: "Germany", CountryCode: "DE", City: "Frankfurt", Latitude: 50.1109, Longitude: 8.6821},
			cpuBase:  18, cpuAmp: 12, memPct: 52, latency: 88, procs: 160,
			flows: [nFlows]float64{320e3, 210e3, 900e3, 700e3, 480e3, 320e3},
		},
		{
			name: "alice-mbp", hostname: "Alices-MacBook-Pro", os: "macOS", goos: "darwin", arch: "arm64", model: "MacBook Pro (14-inch, 2023)",
			platform: "darwin", platVer: "15.1", kernel: "24.1.0",
			cpuModel: "Apple M3 Pro", cores: 12, memBytes: 36 << 30, swap: 2 << 30,
			disks:  []diskSpec{{"/", "/dev/disk3s1s1", "apfs", 1 << 40, 58, true}},
			physIf: "en0", tsIf: "utun4",
			user: "alice@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "10.20.0.14", pubIP: "203.0.113.55", port: 41641,
			clientVersion: ClientVersion, keyExpiryDays: 95, createdDaysAgo: 250, rebootPeriod: 3 * day,
			nat: natMac, derp: derpTravel,
			cpuBase: 14, cpuAmp: 10, memPct: 63, latency: 9, procs: 620,
			flows: [nFlows]float64{40e3, 25e3, 300e3, 120e3, 60e3, 40e3},
		},
		{
			name: "alice-iphone", hostname: "alices-iphone", os: "iOS", model: "iPhone 15 Pro",
			platform: "ios", platVer: "18.1",
			user: "alice@example.com", agent: agentNone, presence: onlineFlap, authorized: true,
			direct: false, relay: "nyc", pubIP: "203.0.113.120", port: 41641, mapVaries: true,
			clientVersion: ClientVersion, keyExpiryDays: 140, createdDaysAgo: 200,
			nat: natMobile, derp: derpUSEast,
			latency: 58, flows: [nFlows]float64{8e3, 12e3, 0, 0, 12e3, 18e3},
		},
		{
			name: "alice-ipad", hostname: "alices-ipad", os: "iOS", model: "iPad Air (5th generation)",
			platform: "ios", platVer: "17.6",
			user: "alice@example.com", agent: agentNone, presence: onlineNever, authorized: true,
			direct: false, relay: "nyc", pubIP: "203.0.113.121", port: 41641, blocksIncoming: true,
			clientVersion: ClientVersion, keyExpiryDays: 40, createdDaysAgo: 180,
			nat: natMobile, derp: derpUSEast,
			latency: 62, flows: [nFlows]float64{3e3, 5e3, 0, 0, 4e3, 6e3},
		},
		{
			name: "bob-desktop", hostname: "DESKTOP-7GK2Q1", os: "windows", goos: "windows", arch: "amd64",
			platform: "Microsoft Windows 11 Pro", platVer: "10.0.22631 Build 22631", kernel: "10.0.22631",
			cpuModel: "12th Gen Intel(R) Core(TM) i7-12700K", cores: 20, memBytes: 32 << 30, swap: 4 << 30,
			disks: []diskSpec{
				{"C:", "C:", "NTFS", 1 << 40, 67, true},
				{"D:", "D:", "NTFS", 2 << 40, 81, false},
			},
			physIf: "Ethernet", tsIf: "Tailscale",
			user: "bob@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "sfo", lanIP: "192.168.50.12", pubIP: "198.51.100.23", port: 41641,
			clientVersion: ClientVersion, keyExpiryDays: 70, createdDaysAgo: 320, rebootPeriod: 7 * day,
			nat: natOffice, derp: derpUSWest,
			cpuBase: 15, cpuAmp: 10, cpuSpiky: true, memPct: 54, latency: 12, procs: 310,
			flows: [nFlows]float64{70e3, 30e3, 1.2e6, 300e3, 100e3, 45e3},
		},
		{
			name: "bob-pixel", hostname: "Pixel-8", os: "android", model: "Pixel 8",
			platform: "android", platVer: "15",
			user: "bob@example.com", agent: agentNone, presence: onlineAlways, authorized: true,
			direct: false, relay: "sfo", pubIP: "198.51.100.200", port: 41641, mapVaries: true,
			clientVersion: ClientVersion, keyExpiryDays: 110, createdDaysAgo: 150,
			nat: natMobile, derp: derpUSWest,
			latency: 96, flows: [nFlows]float64{6e3, 9e3, 0, 0, 9e3, 13e3},
		},
		{
			name: "office-printer-gw", hostname: "printer-gw", os: "linux", goos: "linux", arch: "arm", model: "Raspberry Pi 3 Model B Rev 1.2",
			platform: "debian", platVer: "11", kernel: "5.10.103-v7+",
			cpuModel: "ARMv7 Processor rev 4 (v7l)", cores: 4, memBytes: 1 << 30, swap: 100 << 20,
			disks:  []diskSpec{{"/", "/dev/mmcblk0p2", "ext4", 16 << 30, 48, true}},
			physIf: "eth0", tsIf: "tailscale0", sensors: []string{"cpu_thermal"}, tempBase: 47,
			user: "bob@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "172.16.4.9", pubIP: "192.0.2.44", port: 41641,
			clientVersion: ClientVersion, keyExpiryDays: 3, createdDaysAgo: 177, rebootPeriod: 30 * day,
			nat: natOffice, derp: derpBranch,
			cpuBase: 4, cpuAmp: 3, memPct: 42, latency: 14, procs: 95,
			flows: [nFlows]float64{3e3, 2e3, 20e3, 8e3, 4e3, 3e3},
		},
		{
			name: "old-laptop", hostname: "LAPTOP-OLD", os: "windows", goos: "windows", arch: "amd64",
			platform: "Microsoft Windows 10 Home", platVer: "10.0.19045 Build 19045", kernel: "10.0.19045",
			cpuModel: "Intel(R) Core(TM) i5-6200U CPU @ 2.30GHz", cores: 4, memBytes: 8 << 30, swap: 2 << 30,
			disks:  []diskSpec{{"C:", "C:", "NTFS", 256 << 30, 88, true}},
			physIf: "Wi-Fi", tsIf: "Tailscale",
			user: "bob@example.com", agent: agentYes, presence: onlineBlocks, authorized: true,
			direct: true, relay: "sfo", lanIP: "192.168.50.77", pubIP: "192.0.2.77", port: 41641,
			clientVersion: OldClientVersion, updateAvailable: true, keyExpiryDays: 25, createdDaysAgo: 700,
			nat: natOffice, derp: derpUSWest,
			cpuBase: 25, cpuAmp: 15, memPct: 71, latency: 18, procs: 180,
			flows: [nFlows]float64{15e3, 6e3, 80e3, 20e3, 20e3, 9e3},
		},
		{
			name: "workshop-pi", hostname: "workshop-pi", os: "linux", goos: "linux", arch: "arm64", model: "Raspberry Pi 5 Model B Rev 1.0",
			platform: "raspbian", platVer: "12", kernel: "6.6.31+rpt-rpi-2712",
			cpuModel: "ARM Cortex-A76", cores: 4, memBytes: 8 << 30, swap: 200 << 20,
			disks:  []diskSpec{{"/", "/dev/mmcblk0p2", "ext4", 64 << 30, 19, true}},
			physIf: "eth0", tsIf: "tailscale0", sensors: []string{"cpu_thermal"}, tempBase: 49,
			user: "alice@example.com", agent: agentForbidden, presence: onlineAlways, authorized: false,
			direct: true, relay: "nyc", lanIP: "192.168.1.50", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			clientVersion: ClientVersion, keyExpiryDays: 178, createdDaysAgo: 2, rebootPeriod: 45 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 5, cpuAmp: 4, memPct: 24, latency: 2.8, procs: 110,
			flows: [nFlows]float64{2e3, 1e3, 15e3, 6e3, 3e3, 2e3},
		},
		{
			name: "shared-node", hostname: "shared-node", os: "linux", goos: "linux", arch: "amd64",
			platform: "ubuntu", platVer: "24.04", kernel: "6.8.0-40-generic",
			cpuModel: "Intel(R) Xeon(R) CPU E5-2680 v4", cores: 2, memBytes: 4 << 30,
			disks:  []diskSpec{{"/", "/dev/sda1", "ext4", 40 << 30, 35, true}},
			physIf: "eth0", tsIf: "tailscale0",
			user: "carol@othercorp.example", agent: agentNone, presence: onlineAlways, authorized: true, external: true,
			direct: false, relay: "lhr", pubIP: "198.51.100.150", port: 41641,
			clientVersion: ClientVersion, keyExpiryDays: 60, createdDaysAgo: 90,
			nat: natCloud, derp: derpUK,
			location: &model.Location{Country: "United Kingdom", CountryCode: "GB", City: "London", Latitude: 51.5072, Longitude: -0.1276},
			cpuBase:  9, cpuAmp: 5, memPct: 40, latency: 82, procs: 120,
			flows: [nFlows]float64{20e3, 15e3, 120e3, 80e3, 30e3, 22e3},
		},
		{
			name: "media-box", hostname: "media-box", os: "linux", goos: "linux", arch: "amd64",
			platform: "ubuntu", platVer: "22.04", kernel: "5.15.0-122-generic",
			cpuModel: "Intel(R) Celeron(R) N5105 @ 2.00GHz", cores: 4, memBytes: 16 << 30, swap: 4 << 30,
			disks: []diskSpec{
				{"/", "/dev/nvme0n1p2", "ext4", 240 << 30, 96, true},
				{"/mnt/media", "/dev/sda1", "ext4", 4 << 40, 89, false},
			},
			physIf: "enp2s0", tsIf: "tailscale0",
			user: "alice@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "nyc", lanIP: "192.168.1.60", pubIP: "203.0.113.10", port: 41641, lanPeer: true,
			clientVersion: ClientVersion, keyExpiryDays: 88, createdDaysAgo: 420, rebootPeriod: 14 * day,
			nat: natHome, derp: derpUSEast,
			cpuBase: 35, cpuAmp: 20, memPct: 47, latency: 2.4, procs: 260,
			flows: [nFlows]float64{250e3, 30e3, 3e6, 400e3, 400e3, 50e3},
		},
		{
			name: "dev-box", hostname: "dev-box", os: "freebsd", goos: "freebsd", arch: "amd64",
			platform: "freebsd", platVer: "14.1-RELEASE", kernel: "14.1-RELEASE",
			cpuModel: "AMD Ryzen 5 5600G with Radeon Graphics", cores: 12, memBytes: 32 << 30, swap: 8 << 30,
			disks:  []diskSpec{{"/", "zroot/ROOT/default", "zfs", 2 << 40, 61, true}},
			physIf: "igb0", tsIf: "tailscale0",
			user: "bob@example.com", agent: agentYes, presence: onlineAlways, authorized: true,
			direct: true, relay: "sfo", lanIP: "192.168.50.30", pubIP: "198.51.100.90", port: 41641,
			clientVersion: ClientVersion, keyExpiryDays: 130, createdDaysAgo: 260, rebootPeriod: 20 * day,
			nat: natOffice, derp: derpUSWest,
			cpuBase: 30, cpuAmp: 25, memPct: 92, latency: 7, procs: 350,
			flows: [nFlows]float64{90e3, 60e3, 500e3, 250e3, 140e3, 90e3},
		},
	}
}
