// Package model holds the domain types shared by the collector, store, alert
// engine and HTTP API. Every JSON tag here is part of the public API contract
// documented in docs/API.md and mirrored in web/src/api/types.ts. Do not rename
// fields without updating both.
package model

import "time"

// DeviceID is the Tailscale stable node ID (e.g. "nTLzc5Cf3d11CNTRL").
// It is the same value as ipnstate.PeerStatus.ID and the control API's
// "nodeId" field, so both sources can be merged on it.
type DeviceID string

// PathType describes how the hub currently reaches a peer.
type PathType string

const (
	PathDirect  PathType = "direct" // direct WireGuard endpoint
	PathRelay   PathType = "relay"  // via a DERP relay
	PathNone    PathType = "none"   // no active path (offline / never contacted)
	PathUnknown PathType = "unknown"
)

// AgentState describes whether the per-device tailwatch-agent is reachable.
type AgentState string

const (
	AgentReachable   AgentState = "reachable"
	AgentUnreachable AgentState = "unreachable" // tried recently, failed
	AgentUnknown     AgentState = "unknown"     // never tried / device offline
	AgentDisabled    AgentState = "disabled"    // agent collection disabled by config
)

// Severity levels used by events and alerts.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Location is the optional geographic location of a node (typically exit nodes
// such as Mullvad or nodes with a configured location).
type Location struct {
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"countryCode,omitempty"`
	City        string  `json:"city,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
}

// NATSupport mirrors the control API's clientConnectivity.clientSupports.
type NATSupport struct {
	HairPinning bool `json:"hairPinning"`
	IPv6        bool `json:"ipv6"`
	PCP         bool `json:"pcp"`
	PMP         bool `json:"pmp"`
	UDP         bool `json:"udp"`
	UPnP        bool `json:"upnp"`
}

// Connectivity is the hub's view of its connection to a peer plus the
// control-plane's connectivity report for that device.
type Connectivity struct {
	Path      PathType `json:"path"`
	Relay     string   `json:"relay,omitempty"`     // DERP region code when Path == relay (e.g. "nyc")
	CurAddr   string   `json:"curAddr,omitempty"`   // current direct endpoint ip:port
	Endpoints []string `json:"endpoints,omitempty"` // candidate endpoints reported by the control plane

	LatencyMs *float64   `json:"latencyMs,omitempty"` // last disco ping RTT from hub, nil if never/unavailable
	LastPing  *time.Time `json:"lastPing,omitempty"`

	RxBytes int64   `json:"rxBytes"` // cumulative bytes hub received from peer
	TxBytes int64   `json:"txBytes"` // cumulative bytes hub sent to peer
	RxRate  float64 `json:"rxRate"`  // bytes/second, from delta between last two samples
	TxRate  float64 `json:"txRate"`

	DERPLatencyMs         map[string]float64 `json:"derpLatencyMs,omitempty"` // region code -> ms, from control API
	PreferredDERP         string             `json:"preferredDerp,omitempty"`
	NATSupport            *NATSupport        `json:"natSupport,omitempty"`
	MappingVariesByDestIP *bool              `json:"mappingVariesByDestIp,omitempty"`
}

// AgentStatus is the state of the tailwatch-agent on a device.
type AgentStatus struct {
	State       AgentState `json:"state"`
	Version     string     `json:"version,omitempty"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	URL         string     `json:"url,omitempty"` // e.g. http://100.64.0.5:41820
}

// DiskUsage describes one mounted filesystem.
type DiskUsage struct {
	Mount   string  `json:"mount"`
	FSType  string  `json:"fstype,omitempty"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

// InterfaceCounters are cumulative counters for one network interface.
type InterfaceCounters struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rxBytes"`
	TxBytes   uint64 `json:"txBytes"`
	RxPackets uint64 `json:"rxPackets"`
	TxPackets uint64 `json:"txPackets"`
	RxErrors  uint64 `json:"rxErrors"`
	TxErrors  uint64 `json:"txErrors"`
	// Rates are computed by the hub from consecutive samples (bytes/s).
	RxRate float64 `json:"rxRate"`
	TxRate float64 `json:"txRate"`
}

// Temperature is one sensor reading.
type Temperature struct {
	Sensor   string  `json:"sensor"`
	Celsius  float64 `json:"celsius"`
	Critical float64 `json:"critical,omitempty"`
}

