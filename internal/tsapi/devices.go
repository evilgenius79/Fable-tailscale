package tsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// Devices lists every device of the tailnet
// (GET /api/v2/tailnet/{tailnet}/devices?fields=all).
func (c *Client) Devices(ctx context.Context) ([]source.APIDevice, error) {
	const op = "devices"
	if err := c.ready(op); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, op, request{
		method: http.MethodGet,
		path:   "/api/v2/tailnet/" + url.PathEscape(c.tailnet) + "/devices",
		query:  url.Values{"fields": {"all"}},
	})
	if err != nil {
		return nil, fmt.Errorf("tsapi: %s: %w", op, err)
	}
	if err := checkStatus(resp); err != nil {
		return nil, fmt.Errorf("tsapi: %s: %w", op, err)
	}
	var payload struct {
		Devices []apiDevice `json:"devices"`
	}
	if err := json.Unmarshal(resp.body, &payload); err != nil {
		return nil, fmt.Errorf("tsapi: %s: decode response: %w", op, err)
	}
	out := make([]source.APIDevice, 0, len(payload.Devices))
	for i := range payload.Devices {
		out = append(out, payload.Devices[i].convert(c.log))
	}
	c.log.Debug("fetched devices from control api", "count", len(out))
	return out, nil
}

// apiDevice is the wire representation of a device. Timestamps are kept as
// strings so one malformed value cannot fail the whole listing.
type apiDevice struct {
	Addresses                 []string         `json:"addresses"`
	ID                        string           `json:"id"`
	NodeID                    string           `json:"nodeId"`
	User                      string           `json:"user"`
	Name                      string           `json:"name"`
	Hostname                  string           `json:"hostname"`
	ClientVersion             string           `json:"clientVersion"`
	UpdateAvailable           bool             `json:"updateAvailable"`
	OS                        string           `json:"os"`
	Created                   string           `json:"created"`
	LastSeen                  string           `json:"lastSeen"`
	KeyExpiryDisabled         bool             `json:"keyExpiryDisabled"`
	Expires                   string           `json:"expires"`
	Authorized                bool             `json:"authorized"`
	IsExternal                bool             `json:"isExternal"`
	Tags                      []string         `json:"tags"`
	AdvertisedRoutes          []string         `json:"advertisedRoutes"`
	EnabledRoutes             []string         `json:"enabledRoutes"`
	BlocksIncomingConnections bool             `json:"blocksIncomingConnections"`
	TailnetLockError          string           `json:"tailnetLockError"`
	ClientConnectivity        *apiConnectivity `json:"clientConnectivity"`
}

type apiConnectivity struct {
	Endpoints             []string              `json:"endpoints"`
	MappingVariesByDestIP *bool                 `json:"mappingVariesByDestIP"`
	Latency               map[string]apiLatency `json:"latency"`
	ClientSupports        *apiClientSupports    `json:"clientSupports"`
}

type apiLatency struct {
	Preferred bool    `json:"preferred"`
	LatencyMs float64 `json:"latencyMs"`
}

type apiClientSupports struct {
	HairPinning bool `json:"hairPinning"`
	IPv6        bool `json:"ipv6"`
	PCP         bool `json:"pcp"`
	PMP         bool `json:"pmp"`
	UDP         bool `json:"udp"`
	UPnP        bool `json:"upnp"`
}

// convert maps the wire device to the frozen source.APIDevice.
func (d *apiDevice) convert(log *slog.Logger) source.APIDevice {
	out := source.APIDevice{
		ID:                d.ID,
		NodeID:            model.DeviceID(d.NodeID),
		Name:              strings.TrimSuffix(d.Name, "."),
		Hostname:          d.Hostname,
		User:              d.User,
		OS:                d.OS,
		ClientVersion:     d.ClientVersion,
		UpdateAvailable:   d.UpdateAvailable,
		Addresses:         d.Addresses,
		Created:           parseTime(log, d.ID, "created", d.Created),
		LastSeen:          parseTime(log, d.ID, "lastSeen", d.LastSeen),
		KeyExpiryDisabled: d.KeyExpiryDisabled,
		Authorized:        d.Authorized,
		IsExternal:        d.IsExternal,
		Tags:              d.Tags,
		AdvertisedRoutes:  d.AdvertisedRoutes,
		EnabledRoutes:     d.EnabledRoutes,
		BlocksIncoming:    d.BlocksIncomingConnections,
		TailnetLockError:  d.TailnetLockError,
	}
	if exp := parseTime(log, d.ID, "expires", d.Expires); !exp.IsZero() {
		out.Expires = &exp
	}
	if cc := d.ClientConnectivity; cc != nil {
		out.Endpoints = cc.Endpoints
		out.MappingVariesByDestIP = cc.MappingVariesByDestIP
		if cs := cc.ClientSupports; cs != nil {
			out.NATSupport = &model.NATSupport{
				HairPinning: cs.HairPinning,
				IPv6:        cs.IPv6,
				PCP:         cs.PCP,
				PMP:         cs.PMP,
				UDP:         cs.UDP,
				UPnP:        cs.UPnP,
			}
		}
		out.DERPLatencyMs, out.PreferredDERP = convertLatency(cc.Latency)
	}
	return out
}

// convertLatency maps region display names to codes. Regions are visited in
// sorted name order so the preferred region is deterministic should the API
// ever flag more than one.
func convertLatency(latency map[string]apiLatency) (map[string]float64, string) {
	if len(latency) == 0 {
		return nil, ""
	}
	names := make([]string, 0, len(latency))
	for name := range latency {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]float64, len(names))
	preferred := ""
	for _, name := range names {
		code := DERPCode(name)
		if code == "" {
			continue
		}
		l := latency[name]
		out[code] = l.LatencyMs
		if l.Preferred && preferred == "" {
			preferred = code
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, preferred
}

// parseTime parses an RFC 3339 timestamp. The empty string and the zero time
// ("0001-01-01T00:00:00Z") both yield the zero time; a malformed value is
// logged at debug level and also yields the zero time.
func parseTime(log *slog.Logger, deviceID, field, value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		if log != nil {
			log.Debug("ignoring malformed timestamp from control api", "device", deviceID, "field", field)
		}
		return time.Time{}
	}
	return t
}
