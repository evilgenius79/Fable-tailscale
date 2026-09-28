package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// Errors returned by Authenticator implementations. The auth middleware
// maps ErrForbidden to 403 and every other failure to 401.
var (
	// ErrUnauthenticated means the request could not be attributed to a
	// tailnet identity (WhoIs failed or the address is not on the tailnet).
	ErrUnauthenticated = errors.New("httpapi: request is not from a tailnet identity")
	// ErrForbidden means the identity is known but matches neither the
	// admin nor the viewer lists.
	ErrForbidden = errors.New("httpapi: identity has no role on this hub")
)

const (
	// identityCacheTTL is how long a resolved identity is reused for the
	// same source IP.
	identityCacheTTL = 60 * time.Second
	// identityNegativeTTL is how long a failed WhoIs is remembered so a
	// flood from an unknown address does not hammer tailscaled.
	identityNegativeTTL = 5 * time.Second
	// identityCacheMax bounds the cache size.
	identityCacheMax = 4096
	// whoisTimeout bounds one WhoIs call.
	whoisTimeout = 5 * time.Second

	authModeTailscale = "tailscale"
	authModeNone      = "none"
	taggedDeviceLogin = "tagged-device"
)

// Authenticator attributes a request's remote address to a tailnet identity
// and resolves its role.
type Authenticator interface {
	// Authenticate returns the identity behind remoteAddr ("ip:port" or a
	// bare IP). It returns an error wrapping ErrForbidden when the identity
	// is known but has no role and ErrUnauthenticated otherwise.
	Authenticate(ctx context.Context, remoteAddr string) (*model.Identity, error)
}

// NewTailscaleAuth returns an Authenticator that asks the local tailscaled
// WhoIs for every new source IP and caches the result for 60 seconds. Roles
// come from cfg: admin when the login is in cfg.Admins (case-insensitive) or
// any node tag is in cfg.AdminTags; otherwise viewer when cfg.Viewers
// contains "*", the login is in cfg.Viewers or any tag is in
// cfg.ViewerTags. Tagged nodes carry the login "tagged-device" and match by
// tags only.
func NewTailscaleAuth(local source.LocalSource, cfg *config.Config) Authenticator {
	return &tailscaleAuth{
		local:  local,
		cfg:    cfg,
		now:    time.Now,
		ttl:    identityCacheTTL,
		negTTL: identityNegativeTTL,
		cache:  make(map[netip.Addr]cacheEntry),
	}
}

// NewNoAuth returns an Authenticator that attributes every request to the
// fixed demo identity (demo@example.com, admin). It is only used in demo
// mode and with --insecure-no-auth on a loopback listener.
func NewNoAuth() Authenticator { return noAuth{} }

// cacheEntry is one cached WhoIs outcome.
type cacheEntry struct {
	id  *model.Identity
	err error
	exp time.Time
}

// tailscaleAuth is the WhoIs-backed Authenticator.
type tailscaleAuth struct {
	local  source.LocalSource
	cfg    *config.Config
	now    func() time.Time
	ttl    time.Duration
	negTTL time.Duration

	mu        sync.Mutex
	cache     map[netip.Addr]cacheEntry
	lastSweep time.Time
}

// Authenticate implements Authenticator.
func (a *tailscaleAuth) Authenticate(ctx context.Context, remoteAddr string) (*model.Identity, error) {
	ip, ok := remoteIP(remoteAddr)
	if !ok {
		return nil, fmt.Errorf("%w: unparsable remote address", ErrUnauthenticated)
	}
	now := a.now()
	a.mu.Lock()
	if e, ok := a.cache[ip]; ok && now.Before(e.exp) {
		a.mu.Unlock()
		return cloneIdentity(e.id), e.err
	}
	a.mu.Unlock()

	id, err := a.lookup(ctx, ip, remoteAddr)
	if err != nil && ctx.Err() != nil {
		return nil, err // caller went away; do not cache
	}
	ttl := a.ttl
	if errors.Is(err, ErrUnauthenticated) {
		ttl = a.negTTL
	}
	a.mu.Lock()
	a.sweepLocked(now)
	a.cache[ip] = cacheEntry{id: id, err: err, exp: now.Add(ttl)}
	a.mu.Unlock()
	return cloneIdentity(id), err
}