// MetricsSnapshot is the latest system metrics reported by the agent, enriched
// with hub-computed rates.
type MetricsSnapshot struct {
	SampledAt time.Time `json:"sampledAt"`

	Platform        string     `json:"platform,omitempty"`        // "ubuntu", "darwin", "windows"
	PlatformVersion string     `json:"platformVersion,omitempty"` // "24.04"
	Kernel          string     `json:"kernel,omitempty"`
	Arch            string     `json:"arch,omitempty"`
	BootTime        *time.Time `json:"bootTime,omitempty"`
	UptimeSeconds   uint64     `json:"uptimeSeconds"`

	CPUPercent float64   `json:"cpuPercent"`
	CPUCount   int       `json:"cpuCount"`
	CPUModel   string    `json:"cpuModel,omitempty"`
	PerCore    []float64 `json:"perCore,omitempty"`
	Load1      float64   `json:"load1"`
	Load5      float64   `json:"load5"`
	Load15     float64   `json:"load15"`

	MemTotal     uint64  `json:"memTotal"`
	MemUsed      uint64  `json:"memUsed"`
	MemAvailable uint64  `json:"memAvailable"`
	MemPercent   float64 `json:"memPercent"`
	SwapTotal    uint64  `json:"swapTotal"`
	SwapUsed     uint64  `json:"swapUsed"`

	Disks       []DiskUsage `json:"disks"`
	DiskPercent float64     `json:"diskPercent"` // usage of the root/primary volume (max of Disks marked primary)

	Interfaces  []InterfaceCounters `json:"interfaces"`
	TailscaleIf string              `json:"tailscaleInterface,omitempty"` // e.g. "tailscale0" or "utun4"
	NetRxRate   float64             `json:"netRxRate"`                    // total bytes/s across all physical interfaces
	NetTxRate   float64             `json:"netTxRate"`

	Temperatures []Temperature `json:"temperatures,omitempty"`
	Processes    int           `json:"processes"`
	TailscaleVer string        `json:"tailscaleVersion,omitempty"`
}

// UptimeStats summarises observed availability.
type UptimeStats struct {
	Pct24h     *float64   `json:"pct24h,omitempty"` // 0..100 based on online samples, nil if no data
	Pct7d      *float64   `json:"pct7d,omitempty"`
	Pct30d     *float64   `json:"pct30d,omitempty"`
	LastChange *time.Time `json:"lastChange,omitempty"` // last online<->offline transition
	OnlineFor  *int64     `json:"onlineForSeconds,omitempty"`
}

