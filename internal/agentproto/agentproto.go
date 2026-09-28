// Package agentproto defines the wire format between tailwatch-agent and the
// hub. The agent serves GET /v1/metrics returning a Report as JSON, and
// GET /v1/health returning {"ok":true,"version":"..."}.
//
// Protocol version is carried in Report.ProtocolVersion; the hub accepts any
// report whose major version equals ProtocolMajor.
package agentproto

import "time"

// DefaultPort is the TCP port the agent listens on (bound to the device's
// Tailscale IP only).
const DefaultPort = 41820

// ProtocolMajor is the current major protocol version.
const ProtocolMajor = 1

// ProtocolVersion is the full protocol version string.
const ProtocolVersion = "1.0"

// Paths served by the agent.
const (
	PathMetrics = "/v1/metrics"
	PathHealth  = "/v1/health"
)

// HeaderToken is the optional shared-secret header (used when the agent runs
// with --auth token or --auth both).
const HeaderToken = "X-Tailwatch-Token"

// Report is a full metrics snapshot from one device.
type Report struct {
	ProtocolVersion string    `json:"protocolVersion"`
	AgentVersion    string    `json:"agentVersion"`
	SampledAt       time.Time `json:"sampledAt"`

	Host   Host   `json:"host"`
	CPU    CPU    `json:"cpu"`
	Memory Memory `json:"memory"`
	Disks  []Disk `json:"disks"`
	Net    Net    `json:"net"`

	Temperatures []Temperature `json:"temperatures,omitempty"`
	Processes    int           `json:"processes"`

	// Tailscale is populated when the agent can talk to the local tailscaled.
	Tailscale *TailscaleInfo `json:"tailscale,omitempty"`
}

// Host describes the machine.
type Host struct {
	Hostname        string    `json:"hostname"`
	OS              string    `json:"os"`              // runtime.GOOS: "linux","darwin","windows","freebsd"
	Platform        string    `json:"platform"`        // "ubuntu","darwin","Microsoft Windows 11 Pro"
	PlatformVersion string    `json:"platformVersion"` // "24.04"
	Kernel          string    `json:"kernel"`          // kernel version
	Arch            string    `json:"arch"`            // runtime.GOARCH
	BootTime        time.Time `json:"bootTime"`
	UptimeSeconds   uint64    `json:"uptimeSeconds"`
}

// CPU usage. Percent is the utilisation over the agent's internal sampling
// interval (not since boot), 0..100 across all cores.
type CPU struct {
	Percent float64   `json:"percent"`
	PerCore []float64 `json:"perCore,omitempty"`
	Count   int       `json:"count"`
	Model   string    `json:"model,omitempty"`
	Load1   float64   `json:"load1"`
	Load5   float64   `json:"load5"`
	Load15  float64   `json:"load15"`
}

// Memory usage in bytes.
type Memory struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Percent   float64 `json:"percent"`
	SwapTotal uint64  `json:"swapTotal"`
	SwapUsed  uint64  `json:"swapUsed"`
}

// Disk is one mounted filesystem. Primary marks the root/system volume used
// for the headline "disk %" figure.
type Disk struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device,omitempty"`
	FSType  string  `json:"fstype,omitempty"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
	Primary bool    `json:"primary"`
}

// Net holds per-interface cumulative counters. The agent never computes
// rates; the hub derives them from consecutive reports.
type Net struct {
	Interfaces  []Interface `json:"interfaces"`
	TailscaleIf string      `json:"tailscaleInterface,omitempty"` // name of the tailscale interface if detected
}

// Interface counters (cumulative since boot).
type Interface struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rxBytes"`
	TxBytes   uint64 `json:"txBytes"`
	RxPackets uint64 `json:"rxPackets"`
	TxPackets uint64 `json:"txPackets"`
	RxErrors  uint64 `json:"rxErrors"`
	TxErrors  uint64 `json:"txErrors"`
	Physical  bool   `json:"physical"` // false for loopback, tailscale, docker, veth, bridges
}

// Temperature is one sensor.
type Temperature struct {
	Sensor   string  `json:"sensor"`
	Celsius  float64 `json:"celsius"`
	Critical float64 `json:"critical,omitempty"`
}

// TailscaleInfo is what the agent learns from its local tailscaled.
type TailscaleInfo struct {
	Version      string   `json:"version"`
	BackendState string   `json:"backendState"`
	IPs          []string `json:"ips"`
	Health       []string `json:"health,omitempty"`
}

// HealthResponse is returned by /v1/health.
type HealthResponse struct {
	OK              bool   `json:"ok"`
	AgentVersion    string `json:"agentVersion"`
	ProtocolVersion string `json:"protocolVersion"`
}
