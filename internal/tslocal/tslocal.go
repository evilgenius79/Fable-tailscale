// Package tslocal implements source.LocalSource on top of the tailscaled
// LocalAPI (tailscale.com/client/local). It talks to the daemon running on
// the hub node over its Unix socket (or named pipe / TCP+token on macOS and
// Windows) and maps the daemon's status, WhoIs and disco-ping results onto
// the source types consumed by the collector and the HTTP API.
//
// The pure mapping functions (MapStatus, MapPeer, MapWhoIs, MapPing) are
// exported so the translation can be unit-tested without a daemon.
package tslocal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// SocketHint is the remediation appended to errors caused by a missing or
// unreadable tailscaled socket.
const SocketHint = "hint: is tailscaled running? run tailwatch as root, add its user to the tailscaled socket group, or pass --socket <path>"

var (
	// ErrDaemonUnavailable is wrapped by errors caused by tailscaled not
	// being reachable at all: the socket does not exist or nothing is
	// listening on it.
	ErrDaemonUnavailable = errors.New("tailscaled unavailable")

	// ErrPermissionDenied is wrapped by errors caused by the current process
	// lacking permission to open the tailscaled socket, or by the daemon
	// refusing the request (LocalAPI access denied).
	ErrPermissionDenied = errors.New("tailscaled permission denied")

	// ErrEmptyResponse is returned when the daemon answered but the response
	// carried no usable payload.
	ErrEmptyResponse = errors.New("empty response from tailscaled")
)

// localAPI is the subset of *local.Client used by Client. It is an interface
// so that tests can substitute a fake without a running daemon.
type localAPI interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
	Ping(ctx context.Context, ip netip.Addr, pingtype tailcfg.PingType) (*ipnstate.PingResult, error)
}

// Client implements source.LocalSource using the tailscaled LocalAPI.
// It is safe for concurrent use.
type Client struct {
	api    localAPI
	socket string
	log    *slog.Logger
}

var _ source.LocalSource = (*Client)(nil)

// New returns a Client talking to the local tailscaled. When socketPath is
// non-empty the client connects to that socket only; otherwise the platform
// default (and, on macOS, the GUI client's TCP port) is used. A nil logger
// falls back to slog.Default().
func New(socketPath string, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	lc := &local.Client{}
	if socketPath != "" {
		lc.Socket = socketPath
		lc.UseSocketOnly = true
	}
	return &Client{
		api:    lc,
		socket: socketPath,
		log:    log.With("component", "tslocal"),
	}
}

// Status fetches the daemon status (including all peers) and maps it to a
// source.LocalStatus. Peers are sorted by DNS name so successive calls are
// comparable.
func (c *Client) Status(ctx context.Context) (*source.LocalStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tslocal: status: %w", err)
	}
	st, err := c.api.Status(ctx)
	if err != nil {
		return nil, c.wrapErr("status", err)
	}
	if st == nil {
		return nil, fmt.Errorf("tslocal: status: %w", ErrEmptyResponse)
	}
	ls := MapStatus(st)
	c.log.DebugContext(ctx, "status fetched",
		"backendState", ls.BackendState,
		"peers", len(ls.Peers),
		"self", ls.Self.DNSName,
	)
	return ls, nil
}

// WhoIs resolves the tailnet identity behind remoteAddr ("ip" or "ip:port").
// When the daemon does not know the address the returned error wraps
// source.ErrNotFound.
func (c *Client) WhoIs(ctx context.Context, remoteAddr string) (*source.WhoIs, error) {
	addr := strings.TrimSpace(remoteAddr)
	if addr == "" {
		return nil, errors.New("tslocal: whois: empty remote address")
	}
	if _, err := parseHostOrHostPort(addr); err != nil {
		return nil, fmt.Errorf("tslocal: whois: invalid remote address %q: %w", addr, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tslocal: whois %s: %w", addr, err)
	}
	resp, err := c.api.WhoIs(ctx, addr)
	if err != nil {
		if errors.Is(err, local.ErrPeerNotFound) {
			return nil, fmt.Errorf("tslocal: whois %s: %w: %w", addr, source.ErrNotFound, err)
		}
		return nil, c.wrapErr("whois "+addr, err)
	}
	w, err := MapWhoIs(resp)
	if err != nil {
		return nil, fmt.Errorf("tslocal: whois %s: %w", addr, err)
	}
	return w, nil
}

// Ping sends a disco ping to the given Tailscale IP and waits up to timeout
// (when > 0) for the reply. A reply carrying an error from the daemon (for
// example "no matching peer") is returned in PingReply.Err with a nil error;
// only failures of the request itself are returned as errors.
func (c *Client) Ping(ctx context.Context, ip string, timeout time.Duration) (*source.PingReply, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return nil, fmt.Errorf("tslocal: ping: invalid ip %q: %w", ip, err)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tslocal: ping %s: %w", addr, err)
	}
	res, err := c.api.Ping(ctx, addr, tailcfg.PingDisco)
	if err != nil {
		return nil, c.wrapErr("ping "+addr.String(), err)
	}
	if res == nil {
		return nil, fmt.Errorf("tslocal: ping %s: %w", addr, ErrEmptyResponse)
	}
	reply := MapPing(res)
	c.log.DebugContext(ctx, "ping",
		"ip", addr.String(),
		"latencyMs", reply.LatencyMs,
		"endpoint", reply.Endpoint,
		"derp", reply.DERPRegionCode,
		"err", reply.Err,
	)
	return &reply, nil
}

// wrapErr classifies an error from the LocalAPI client and wraps it with a
// clear, actionable message. Socket-level failures (missing socket, nothing
// listening, permission denied) gain SocketHint exactly once and wrap
// ErrDaemonUnavailable or ErrPermissionDenied so callers can match on them.
func (c *Client) wrapErr(op string, err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("tslocal: %s: %w", op, err)
	case local.IsAccessDeniedError(err),
		errors.Is(err, fs.ErrPermission),
		errors.Is(err, syscall.EACCES),
		errors.Is(err, syscall.EPERM):
		return fmt.Errorf("tslocal: %s: %w%s: %w", op, ErrPermissionDenied, hint(err), err)
	case errors.Is(err, fs.ErrNotExist),
		errors.Is(err, syscall.ENOENT),
		errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("tslocal: %s: %w at %s%s: %w", op, ErrDaemonUnavailable, c.socketName(), hint(err), err)
	}
	return fmt.Errorf("tslocal: %s: %w", op, err)
}

// hint returns " (SocketHint)" unless the underlying error text already
// carries the hint, so the remediation appears exactly once.
func hint(err error) string {
	if err != nil && strings.Contains(err.Error(), SocketHint) {
		return ""
	}
	return " (" + SocketHint + ")"
}

// socketName describes the socket in use for error messages.
func (c *Client) socketName() string {
	if c.socket != "" {
		return "socket " + c.socket
	}
	return "the platform default socket"
}

// parseHostOrHostPort accepts "ip" or "ip:port" (IPv6 with brackets when a
// port is present) and returns the IP.
func parseHostOrHostPort(s string) (netip.Addr, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, errors.New("expected ip or ip:port")
	}
	return addr, nil
}