// Device is the merged view of a tailnet node from all sources.
type Device struct {
	ID       DeviceID `json:"id"`
	Name     string   `json:"name"`     // MagicDNS base name (e.g. "laptop")
	DNSName  string   `json:"dnsName"`  // FQDN without trailing dot
	Hostname string   `json:"hostname"` // as reported by the node
	OS       string   `json:"os"`       // "linux","macOS","windows","iOS","android","freebsd","tvOS", ...
	Model    string   `json:"deviceModel,omitempty"`

	Addresses []string `json:"addresses"` // Tailscale IPs (v4 first)
	User      string   `json:"user"`      // owner login name; empty for tagged devices
	UserName  string   `json:"userDisplayName,omitempty"`
	Tags      []string `json:"tags"`

	IsSelf     bool `json:"isSelf"`     // the hub node itself
	IsExternal bool `json:"isExternal"` // shared into this tailnet from another
	Online     bool `json:"online"`
	Active     bool `json:"active"` // traffic with hub recently

	LastSeen      time.Time  `json:"lastSeen"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"`
	Created       time.Time  `json:"created"`
	FirstSeen     time.Time  `json:"firstSeen"` // first observed by tailwatch
	UpdatedAt     time.Time  `json:"updatedAt"`

	ClientVersion   string `json:"clientVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Authorized      bool   `json:"authorized"`

	KeyExpiry         *time.Time `json:"keyExpiry,omitempty"`
	KeyExpiryDisabled bool       `json:"keyExpiryDisabled"`
	Expired           bool       `json:"expired"`

	ExitNodeOption   bool     `json:"exitNodeOption"` // advertises itself as an exit node
	IsExitNode       bool     `json:"isExitNode"`     // currently used as exit node by the hub
	AdvertisedRoutes []string `json:"advertisedRoutes"`
	EnabledRoutes    []string `json:"enabledRoutes"`
	PrimaryRoutes    []string `json:"primaryRoutes"`
	BlocksIncoming   bool     `json:"blocksIncomingConnections"`

	Location     *Location        `json:"location,omitempty"`
	Connectivity Connectivity     `json:"connectivity"`
	Agent        AgentStatus      `json:"agent"`
	Metrics      *MetricsSnapshot `json:"metrics,omitempty"`
	Uptime       UptimeStats      `json:"uptime"`
}

// PrimaryIP returns the first IPv4 address or the first address.
func (d *Device) PrimaryIP() string {
	for _, a := range d.Addresses {
		if len(a) > 0 && a[0] != ':' && !containsColon(a) {
			return a
		}
	}
	if len(d.Addresses) > 0 {
		return d.Addresses[0]
	}
	return ""
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

// Sample is one raw time-series row per device per collector tick.
// Pointer fields are nil when the corresponding source had no data.
type Sample struct {
	DeviceID DeviceID  `json:"deviceId"`
	TS       time.Time `json:"ts"`

	Online    bool     `json:"online"`
	LatencyMs *float64 `json:"latencyMs,omitempty"`
	Relay     string   `json:"relay,omitempty"`
	Direct    *bool    `json:"direct,omitempty"`

	TSRxBytes int64   `json:"tsRxBytes"` // cumulative, hub<->peer
	TSTxBytes int64   `json:"tsTxBytes"`
	TSRxRate  float64 `json:"tsRxRate"` // bytes/s
	TSTxRate  float64 `json:"tsTxRate"`

	AgentOK   bool     `json:"agentOk"`
	CPU       *float64 `json:"cpu,omitempty"`
	Mem       *float64 `json:"mem,omitempty"`
	Disk      *float64 `json:"disk,omitempty"`
	Load1     *float64 `json:"load1,omitempty"`
	NetRxRate *float64 `json:"netRxRate,omitempty"` // device total bytes/s
	NetTxRate *float64 `json:"netTxRate,omitempty"`
	TempC     *float64 `json:"tempC,omitempty"`
	Uptime    *uint64  `json:"uptimeSeconds,omitempty"`
}

// SeriesPoint is one point of a queried time series. It is produced either
// from raw samples (short ranges) or from rollups (long ranges). Averages are
// used for rolled-up values; Max fields are populated for rollups only.
type SeriesPoint struct {
	T          int64    `json:"t"`                // unix seconds (bucket start)
	Online     float64  `json:"online"`           // 0..1 ratio in bucket
	Direct     *float64 `json:"direct,omitempty"` // 0..1 ratio of samples with a direct path
	LatencyMs  *float64 `json:"latencyMs,omitempty"`
	LatencyMax *float64 `json:"latencyMax,omitempty"`
	TSRxRate   float64  `json:"tsRxRate"`
	TSTxRate   float64  `json:"tsTxRate"`
	CPU        *float64 `json:"cpu,omitempty"`
	CPUMax     *float64 `json:"cpuMax,omitempty"`
	Mem        *float64 `json:"mem,omitempty"`
	Disk       *float64 `json:"disk,omitempty"`
	Load1      *float64 `json:"load1,omitempty"`
	NetRxRate  *float64 `json:"netRxRate,omitempty"`
	NetTxRate  *float64 `json:"netTxRate,omitempty"`
	TempC      *float64 `json:"tempC,omitempty"`
}

// Series is the response of the series endpoint.
type Series struct {
	DeviceID DeviceID      `json:"deviceId"`
	From     time.Time     `json:"from"`
	To       time.Time     `json:"to"`
	StepSec  int64         `json:"stepSeconds"`
	Source   string        `json:"source"` // "raw" or "rollup"
	Points   []SeriesPoint `json:"points"`
}

// TimelineSegment is a contiguous online/offline period.
type TimelineSegment struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Online bool      `json:"online"`
}

// UptimeReport is the response of the uptime endpoint.
type UptimeReport struct {
	DeviceID DeviceID          `json:"deviceId"`
	From     time.Time         `json:"from"`
	To       time.Time         `json:"to"`
	Pct      float64           `json:"pct"` // 0..100
	Segments []TimelineSegment `json:"segments"`
	Outages  int               `json:"outages"`
}

// EventType enumerates events emitted by the collector and alert engine.
type EventType string

const (
	EventDeviceOnline     EventType = "device.online"
	EventDeviceOffline    EventType = "device.offline"
	EventDeviceNew        EventType = "device.new"
	EventDeviceRemoved    EventType = "device.removed"
	EventDeviceUpdated    EventType = "device.updated" // e.g. version/OS/tags/routes changed
	EventDeviceAuthorized EventType = "device.authorized"
	EventDeviceExpired    EventType = "device.expired"
	EventPathChanged      EventType = "device.path_changed" // direct <-> relay
	EventAgentReachable   EventType = "agent.reachable"
	EventAgentUnreachable EventType = "agent.unreachable"
	EventAlertOpened      EventType = "alert.opened"
	EventAlertResolved    EventType = "alert.resolved"
	EventAlertAcked       EventType = "alert.acked"
	EventAdminAction      EventType = "admin.action"
	EventHubStarted       EventType = "hub.started"
	EventHubError         EventType = "hub.error"
)

// Event is an append-only record of something that happened.
type Event struct {
	ID       int64          `json:"id"`
	TS       time.Time      `json:"ts"`
	Type     EventType      `json:"type"`
	Severity Severity       `json:"severity"`
	DeviceID DeviceID       `json:"deviceId,omitempty"`
	Device   string         `json:"deviceName,omitempty"`
	Title    string         `json:"title"`
	Message  string         `json:"message,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// EventQuery filters ListEvents.
type EventQuery struct {
	DeviceID DeviceID
	Types    []EventType
	Since    time.Time
	Before   time.Time
	Limit    int // default 100, max 1000
}

// AlertState is the lifecycle state of an alert.
type AlertState string

const (
	AlertOpen     AlertState = "open"
	AlertResolved AlertState = "resolved"
)

// AlertRuleType enumerates built-in rule kinds. Each rule type has a fixed
// set of parameters; see docs/API.md.
type AlertRuleType string

const (
	RuleDeviceOffline    AlertRuleType = "device_offline"      // offline for > ForSeconds
	RuleHighCPU          AlertRuleType = "high_cpu"            // cpu% > Threshold for ForSeconds
	RuleHighMemory       AlertRuleType = "high_memory"         // mem% > Threshold for ForSeconds
	RuleDiskFull         AlertRuleType = "disk_full"           // disk% > Threshold
	RuleHighLatency      AlertRuleType = "high_latency"        // latency ms > Threshold for ForSeconds
	RuleRelayOnly        AlertRuleType = "relay_only"          // path == relay for > ForSeconds
	RuleKeyExpiring      AlertRuleType = "key_expiring"        // key expires within Threshold days
	RuleUpdateAvailable  AlertRuleType = "update_available"    // client update available
	RuleAgentUnreachable AlertRuleType = "agent_unreachable"   // agent was reachable, now not, for ForSeconds
	RuleNewDevice        AlertRuleType = "new_device"          // new device joined (auto-resolves after ForSeconds)
	RuleUnauthorized     AlertRuleType = "unauthorized_device" // device awaiting authorization
	RuleHighTemp         AlertRuleType = "high_temperature"    // temp C > Threshold
	RuleHighLoad         AlertRuleType = "high_load"           // load1 / cpuCount > Threshold for ForSeconds
)

// AlertRule is a user-tunable rule. Rules are keyed by ID; the built-in
// defaults use the rule type as the ID.
type AlertRule struct {
	ID          string        `json:"id"`
	Type        AlertRuleType `json:"type"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Enabled     bool          `json:"enabled"`
	Severity    Severity      `json:"severity"`
	Threshold   float64       `json:"threshold"`  // meaning depends on Type (percent, ms, days, ratio)
	ForSeconds  int           `json:"forSeconds"` // condition must hold this long before firing (0 = immediately)
	// Optional scoping. Empty = all devices.
	IncludeTags   []string   `json:"includeTags,omitempty"`
	ExcludeTags   []string   `json:"excludeTags,omitempty"`
	IncludeDevice []DeviceID `json:"includeDevices,omitempty"`
	ExcludeDevice []DeviceID `json:"excludeDevices,omitempty"`
	Notify        bool       `json:"notify"` // send to configured notifiers
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// Alert is an instance of a rule firing for a device (or the hub).
type Alert struct {
	ID         int64          `json:"id"`
	RuleID     string         `json:"ruleId"`
	RuleType   AlertRuleType  `json:"ruleType"`
	DeviceID   DeviceID       `json:"deviceId,omitempty"`
	DeviceName string         `json:"deviceName,omitempty"`
	State      AlertState     `json:"state"`
	Severity   Severity       `json:"severity"`
	Title      string         `json:"title"`
	Message    string         `json:"message"`
	Value      *float64       `json:"value,omitempty"` // last observed value that triggered
	OpenedAt   time.Time      `json:"openedAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	ResolvedAt *time.Time     `json:"resolvedAt,omitempty"`
	AckedAt    *time.Time     `json:"ackedAt,omitempty"`
	AckedBy    string         `json:"ackedBy,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

// Key identifies the logical alert (one open alert per rule+device).
func (a Alert) Key() string { return a.RuleID + "|" + string(a.DeviceID) }

// AlertQuery filters ListAlerts.
type AlertQuery struct {
	State    AlertState // "" = all
	DeviceID DeviceID
	Limit    int // default 200
}

// AuditEntry records an administrative action.
type AuditEntry struct {
	ID        int64          `json:"id"`
	TS        time.Time      `json:"ts"`
	Actor     string         `json:"actor"`     // login name
	ActorNode string         `json:"actorNode"` // node the request came from
	Action    string         `json:"action"`    // e.g. "device.authorize"
	Target    string         `json:"target"`    // device id or rule id
	Details   map[string]any `json:"details,omitempty"`
	OK        bool           `json:"ok"`
	Error     string         `json:"error,omitempty"`
	RemoteIP  string         `json:"remoteIp,omitempty"`
}

// Role is the authorization level of an authenticated user.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleAdmin  Role = "admin"
)

// Identity is the authenticated caller of an HTTP request.
type Identity struct {
	Login       string   `json:"login"`       // e.g. "alice@example.com"; for tagged nodes: "tagged-device"
	DisplayName string   `json:"displayName"` // e.g. "Alice Smith"
	ProfilePic  string   `json:"profilePicUrl,omitempty"`
	NodeName    string   `json:"nodeName"` // MagicDNS name of the requesting node
	NodeID      DeviceID `json:"nodeId"`
	NodeIP      string   `json:"nodeIp"`
	Tags        []string `json:"tags,omitempty"`
	Role        Role     `json:"role"`
	AuthMode    string   `json:"authMode"` // "tailscale" or "none"
}

// HubInfo describes the hub node and tailnet.
type HubInfo struct {
	Version         string     `json:"version"`
	Tailnet         string     `json:"tailnet"`
	MagicDNSSuffix  string     `json:"magicDnsSuffix"`
	SelfName        string     `json:"selfName"`
	SelfID          DeviceID   `json:"selfId"`
	SelfIPs         []string   `json:"selfIps"`
	TailscaleVer    string     `json:"tailscaleVersion"`
	BackendState    string     `json:"backendState"` // "Running", "NeedsLogin", ...
	Health          []string   `json:"health"`       // tailscaled health warnings
	StartedAt       time.Time  `json:"startedAt"`
	DemoMode        bool       `json:"demoMode"`
	ControlAPI      bool       `json:"controlApiEnabled"`
	AdminActions    bool       `json:"adminActionsEnabled"`
	AgentPort       int        `json:"agentPort"`
	PollIntervalSec int        `json:"pollIntervalSeconds"`
	LastPoll        time.Time  `json:"lastPoll"`
	LastAPIPoll     *time.Time `json:"lastApiPoll,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
}

// Overview is the dashboard summary.
type Overview struct {
	Hub            HubInfo `json:"hub"`
	Devices        int     `json:"devices"`
	Online         int     `json:"online"`
	Offline        int     `json:"offline"`
	Direct         int     `json:"direct"`
	Relayed        int     `json:"relayed"`
	AgentsUp       int     `json:"agentsReachable"`
	UpdatesPending int     `json:"updatesPending"`
	KeysExpiring   int     `json:"keysExpiringSoon"` // within 7 days
	Unauthorized   int     `json:"unauthorized"`
	ExitNodes      int     `json:"exitNodes"`
	SubnetRouters  int     `json:"subnetRouters"`
	OpenAlerts     int     `json:"openAlerts"`
	CriticalAlerts int     `json:"criticalAlerts"`

	TotalRxRate  float64 `json:"totalRxRate"` // sum of hub<->peer rates, bytes/s
	TotalTxRate  float64 `json:"totalTxRate"`
	TotalRxBytes int64   `json:"totalRxBytes"` // cumulative since tailscaled start
	TotalTxBytes int64   `json:"totalTxBytes"`

	AvgLatencyMs  *float64       `json:"avgLatencyMs,omitempty"`
	OSBreakdown   map[string]int `json:"osBreakdown"`
	UserBreakdown map[string]int `json:"userBreakdown"`

	// Recent short series for sparklines (last 60 minutes, 1-minute buckets).
	Sparklines OverviewSparklines `json:"sparklines"`
}

// OverviewSparklines are small network-wide series for the dashboard.
type OverviewSparklines struct {
	T       []int64   `json:"t"`      // unix seconds
	Online  []float64 `json:"online"` // count of online devices
	RxRate  []float64 `json:"rxRate"` // bytes/s total
	TxRate  []float64 `json:"txRate"`
	Latency []float64 `json:"latency"` // avg ms (NaN encoded as 0 with no data; UI treats 0 as gap when online==0)
}

// TopologyNode is a node in the network map.
type TopologyNode struct {
	ID        DeviceID `json:"id"`
	Name      string   `json:"name"`
	OS        string   `json:"os"`
	Online    bool     `json:"online"`
	IsSelf    bool     `json:"isSelf"`
	IsExit    bool     `json:"isExitNode"`
	IsRouter  bool     `json:"isSubnetRouter"`
	Tags      []string `json:"tags"`
	User      string   `json:"user"`
	Relay     string   `json:"relay,omitempty"`
	Path      PathType `json:"path"`
	LatencyMs *float64 `json:"latencyMs,omitempty"`
}

// TopologyEdge is a hub<->peer link.
type TopologyEdge struct {
	From      DeviceID `json:"from"`
	To        DeviceID `json:"to"`
	Path      PathType `json:"path"`
	Relay     string   `json:"relay,omitempty"`
	LatencyMs *float64 `json:"latencyMs,omitempty"`
	RxRate    float64  `json:"rxRate"`
	TxRate    float64  `json:"txRate"`
}

// Topology is the response of the topology endpoint.
type Topology struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
	// DERPRegions maps region code -> region name for relays in use.
	DERPRegions map[string]string `json:"derpRegions"`
}

