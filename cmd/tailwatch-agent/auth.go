package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// authMode selects how requests are authenticated.
type authMode string

const (
	// authWhois asks the local tailscaled who is behind the source IP.
	authWhois authMode = "whois"
	// authToken requires the shared secret in the X-Tailwatch-Token header.
	authToken authMode = "token"
	// authBoth requires WhoIs and the token.
	authBoth authMode = "both"
)

// hubTag is the tag that grants access when no allow-lists are configured.
const hubTag = "tag:tailwatch"

// whoisCacheTTL is how long a resolved identity is reused per source IP.
const whoisCacheTTL = 60 * time.Second

// whoisCacheMax bounds the identity cache.
const whoisCacheMax = 1024

// parseAuthMode validates an --auth value.
func parseAuthMode(s string) (authMode, error) {
	switch m := authMode(strings.ToLower(strings.TrimSpace(s))); m {
	case authWhois, authToken, authBoth:
		return m, nil
	default:
		return "", fmt.Errorf("--auth %q must be whois, token or both", s)
	}
}

// usesWhoIs reports whether the mode consults tailscaled.
func (m authMode) usesWhoIs() bool { return m == authWhois || m == authBoth }

// usesToken reports whether the mode checks the shared token.
func (m authMode) usesToken() bool { return m == authToken || m == authBoth }

// whoisSource resolves the tailnet identity behind a remote address.
type whoisSource interface {
	WhoIs(ctx context.Context, remoteAddr string) (*source.WhoIs, error)
}

// localIdentity is what the policy needs to know about this node, as
// reported by the local tailscaled.
type localIdentity struct {
	// Owner is the node owner's login ("" when tagged or unknown). It is
	// consulted only when no allow-lists are configured.
	Owner string
	// MagicDNSSuffix is the local tailnet's MagicDNS suffix, e.g.
	// "example.ts.net" ("" when unknown). A bare --allow-node label matches
	// only callers inside this suffix, so a node with the same base name in
	// another tailnet (WhoIs reports shared nodes by their own FQDN) is not
	// mistaken for the local one.
	MagicDNSSuffix string
}

// policy is the immutable authorization configuration.
type policy struct {
	mode       authMode
	tokenHash  [sha256.Size]byte
	allowUsers map[string]struct{}
	allowTags  map[string]struct{}
	allowNodes map[string]struct{}
}

// newPolicy builds a policy. token is required for token/both. Lists are
// normalised: users and tags lower-cased, node names lower-cased without a
// trailing dot.
func newPolicy(mode authMode, token string, users, tags, nodes []string) (*policy, error) {
	if _, err := parseAuthMode(string(mode)); err != nil {
		return nil, err
	}
	p := &policy{
		mode:       mode,
		allowUsers: toSet(users, strings.ToLower),
		allowTags:  toSet(tags, strings.ToLower),
		allowNodes: toSet(nodes, normalizeNodeName),
	}
	if mode.usesToken() {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, errors.New("auth mode " + string(mode) + " requires a token")
		}
		p.tokenHash = sha256.Sum256([]byte(token))
	}
	return p, nil
}

// hasAllowLists reports whether any explicit allow-list is configured.
func (p *policy) hasAllowLists() bool {
	return len(p.allowUsers)+len(p.allowTags)+len(p.allowNodes) > 0
}

// tokenOK compares a presented token with the configured one in constant
// time. Both sides are hashed first so the comparison does not leak the
// token length.
func (p *policy) tokenOK(presented string) bool {
	if !p.mode.usesToken() {
		return true
	}
	presented = strings.TrimSpace(presented)
	if presented == "" {
		return false
	}
	h := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(h[:], p.tokenHash[:]) == 1
}

// needsLocalIdentity reports whether identityAllowed consults the local
// node's identity: the owner login for the default policy, the MagicDNS
// suffix for bare --allow-node labels.
func (p *policy) needsLocalIdentity() bool {
	return !p.hasAllowLists() || len(p.allowNodes) > 0
}

