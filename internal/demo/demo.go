// Package demo implements a deterministic simulated tailnet so the Tailwatch
// hub can run with --demo, and the UI can be developed and screenshotted,
// without a real tailnet, control-API key or agents.
//
// A single *Sim implements all three source interfaces (source.LocalSource,
// source.ControlAPI and source.AgentClient). Every value it reports is a pure
// function of (seed, time) plus a small amount of per-device administrative
// state (authorized, tags, name, routes, key-expiry, deleted) that the admin
// actions mutate. Two Sims built from the same seed and queried at the same
// instant therefore return identical data, consecutive polls are smooth, and
// cumulative counters (hub<->peer bytes, interface counters) never decrease.
//
// Metrics follow a diurnal sinusoid plus band-limited noise (a sum of sines
// with per-device phases, never a random walk) plus occasional spikes.
// Bandwidth counters are the integral of an analytic rate function; see
// flows.go. Backfill writes the same functions into the store so history is
// continuous with live data.
package demo

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// Compile-time interface assertions.
var (
	_ source.LocalSource = (*Sim)(nil)
	_ source.ControlAPI  = (*Sim)(nil)
	_ source.AgentClient = (*Sim)(nil)
)

const (
	// MagicDNSSuffix is the simulated tailnet's MagicDNS suffix.
	MagicDNSSuffix = "tail1234.ts.net"
	// TailnetName is the tailnet name reported by the simulated LocalAPI.
	TailnetName = "demo@example.com"
	// APITailnet is the tailnet name reported by the simulated control API.
	APITailnet = "example.com"
	// ClientVersion is the current Tailscale client version in the fleet.
	ClientVersion = "1.92.1"
	// OldClientVersion is the outdated version run by "old-laptop".
	OldClientVersion = "1.60.0"
	// AgentVersion is the tailwatch-agent version reported by simulated agents.
	AgentVersion = "1.0.0"

	// WhoIsLogin and WhoIsDisplay are the fixed identity returned by WhoIs.
	WhoIsLogin   = "demo@example.com"
	WhoIsDisplay = "Demo User"
	// WhoIsNode is the fleet device WhoIs attributes requests to.
	WhoIsNode = "alice-mbp"

	// SelfName is the MagicDNS base name of the hub node.
	SelfName = "tailwatch-hub"
)

// Sim is a simulated tailnet. It is safe for concurrent use.
type Sim struct {
	log  *slog.Logger
	seed int64
	h    uint64
	now  func() time.Time

	mu   sync.Mutex
	t0   time.Time // simulation base time, fixed on first use
	devs []*device
	byID map[model.DeviceID]*device
	byIP map[string]*device
	self *device
}

// New builds a simulated tailnet from seed. A nil log uses slog.Default().
func New(seed int64, log *slog.Logger) *Sim {
	if log == nil {
		log = slog.Default()
	}
	s := &Sim{
		log:  log.With("component", "demo"),
		seed: seed,
		h:    splitmix(uint64(seed) ^ 0xD1B54A32D192ED03),
		now:  time.Now,
		byID: make(map[model.DeviceID]*device),
		byIP: make(map[string]*device),
	}
	s.devs = buildFleet(s.h)
	for _, d := range s.devs {
		s.byID[d.id] = d
		s.byIP[d.ip4] = d
		s.byIP[d.ip6] = d
		if d.name == SelfName {
			s.self = d
		}
	}
	if s.self == nil {
		// buildFleet always includes the hub; guard against future edits.
		s.self = s.devs[0]
	}
	s.log.Debug("simulated tailnet ready", "seed", seed, "devices", len(s.devs))
	return s
}

// DeviceIPs returns the Tailscale IPv4 address of every device that has not
// been deleted, mapped to its stable node ID. It is intended for tests and
// tooling that need to address simulated devices without parsing Status.
func (s *Sim) DeviceIPs() map[string]model.DeviceID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]model.DeviceID, len(s.devs))
	for _, d := range s.devs {
		if d.deleted {
			continue
		}
		out[d.ip4] = d.id
	}
	return out
}

// tick returns the current simulated time (UTC) and fixes the simulation
// base time on first use. The base time is the start of the UTC day of
// first use, so device creation dates, key expiries and counter origins are
// identical for every Sim first used on the same day. s.mu must be held.
func (s *Sim) tick() time.Time {
	t := s.now().UTC()
	if s.t0.IsZero() {
		s.t0 = unix(t.Unix() - floorMod(t.Unix(), dayPeriod))
	}
	return t
}

// lookupIP returns the live device with the given Tailscale IP (v4 or v6),
// or nil. s.mu must be held.
func (s *Sim) lookupIP(ip string) *device {
	d := s.byIP[ip]
	if d == nil || d.deleted {
		return nil
	}
	return d
}

// lookupID returns the live device with the given stable node ID or legacy
// numeric ID, or nil. s.mu must be held.
func (s *Sim) lookupID(id string) *device {
	id = strings.TrimSpace(id)
	if d := s.byID[model.DeviceID(id)]; d != nil && !d.deleted {
		return d
	}
	for _, d := range s.devs {
		if !d.deleted && d.legacyID == id {
			return d
		}
	}
	return nil
}

// created returns the time the device joined the tailnet. s.mu must be held
// (it depends on the base time).
func (s *Sim) created(d *device) time.Time {
	return s.t0.Add(-time.Duration(d.createdDaysAgo * float64(day))).Truncate(time.Second)
}

// keyExpiry returns the node key expiry and whether expiry is disabled.
// Keys expire at 14:00 UTC, keyExpiryDays days after the base day. s.mu
// must be held.
func (s *Sim) keyExpiry(d *device) (time.Time, bool) {
	if d.keyExpiryDisabled {
		return time.Time{}, true
	}
	return s.t0.Add(time.Duration(d.keyExpiryDays*float64(day)) + 14*time.Hour).Truncate(time.Second), false
}

// keyExpiryPtr is keyExpiry as a pointer (nil when disabled).
func (s *Sim) keyExpiryPtr(d *device) *time.Time {
	exp, disabled := s.keyExpiry(d)
	if disabled {
		return nil
	}
	return &exp
}

// isExpired reports whether the node key has expired at t. s.mu must be held.
func (s *Sim) isExpired(d *device, t time.Time) bool {
	exp, disabled := s.keyExpiry(d)
	return !disabled && !t.Before(exp)
}
