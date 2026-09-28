package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/evilgenius79/fable-tailscale/internal/config"
)

// hostEntry is one accepted Host value: a canonical name and an optional
// port ("" matches any port).
type hostEntry struct {
	name string
	port string
}

// parseHostEntry canonicalizes a "host" or "host:port" value. ok is false
// when the value is not a valid host.
func parseHostEntry(s string) (hostEntry, bool) {
	host, port, err := config.SplitHostOptionalPort(s)
	if err != nil {
		return hostEntry{}, false
	}
	return hostEntry{name: canonicalHostName(host), port: port}, true
}

// canonicalHostName lower-cases a host name, strips a trailing dot and
// renders IP literals in their canonical form (IPv6 in brackets, IPv4-mapped
// addresses unmapped) so that equal hosts compare equal.
func canonicalHostName(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")); err == nil {
		ip = ip.Unmap()
		if ip.Is6() {
			return "[" + ip.String() + "]"
		}
		return ip.String()
	}
	return strings.TrimSuffix(host, ".")
}

// isUnspecifiedName reports whether a canonical host name is the
// unspecified address (0.0.0.0 or ::), which names nothing.
func isUnspecifiedName(name string) bool {
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(name, "["), "]"))
	return err == nil && ip.Unmap().IsUnspecified()
}

// isLoopbackName reports whether a canonical host name is "localhost" or a
// loopback IP literal.
func isLoopbackName(name string) bool {
	if name == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(name, "["), "]"))
	return err == nil && ip.Unmap().IsLoopback()
}

// hostPolicy decides which Host header values name this hub. It is the
// DNS-rebinding defence: a page on an attacker's domain whose DNS is
// re-pointed at the hub sends the attacker's domain as Host (and Origin),
// which none of the browser-hardening checks would catch on their own.
type hostPolicy struct {
	// static entries come from the configuration: the explicit --listen
	// address (host:port and bare host) and --allowed-hosts.
	static []hostEntry
	// bound is the address the listener ended up on (set by Serve; nil
	// until then).
	bound atomic.Pointer[hostEntry]
	// loopback accepts localhost and loopback IP literals on any port.
	loopback bool
}

// newHostPolicy derives the static policy from the configuration.
func newHostPolicy(cfg *config.Config) *hostPolicy {
	p := &hostPolicy{loopback: cfg.Demo || cfg.InsecureNoAuth || config.IsLoopbackListen(cfg.Listen)}
	if cfg.Listen != config.ListenAuto {
		// The listen port is authoritative, the listen host is accepted on
		// any port so that a proxy on another port still works. An
		// unspecified bind address (0.0.0.0 / ::) names nothing.
		if e, ok := parseHostEntry(strings.TrimSpace(cfg.Listen)); ok && !isUnspecifiedName(e.name) {
			p.static = append(p.static, e, hostEntry{name: e.name})
		}
	}
	for _, h := range cfg.AllowedHosts {
		if e, ok := parseHostEntry(h); ok {
			p.static = append(p.static, e)
		}
	}
	return p
}

// setBound records the listener address once it is known.
func (p *hostPolicy) setBound(addr string) {
	e, ok := parseHostEntry(addr)
	if !ok || isUnspecifiedName(e.name) {
		return
	}
	if isLoopbackName(e.name) {
		p.loopback = true
	}
	p.bound.Store(&e)
}

// allows reports whether r.Host names this hub. hubNames are the hub's
// current Tailscale IPs and MagicDNS names, accepted on any port.
func (p *hostPolicy) allows(r *http.Request, hubNames []string) bool {
	host, port, err := config.SplitHostOptionalPort(r.Host)
	if err != nil {
		return false
	}
	name := canonicalHostName(host)
	if name == "" {
		return false
	}
	if port == "" {
		port = "80"
		if r.TLS != nil {
			port = "443"
		}
	}
	match := func(e hostEntry) bool {
		return e.name == name && (e.port == "" || e.port == port)
	}
	for _, e := range p.static {
		if match(e) {
			return true
		}
	}
	if b := p.bound.Load(); b != nil && (match(*b) || match(hostEntry{name: b.name})) {
		return true
	}
	for _, n := range hubNames {
		if canonicalHostName(n) == name {
			return true
		}
	}
	return p.loopback && isLoopbackName(name)
}

// hubNames returns the Host values that name the hub according to
// tailscaled: its Tailscale IPs, its MagicDNS FQDN and short name.
func (s *Server) hubNames() []string {
	hub := s.d.Collector.Hub()
	names := hub.SelfIPs
	if hub.SelfName != "" {
		names = append(names, hub.SelfName)
		if hub.MagicDNSSuffix != "" {
			names = append(names, hub.SelfName+"."+hub.MagicDNSSuffix)
		}
	}
	return names
}

// requireKnownHost rejects API requests whose Host header does not name the
// hub. It runs before authentication so a rebound request never reaches
// WhoIs or the identity cache.
func (s *Server) requireKnownHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts.allows(r, s.hubNames()) {
			s.log.Info("httpapi: rejected request for an unknown host", "host", truncateText(r.Host, 120), "remote", r.RemoteAddr, "path", r.URL.Path)
			writeError(w, http.StatusForbidden, codeForbidden, "the request Host header does not name this hub (add it with --allowed-hosts if it should)")
			return
		}
		next.ServeHTTP(w, r)
	})
}
