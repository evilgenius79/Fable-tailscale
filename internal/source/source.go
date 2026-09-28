// Package source defines the interfaces through which the collector talks to
// the outside world. Real implementations live in internal/tslocal (local
// tailscaled), internal/tsapi (Tailscale control API) and internal/agentclient
// (tailwatch-agent). internal/demo provides simulated implementations of all
// three so the hub can run without a tailnet.
package source

import (
	"context"
	"errors"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// ErrNotConfigured is returned by ControlAPI implementations that have no
// credentials, and by admin actions when they are disabled.
var ErrNotConfigured = errors.New("not configured")

// ErrNotFound is returned when a device or resource does not exist upstream.
var ErrNotFound = errors.New("not found")

// LocalPeer is the hub's local view of one peer (from tailscaled).
type LocalPeer struct {
	ID            model.DeviceID
	PublicKey     string
	HostName      string
	DNSName       string // FQDN without trailing dot
	OS            string
	UserLogin     string // owner login name ("" if tagged/unknown)
	UserDisplay   string
	Tags          []string
	TailscaleIPs  []string
	Addrs         []string
	CurAddr       string
	Relay         string
	RxBytes       int64
	TxBytes       int64
	Created       time.Time
	LastWrite     time.Time
	LastSeen      time.Time
	LastHandshake time.Time
	Online        bool
	Active        bool
	ExitNode      bool // currently used as exit node by the hub
	ExitNodeOpt   bool
	PrimaryRoutes []string
	AllowedIPs    []string
	Expired       bool
	KeyExpiry     *time.Time
	ShareeNode    bool
	Location      *model.Location
}

// LocalStatus is the hub's local tailscaled status.
type LocalStatus struct {
	Version        string
	BackendState   string
	TailscaleIPs   []string
	Self           LocalPeer
	Health         []string
	TailnetName    string
	MagicDNSSuffix string
	Peers          []LocalPeer
	// ClientVersion, when known, reports whether the hub's own client is current.
	RunningLatest *bool
	LatestVersion string
}

// WhoIs is the identity behind a remote address on the tailnet.
type WhoIs struct {
	NodeID      model.DeviceID
	NodeName    string // MagicDNS FQDN without trailing dot
	NodeIP      string
	Tags        []string
	IsTagged    bool
	LoginName   string
	DisplayName string
	ProfilePic  string
}

// PingReply is the result of a disco ping.
type PingReply struct {
	LatencyMs      float64
	Endpoint       string // direct endpoint when direct
	DERPRegionID   int
	DERPRegionCode string
	NodeName       string
	Err            string
}

// LocalSource abstracts the local tailscaled (LocalAPI).
type LocalSource interface {
	Status(ctx context.Context) (*LocalStatus, error)
	WhoIs(ctx context.Context, remoteAddr string) (*WhoIs, error)
	// Ping sends a disco ping to the given Tailscale IP.
	Ping(ctx context.Context, ip string, timeout time.Duration) (*PingReply, error)
}

// APIDevice is a device as returned by the Tailscale control API.
type APIDevice struct {
	ID                    string // legacy numeric id (used in /device/{id} admin calls)
	NodeID                model.DeviceID
	Name                  string // FQDN
	Hostname              string
	User                  string
	OS                    string
	ClientVersion         string
	UpdateAvailable       bool
	Addresses             []string
	Created               time.Time
	LastSeen              time.Time
	Expires               *time.Time
	KeyExpiryDisabled     bool
	Authorized            bool
	IsExternal            bool
	Tags                  []string
	AdvertisedRoutes      []string
	EnabledRoutes         []string
	BlocksIncoming        bool
	Endpoints             []string
	DERPLatencyMs         map[string]float64 // region code -> ms
	PreferredDERP         string
	NATSupport            *model.NATSupport
	MappingVariesByDestIP *bool
	TailnetLockError      string
}

// ControlAPI abstracts the Tailscale control API (api.tailscale.com).
// Implementations return ErrNotConfigured when no credentials are set.
type ControlAPI interface {
	Configured() bool
	Tailnet() string
	Devices(ctx context.Context) ([]APIDevice, error)
	// Admin actions. deviceID is the APIDevice.ID (legacy id) or NodeID; the
	// control API accepts both.
	SetAuthorized(ctx context.Context, deviceID string, authorized bool) error
	SetTags(ctx context.Context, deviceID string, tags []string) error
	SetKeyExpiryDisabled(ctx context.Context, deviceID string, disabled bool) error
	SetRoutes(ctx context.Context, deviceID string, routes []string) error
	DeleteDevice(ctx context.Context, deviceID string) error
	// SetName sets the machine name (base name, without domain).
	SetName(ctx context.Context, deviceID string, name string) error
}

// AgentClient fetches reports from tailwatch-agent instances.
type AgentClient interface {
	// Fetch retrieves a report from the agent at the given Tailscale IP.
	Fetch(ctx context.Context, ip string, port int) (*agentproto.Report, error)
}