// lookup performs the WhoIs call and role resolution for ip.
func (a *tailscaleAuth) lookup(ctx context.Context, ip netip.Addr, remoteAddr string) (*model.Identity, error) {
	if a.local == nil {
		return nil, fmt.Errorf("%w: no tailscaled client", ErrUnauthenticated)
	}
	wctx, cancel := context.WithTimeout(ctx, whoisTimeout)
	defer cancel()
	who, err := a.local.WhoIs(wctx, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("%w: whois %s: %v", ErrUnauthenticated, ip, err)
	}
	if who == nil || (who.LoginName == "" && !who.IsTagged) {
		return nil, fmt.Errorf("%w: whois %s: empty identity", ErrUnauthenticated, ip)
	}
	id := identityFromWhoIs(who, ip)
	role, ok := resolveRole(a.cfg, id.Login, who.IsTagged, id.Tags)
	if !ok {
		return nil, fmt.Errorf("%w: %s from %s", ErrForbidden, id.Login, id.NodeName)
	}
	id.Role = role
	return id, nil
}

// sweepLocked evicts expired entries at most once per minute, or
// immediately when the cache is over capacity.
func (a *tailscaleAuth) sweepLocked(now time.Time) {
	over := len(a.cache) >= identityCacheMax
	if !over && now.Sub(a.lastSweep) < time.Minute {
		return
	}
	a.lastSweep = now
	for k, e := range a.cache {
		if !now.Before(e.exp) {
			delete(a.cache, k)
		}
	}
	if len(a.cache) >= identityCacheMax {
		clear(a.cache) // pathological: thousands of distinct live sources
	}
}

// identityFromWhoIs maps a WhoIs answer to an Identity without a role.
func identityFromWhoIs(who *source.WhoIs, ip netip.Addr) *model.Identity {
	login := strings.ToLower(strings.TrimSpace(who.LoginName))
	if who.IsTagged {
		login = taggedDeviceLogin
	}
	nodeIP := who.NodeIP
	if nodeIP == "" {
		nodeIP = ip.String()
	}
	tags := make([]string, 0, len(who.Tags))
	for _, t := range who.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tags = append(tags, t)
		}
	}
	return &model.Identity{
		Login:       login,
		DisplayName: who.DisplayName,
		ProfilePic:  who.ProfilePic,
		NodeName:    strings.TrimSuffix(who.NodeName, "."),
		NodeID:      who.NodeID,
		NodeIP:      nodeIP,
		Tags:        tags,
		AuthMode:    authModeTailscale,
	}
}

// resolveRole applies the admin/viewer lists from cfg. Tagged nodes match
// by tags only; user-owned nodes match by login (and, defensively, by any
// tags they carry). The boolean is false when no role applies.
func resolveRole(cfg *config.Config, login string, tagged bool, tags []string) (model.Role, bool) {
	if cfg == nil {
		return "", false
	}
	login = strings.ToLower(login)
	byLogin := func(list []string) bool {
		if tagged {
			return false
		}
		for _, l := range list {
			if strings.EqualFold(l, login) {
				return true
			}
		}
		return false
	}
	byTag := func(list []string) bool {
		for _, t := range tags {
			for _, want := range list {
				if strings.EqualFold(want, t) {
					return true
				}
			}
		}
		return false
	}
	if byLogin(cfg.Admins) || byTag(cfg.AdminTags) {
		return model.RoleAdmin, true
	}
	if slices.Contains(cfg.Viewers, "*") || byLogin(cfg.Viewers) || byTag(cfg.ViewerTags) {
		return model.RoleViewer, true
	}
	return "", false
}

// noAuth is the fixed-identity Authenticator.
type noAuth struct{}

// Authenticate implements Authenticator.
func (noAuth) Authenticate(_ context.Context, remoteAddr string) (*model.Identity, error) {
	ip, _ := remoteIP(remoteAddr)
	nodeIP := ""
	if ip.IsValid() {
		nodeIP = ip.String()
	}
	return &model.Identity{
		Login:       "demo@example.com",
		DisplayName: "Demo User",
		NodeName:    "alice-mbp",
		NodeID:      "demo",
		NodeIP:      nodeIP,
		Tags:        []string{},
		Role:        model.RoleAdmin,
		AuthMode:    authModeNone,
	}, nil
}

// remoteIP extracts the IP from "ip:port", "[ip]:port" or a bare IP.
func remoteIP(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i] // drop IPv6 zone
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// cloneIdentity returns an independent copy (nil-safe).
func cloneIdentity(id *model.Identity) *model.Identity {
	if id == nil {
		return nil
	}
	cp := *id
	cp.Tags = slices.Clone(id.Tags)
	if cp.Tags == nil {
		cp.Tags = []string{}
	}
	return &cp
}

// identityKey is the context key under which the auth middleware stores
// the caller's identity.
type identityKey struct{}

// IdentityFromContext returns the identity the auth middleware attached to
// the request context, if any.
func IdentityFromContext(ctx context.Context) (*model.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(*model.Identity)
	return id, ok && id != nil
}

func withIdentity(ctx context.Context, id *model.Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