// PingResult is the response of an on-demand ping.
type PingResult struct {
	DeviceID  DeviceID  `json:"deviceId"`
	IP        string    `json:"ip"`
	LatencyMs float64   `json:"latencyMs"`
	Path      PathType  `json:"path"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Relay     string    `json:"relay,omitempty"`
	Err       string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

// StoreStats reports database size information.
type StoreStats struct {
	Devices      int64      `json:"devices"`
	Samples      int64      `json:"samples"`
	Rollups      int64      `json:"rollups"`
	Events       int64      `json:"events"`
	Alerts       int64      `json:"alerts"`
	SizeBytes    int64      `json:"sizeBytes"`
	OldestSample *time.Time `json:"oldestSample,omitempty"`
}

// Settings is the non-secret runtime configuration exposed to the UI.
type Settings struct {
	AuthMode            string     `json:"authMode"`
	AdminUsers          []string   `json:"adminUsers"`
	AdminTags           []string   `json:"adminTags"`
	ViewerUsers         []string   `json:"viewerUsers"` // ["*"] = any tailnet identity
	ViewerTags          []string   `json:"viewerTags"`
	AdminActions        bool       `json:"adminActionsEnabled"`
	ControlAPI          bool       `json:"controlApiEnabled"`
	Tailnet             string     `json:"tailnet,omitempty"`
	PollIntervalSec     int        `json:"pollIntervalSeconds"`
	APIIntervalSec      int        `json:"apiIntervalSeconds"`
	PingIntervalSec     int        `json:"pingIntervalSeconds"`
	AgentPort           int        `json:"agentPort"`
	AgentEnabled        bool       `json:"agentEnabled"`
	RawRetentionHours   int        `json:"rawRetentionHours"`
	RollupRetentionDays int        `json:"rollupRetentionDays"`
	EventRetentionDays  int        `json:"eventRetentionDays"`
	Notifiers           []string   `json:"notifiers"` // kinds configured, e.g. ["webhook","ntfy"]
	Listen              string     `json:"listen"`
	DemoMode            bool       `json:"demoMode"`
	Store               StoreStats `json:"store"`
}

// DeviceDetail is the device endpoint response.
type DeviceDetail struct {
	Device       Device        `json:"device"`
	RecentEvents []Event       `json:"recentEvents"`
	OpenAlerts   []Alert       `json:"openAlerts"`
	Uptime24h    *UptimeReport `json:"uptime24h,omitempty"`
}

// Snapshot is the collector's in-memory state after each poll. It is
// published on the collector's tick bus and pushed to SSE clients.
type Snapshot struct {
	Overview Overview `json:"overview"`
	Devices  []Device `json:"devices"`
}
