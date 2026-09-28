package demo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// ErrSelfDelete is returned when an admin action tries to delete the hub's
// own node.
var ErrSelfDelete = errors.New("demo: refusing to delete the hub's own node")

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tagRe  = regexp.MustCompile(`^tag:[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Configured implements source.ControlAPI; the simulated API is always
// available.
func (s *Sim) Configured() bool { return true }

// Tailnet implements source.ControlAPI.
func (s *Sim) Tailnet() string { return APITailnet }

// Devices implements source.ControlAPI. It lists every non-deleted device,
// including unauthorized and shared-in ones.
func (s *Sim) Devices(ctx context.Context) ([]source.APIDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("demo: devices: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tick()
	out := make([]source.APIDevice, 0, len(s.devs))
	for _, d := range s.devs {
		if d.deleted {
			continue
		}
		out = append(out, s.apiDevice(d, t))
	}
	return out, nil
}

// apiDevice builds the control-API view of one device. s.mu must be held.
func (s *Sim) apiDevice(d *device, t time.Time) source.APIDevice {
	in := s.instantAt(d, t, false)
	ft := float64(t.Unix())
	nat := d.nat
	mapVaries := d.mapVaries
	ad := source.APIDevice{
		ID:                    d.legacyID,
		NodeID:                d.id,
		Name:                  d.dnsName(),
		Hostname:              d.hostname,
		User:                  d.user,
		OS:                    d.os,
		ClientVersion:         d.clientVersion,
		UpdateAvailable:       d.updateAvailable,
		Addresses:             d.addresses(),
		Created:               s.created(d),
		Expires:               s.keyExpiryPtr(d),
		KeyExpiryDisabled:     d.keyExpiryDisabled,
		Authorized:            d.authorized,
		IsExternal:            d.external,
		Tags:                  append([]string(nil), d.tags...),
		AdvertisedRoutes:      append([]string(nil), d.advertised...),
		EnabledRoutes:         append([]string(nil), d.enabledRoutes...),
		BlocksIncoming:        d.blocksIncoming,
		Endpoints:             d.endpoints(),
		DERPLatencyMs:         make(map[string]float64, len(d.derp)),
		PreferredDERP:         d.preferredDERP(),
		NATSupport:            &nat,
		MappingVariesByDestIP: &mapVaries,
	}
	for i, code := range sortedKeys(d.derp) {
		base := d.derp[code]
		ms := base * (1 + 0.12*sumOfSines(mix(d.h, chDERP, uint64(i)), slowComponents, ft))
		ad.DERPLatencyMs[code] = roundTo(ms, 0.1)
	}
	if in.online {
		// The control plane's lastSeen lags the live view by up to a minute.
		lag := time.Duration(unit(mix(d.h, chAPISeen))*45) * time.Second
		ad.LastSeen = t.Add(-lag)
	} else {
		ad.LastSeen = in.since
	}
	return ad
}

// SetAuthorized implements source.ControlAPI.
func (s *Sim) SetAuthorized(ctx context.Context, deviceID string, authorized bool) error {
	return s.mutate(ctx, "authorize", deviceID, func(d *device) error {
		d.authorized = authorized
		return nil
	}, "authorized", authorized)
}

// SetTags implements source.ControlAPI. Tags must look like "tag:name".
// Tagging a device removes its user, as Tailscale does; clearing all tags
// hands it back to its original owner.
func (s *Sim) SetTags(ctx context.Context, deviceID string, tags []string) error {
	clean := make([]string, 0, len(tags))
	for _, tg := range tags {
		tg = strings.TrimSpace(tg)
		if !tagRe.MatchString(tg) {
			return fmt.Errorf("demo: set tags: invalid tag %q (want tag:name)", tg)
		}
		clean = append(clean, tg)
	}
	return s.mutate(ctx, "set tags", deviceID, func(d *device) error {
		d.tags = clean
		if len(clean) > 0 {
			d.user = ""
		} else {
			d.user = d.profile.user
		}
		return nil
	}, "tags", strings.Join(clean, ","))
}

// SetKeyExpiryDisabled implements source.ControlAPI.
func (s *Sim) SetKeyExpiryDisabled(ctx context.Context, deviceID string, disabled bool) error {
	return s.mutate(ctx, "set key expiry", deviceID, func(d *device) error {
		d.keyExpiryDisabled = disabled
		return nil
	}, "disabled", disabled)
}

// SetRoutes implements source.ControlAPI. Routes must be valid CIDRs; the
// enabled set replaces the previous one.
func (s *Sim) SetRoutes(ctx context.Context, deviceID string, routes []string) error {
	clean, err := parseRoutes(routes)
	if err != nil {
		return fmt.Errorf("demo: set routes: %w", err)
	}
	return s.mutate(ctx, "set routes", deviceID, func(d *device) error {
		d.enabledRoutes = clean
		return nil
	}, "routes", strings.Join(clean, ","))
}

// SetName implements source.ControlAPI. The name must be a DNS label
// (lowercase letters, digits, hyphens; 1-63 chars) unique in the tailnet.
func (s *Sim) SetName(ctx context.Context, deviceID string, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if !nameRe.MatchString(name) {
		return fmt.Errorf("demo: set name: invalid machine name %q", name)
	}
	return s.mutate(ctx, "rename", deviceID, func(d *device) error {
		for _, o := range s.devs {
			if o != d && !o.deleted && o.name == name {
				return fmt.Errorf("demo: set name: %q is already used by %s", name, o.id)
			}
		}
		d.name = name
		return nil
	}, "name", name)
}

// DeleteDevice implements source.ControlAPI. Deleted devices disappear from
// Status, Devices, Ping and Fetch. Deleting the hub's own node fails with
// ErrSelfDelete.
func (s *Sim) DeleteDevice(ctx context.Context, deviceID string) error {
	return s.mutate(ctx, "delete", deviceID, func(d *device) error {
		if d == s.self {
			return ErrSelfDelete
		}
		d.deleted = true
		return nil
	})
}

// mutate applies fn to the device identified by deviceID under the lock and
// logs the outcome. Unknown devices wrap source.ErrNotFound.
func (s *Sim) mutate(ctx context.Context, action, deviceID string, fn func(*device) error, attrs ...any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("demo: %s: %w", action, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick()
	d := s.lookupID(deviceID)
	if d == nil {
		return fmt.Errorf("demo: %s: device %q: %w", action, deviceID, source.ErrNotFound)
	}
	if err := fn(d); err != nil {
		s.log.Warn("admin action rejected", append([]any{"action", action, "device", d.name, "id", d.id, "err", err}, attrs...)...)
		return err
	}
	s.log.Info("admin action applied", append([]any{"action", action, "device", d.name, "id", d.id}, attrs...)...)
	return nil
}

// modelDevice builds a merged model.Device for the device at t, used when
// Backfill seeds the device table. The collector overwrites it on its first
// poll; only FirstSeen (set by the caller) really matters. s.mu must be held.
func (s *Sim) modelDevice(d *device, t time.Time) model.Device {
	in := s.instantAt(d, t, false)
	inNetmap := d.authorized && d != s.self
	md := model.Device{
		ID:                d.id,
		Name:              d.name,
		DNSName:           d.dnsName(),
		Hostname:          d.hostname,
		OS:                d.os,
		Model:             d.model,
		Addresses:         d.addresses(),
		User:              d.user,
		UserName:          displayName(d.user),
		Tags:              append([]string{}, d.tags...),
		IsSelf:            d == s.self,
		IsExternal:        d.external,
		Online:            in.online || d == s.self,
		Active:            in.active,
		LastSeen:          t,
		Created:           s.created(d),
		UpdatedAt:         t,
		ClientVersion:     d.clientVersion,
		UpdateAvailable:   d.updateAvailable,
		Authorized:        d.authorized,
		KeyExpiry:         s.keyExpiryPtr(d),
		KeyExpiryDisabled: d.keyExpiryDisabled,
		Expired:           in.expired,
		ExitNodeOption:    d.exitNodeOpt() && d.exitEnabled(),
		IsExitNode:        inNetmap && d.exitNodeInUse && d.exitEnabled() && in.online,
		AdvertisedRoutes:  append([]string{}, d.advertised...),
		EnabledRoutes:     append([]string{}, d.enabledRoutes...),
		PrimaryRoutes:     append([]string{}, d.primaryRoutes()...),
		BlocksIncoming:    d.blocksIncoming,
		Location:          d.location,
		Connectivity: model.Connectivity{
			Path:          model.PathNone,
			Endpoints:     d.endpoints(),
			PreferredDERP: d.preferredDERP(),
		},
		Agent: model.AgentStatus{State: model.AgentUnknown},
	}
	if !in.online && d != s.self {
		md.LastSeen = in.since
	}
	if d == s.self {
		md.Connectivity.Path = model.PathUnknown
	} else if inNetmap {
		md.Connectivity.RxBytes = int64(in.counters[flowPeerRx])
		md.Connectivity.TxBytes = int64(in.counters[flowPeerTx])
		if in.online {
			if d.direct {
				md.Connectivity.Path = model.PathDirect
				md.Connectivity.CurAddr = d.curAddr()
			} else {
				md.Connectivity.Path = model.PathRelay
				md.Connectivity.Relay = d.relay
			}
			lat := in.latency
			md.Connectivity.LatencyMs = &lat
		}
	} else {
		md.Connectivity.Path = model.PathUnknown
	}
	return md
}

// sortedKeys returns the map's keys in sorted order for deterministic output.
func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// roundTo rounds v to the nearest multiple of step (0.1, 0.01, ...). It
// divides by the reciprocal so results such as 85.8 are the shortest float
// representation rather than 85.80000000000001.
func roundTo(v, step float64) float64 {
	if step <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	n := math.Round(1 / step)
	return math.Round(v*n) / n
}
