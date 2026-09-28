package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/sysmetrics"
)

const (
	// autoRetryInterval is how often "auto" re-asks tailscaled for an IP
	// while the daemon is still coming up.
	autoRetryInterval = 5 * time.Second

	// autoRetryTimeout bounds the wait for a Tailscale IPv4 address.
	autoRetryTimeout = 2 * time.Minute
)

// statusSource is the subset of source.LocalSource the agent uses to learn
// its own Tailscale IPs, owner and daemon state.
type statusSource interface {
	Status(ctx context.Context) (*source.LocalStatus, error)
}

// listenResolver turns the --listen flag into a concrete bind address.
type listenResolver struct {
	status  statusSource
	log     *slog.Logger
	retry   time.Duration
	timeout time.Duration
	// sleep waits for d or until ctx is done; it reports whether the full
	// duration elapsed. Tests inject an instant version.
	sleep func(ctx context.Context, d time.Duration) bool
}

// newListenResolver returns a resolver with production retry timings.
func newListenResolver(st statusSource, log *slog.Logger) *listenResolver {
	if log == nil {
		log = slog.Default()
	}
	return &listenResolver{
		status:  st,
		log:     log,
		retry:   autoRetryInterval,
		timeout: autoRetryTimeout,
		sleep:   sleepCtx,
	}
}

// resolve returns the "host:port" to bind. "auto" polls tailscaled for a
// Tailscale address (IPv4 preferred, IPv6 fallback) while the daemon comes
// up; any other value must be a Tailscale, loopback or, with insecure,
// arbitrary IP literal.
func (r *listenResolver) resolve(ctx context.Context, listen string, port int, insecure bool) (string, error) {
	if listen == listenAuto {
		ip, err := r.autoIP(ctx, port)
		if err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
	}
	host, p, err := splitListen(listen, port)
	if err != nil {
		return "", fmt.Errorf("--listen %q: %w", listen, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "", fmt.Errorf("--listen %q: %w", listen, err)
	}
	kind := classifyListenIP(ip)
	switch kind {
	case listenTailscale, listenLoopback:
	default:
		if !insecure {
			return "", fmt.Errorf("refusing to listen on non-Tailscale address %s: use --listen auto, the device's Tailscale IP (100.x.y.z), a loopback address, or --insecure-listen-any", host)
		}
		r.log.Warn("listening on a non-Tailscale address because --insecure-listen-any is set; the agent may be reachable from outside the tailnet", "addr", host, "kind", kind)
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(p)), nil
}

// autoIP asks tailscaled for a Tailscale address, preferring IPv4 and
// falling back to IPv6, retrying every r.retry for up to r.timeout.
func (r *listenResolver) autoIP(ctx context.Context, port int) (netip.Addr, error) {
	if r.status == nil {
		return netip.Addr{}, errors.New("listen auto: no tailscaled client configured")
	}
	deadline := time.Now().Add(r.timeout)
	var lastErr error
	attempt := 0
	for {
		attempt++
		st, err := r.status.Status(ctx)
		switch {
		case err != nil:
			lastErr = err
		case st == nil:
			lastErr = errors.New("empty status")
		default:
			if ip, ok := firstTailscaleIP(st.TailscaleIPs); ok {
				return ip, nil
			}
			lastErr = fmt.Errorf("tailscaled reports no Tailscale IP yet (state %q)", st.BackendState)
		}
		if ctx.Err() != nil {
			return netip.Addr{}, fmt.Errorf("listen auto: %w (last error: %v)", ctx.Err(), lastErr)
		}
		if time.Now().Add(r.retry).After(deadline) {
			return netip.Addr{}, fmt.Errorf("listen auto: could not determine a Tailscale IP address after %s: %w; is tailscaled running and logged in? (or pass --listen <tailscale-ip>:%d)", r.timeout, lastErr, port)
		}
		if attempt == 1 {
			r.log.Info("waiting for tailscaled to report a Tailscale IP address", "retryEvery", r.retry, "timeout", r.timeout, "err", lastErr)
		} else {
			r.log.Debug("still waiting for tailscaled", "attempt", attempt, "err", lastErr)
		}
		if !r.sleep(ctx, r.retry) {
			return netip.Addr{}, fmt.Errorf("listen auto: %w (last error: %v)", ctx.Err(), lastErr)
		}
	}
}

// listenKind classifies a bind address.
type listenKind string

const (
	listenTailscale listenKind = "tailscale"
	listenLoopback  listenKind = "loopback"
	listenAny       listenKind = "unspecified"
	listenOther     listenKind = "other"
)

// classifyListenIP reports whether ip is a Tailscale address (CGNAT
// 100.64.0.0/10 or fd7a:115c:a1e0::/48), loopback, unspecified (0.0.0.0 /
// ::) or something else.
func classifyListenIP(ip netip.Addr) listenKind {
	ip = ip.Unmap()
	switch {
	case sysmetrics.IsTailscaleAddr(ip):
		return listenTailscale
	case ip.IsLoopback():
		return listenLoopback
	case ip.IsUnspecified():
		return listenAny
	default:
		return listenOther
	}
}

// firstIPv4 returns the first IPv4 address in the list.
func firstIPv4(addrs []string) (netip.Addr, bool) {
	for _, s := range addrs {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		a = a.Unmap()
		if a.Is4() {
			return a, true
		}
	}
	return netip.Addr{}, false
}

// firstTailscaleIP prefers the first IPv4 address and falls back to the
// first IPv6 address reported by tailscaled.
func firstTailscaleIP(addrs []string) (netip.Addr, bool) {
	if ip, ok := firstIPv4(addrs); ok {
		return ip, true
	}
	for _, s := range addrs {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		a = a.Unmap()
		if a.Is6() && !a.IsLoopback() && !a.IsUnspecified() {
			return a, true
		}
	}
	return netip.Addr{}, false
}

// sleepCtx waits for d or until ctx is done; it reports whether the full
// duration elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