// identityAllowed decides whether the resolved identity may read metrics.
// local describes this node (owner login, MagicDNS suffix). With no
// allow-lists, the caller must be owned by the same user or carry hubTag;
// otherwise it must match an allow-list entry. The returned reason is for
// logs only.
func (p *policy) identityAllowed(w *source.WhoIs, local localIdentity) (bool, string) {
	if w == nil {
		return false, "no identity"
	}
	login := strings.ToLower(strings.TrimSpace(w.LoginName))
	if !p.hasAllowLists() {
		owner := strings.ToLower(strings.TrimSpace(local.Owner))
		if owner != "" && login == owner && !w.IsTagged {
			return true, "owner match"
		}
		if hasTag(w.Tags, hubTag) {
			return true, hubTag
		}
		if owner == "" {
			return false, "this node has no owner (tagged) and caller lacks " + hubTag
		}
		return false, "caller is not owned by " + owner + " and lacks " + hubTag
	}
	if login != "" {
		if _, ok := p.allowUsers[login]; ok {
			return true, "allow-user"
		}
	}
	for _, t := range w.Tags {
		if _, ok := p.allowTags[strings.ToLower(t)]; ok {
			return true, "allow-tag " + t
		}
	}
	if nodeMatches(p.allowNodes, w.NodeName, local.MagicDNSSuffix) {
		return true, "allow-node"
	}
	return false, "not in allow-lists"
}

// nodeMatches reports whether the caller's MagicDNS name is in the set.
// An entry given as FQDN matches that exact FQDN (or the same base name
// when the daemon reports only a short name). An entry given as a bare
// label matches a short caller name of that label, or label+"."+suffix
// where suffix is the local tailnet's MagicDNS suffix; a caller with the
// same label under a different (or, when the suffix is unknown, any)
// domain is not matched, since WhoIs names nodes shared from other
// tailnets by their own FQDN.
func nodeMatches(set map[string]struct{}, nodeName, suffix string) bool {
	if len(set) == 0 {
		return false
	}
	fqdn := normalizeNodeName(nodeName)
	if fqdn == "" {
		return false
	}
	if _, ok := set[fqdn]; ok {
		return true
	}
	base, domain, hasDomain := strings.Cut(fqdn, ".")
	if base == "" {
		return false
	}
	if !hasDomain {
		// An allow-list entry given as FQDN should also match by base name
		// when the daemon reports only a short name.
		for entry := range set {
			if eb, _, _ := strings.Cut(entry, "."); eb == fqdn {
				return true
			}
		}
		return false
	}
	suffix = normalizeSuffix(suffix)
	if suffix == "" || domain != suffix {
		return false
	}
	_, ok := set[base]
	return ok
}

// normalizeSuffix lower-cases a MagicDNS suffix and strips surrounding
// whitespace and dots.
func normalizeSuffix(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), ".")
}

// hasTag reports whether tags contains tag (case-insensitive).
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), tag) {
			return true
		}
	}
	return false
}

// toSet normalises values into a set.
func toSet(in []string, fn func(string) string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v = fn(strings.TrimSpace(v)); v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}

// whoisCache memoises WhoIs answers per source IP for whoisCacheTTL so the
// hub's regular polls do not hit tailscaled every time.
type whoisCache struct {
	src whoisSource
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]whoisEntry
}

type whoisEntry struct {
	w       *source.WhoIs
	expires time.Time
}

// newWhoisCache returns a cache over src.
func newWhoisCache(src whoisSource, ttl time.Duration, now func() time.Time) *whoisCache {
	if ttl <= 0 {
		ttl = whoisCacheTTL
	}
	if now == nil {
		now = time.Now
	}
	return &whoisCache{src: src, ttl: ttl, now: now, entries: make(map[string]whoisEntry)}
}

// lookup returns the identity behind remoteAddr, keyed by ip. Only
// successful answers are cached.
func (c *whoisCache) lookup(ctx context.Context, ip, remoteAddr string) (*source.WhoIs, error) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.entries[ip]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.w, nil
	}
	c.mu.Unlock()

	if c.src == nil {
		return nil, errors.New("whois: no tailscaled client configured")
	}
	w, err := c.src.WhoIs(ctx, remoteAddr)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("whois: empty identity")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= whoisCacheMax {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= whoisCacheMax {
			clear(c.entries)
		}
	}
	c.entries[ip] = whoisEntry{w: w, expires: now.Add(c.ttl)}
	return w, nil
}
