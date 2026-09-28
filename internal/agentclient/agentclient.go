// Package agentclient implements source.AgentClient: it fetches
// agentproto.Report documents from tailwatch-agent instances over plain HTTP
// on the tailnet. The client is deliberately strict: bounded timeouts, a hard
// body-size cap, no redirects, no environment proxies, a required JSON
// content type and a protocol-major check, so a misbehaving or hostile peer
// cannot stall or bloat the hub's poll loop.
package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

const (
	// MaxBodyBytes is the largest agent response body the client will read.
	MaxBodyBytes = 1 << 20

	// UserAgent is sent with every request.
	UserAgent = "tailwatch"

	// DefaultTimeout is used when New is given a non-positive timeout.
	DefaultTimeout = 5 * time.Second

	// maxConnsPerHost bounds concurrent connections to one agent.
	maxConnsPerHost = 4

	// statusBodySnippet is how much of a non-200 body is kept for diagnostics.
	statusBodySnippet = 200

	// maxFutureSkew is how far ahead of the hub clock a report may claim to
	// have been sampled before it is rejected.
	maxFutureSkew = time.Hour
)

var (
	// ErrUnauthorized is wrapped when the agent answered 401 or 403: the hub
	// is not on the agent's allow-list or the shared token is wrong.
	ErrUnauthorized = errors.New("agent rejected hub (check allow-list/token)")

	// ErrProtocol is wrapped when the report's protocol major version does
	// not match agentproto.ProtocolMajor (or cannot be parsed).
	ErrProtocol = errors.New("unsupported agent protocol version")

	// ErrInvalidReport is wrapped when the response is not a usable report:
	// wrong content type, undecodable JSON or an implausible SampledAt.
	ErrInvalidReport = errors.New("invalid agent report")

	// ErrBodyTooLarge is wrapped when the response body exceeds MaxBodyBytes.
	ErrBodyTooLarge = errors.New("agent response body too large")

	// ErrRedirect is wrapped when the agent tried to redirect the request.
	// Redirects are never followed.
	ErrRedirect = errors.New("agent attempted a redirect")
)

// StatusError is returned (wrapped) when the agent answered with a non-200
// status. Body holds at most the first 200 bytes of the response, with
// control characters stripped. For 401 and 403, Unwrap returns
// ErrUnauthorized so errors.Is(err, ErrUnauthorized) holds.
type StatusError struct {
	Code int
	Body string
}

// Error implements error.
func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("agent returned HTTP %d", e.Code)
	}
	return fmt.Sprintf("agent returned HTTP %d: %s", e.Code, e.Body)
}

// Unwrap returns ErrUnauthorized for 401/403 responses and nil otherwise.
func (e *StatusError) Unwrap() error {
	if e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden {
		return ErrUnauthorized
	}
	return nil
}

// Client fetches reports from tailwatch-agent instances. It is safe for
// concurrent use; connections are pooled per agent.
type Client struct {
	hc      *http.Client
	token   string
	timeout time.Duration
	log     *slog.Logger
	now     func() time.Time
}

var _ source.AgentClient = (*Client)(nil)

// New returns a Client. token, when non-empty, is sent as the
// X-Tailwatch-Token header (it is never logged). timeout bounds the whole
// exchange (dial, response headers and body); non-positive values use
// DefaultTimeout. A nil logger falls back to slog.Default().
func New(token string, timeout time.Duration, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	transport := &http.Transport{
		// Tailnet traffic must never be routed through an environment proxy.
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxConnsPerHost:       maxConnsPerHost,
		MaxIdleConnsPerHost:   maxConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     false,
	}
	hc := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrRedirect
		},
	}
	return &Client{
		hc:      hc,
		token:   token,
		timeout: timeout,
		log:     log.With("component", "agentclient"),
		now:     time.Now,
	}
}

// CloseIdleConnections releases pooled connections to agents. Call it on
// shutdown or after a long pause in polling.
func (c *Client) CloseIdleConnections() {
	c.hc.CloseIdleConnections()
}

// Fetch retrieves and validates the report served by the agent at
// http://<ip>:<port>/v1/metrics. ip must be an IP literal (IPv6 is bracketed
// automatically); port 0 selects agentproto.DefaultPort.
//
// Errors are wrapped so callers can classify them: IsUnreachable for
// dial/timeout failures, errors.Is with ErrUnauthorized, ErrProtocol,
// ErrInvalidReport, ErrBodyTooLarge or ErrRedirect, and errors.As with
// *StatusError for any non-200 response.
func (c *Client) Fetch(ctx context.Context, ip string, port int) (*agentproto.Report, error) {
	u, err := endpointURL(ip, port)
	if err != nil {
		return nil, fmt.Errorf("agentclient: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("agentclient: fetch %s: %w", u, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if c.token != "" {
		req.Header.Set(agentproto.HeaderToken, c.token)
	}

	start := c.now()
	resp, err := c.hc.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		c.log.DebugContext(ctx, "agent fetch failed", "url", u, "err", err)
		return nil, fmt.Errorf("agentclient: fetch %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		se := &StatusError{Code: resp.StatusCode, Body: readSnippet(resp.Body, statusBodySnippet)}
		c.log.DebugContext(ctx, "agent returned error status", "url", u, "status", resp.StatusCode)
		return nil, fmt.Errorf("agentclient: fetch %s: %w", u, se)
	}
	if ct := resp.Header.Get("Content-Type"); !isJSONContentType(ct) {
		return nil, fmt.Errorf("agentclient: fetch %s: %w: unexpected content type %q", u, ErrInvalidReport, ct)
	}
	if resp.ContentLength > MaxBodyBytes {
		return nil, fmt.Errorf("agentclient: fetch %s: %w: declared %d bytes, limit %d", u, ErrBodyTooLarge, resp.ContentLength, MaxBodyBytes)
	}
	body, err := readLimited(resp.Body, MaxBodyBytes)
	if err != nil {
		if errors.Is(err, ErrBodyTooLarge) {
			return nil, fmt.Errorf("agentclient: fetch %s: %w: limit %d bytes", u, ErrBodyTooLarge, MaxBodyBytes)
		}
		return nil, fmt.Errorf("agentclient: fetch %s: read body: %w", u, err)
	}

	var rep agentproto.Report
	if err := json.Unmarshal(body, &rep); err != nil {
		return nil, fmt.Errorf("agentclient: fetch %s: %w: decode: %w", u, ErrInvalidReport, err)
	}
	if err := ValidateReport(&rep, c.now()); err != nil {
		return nil, fmt.Errorf("agentclient: fetch %s: %w", u, err)
	}
	c.log.DebugContext(ctx, "agent report fetched",
		"url", u,
		"agentVersion", rep.AgentVersion,
		"protocol", rep.ProtocolVersion,
		"bytes", len(body),
		"duration", c.now().Sub(start),
	)
	return &rep, nil
}

// ValidateReport checks the parts of a report the hub relies on: the
// protocol major must equal agentproto.ProtocolMajor and SampledAt must be
// set and not more than one hour ahead of now. It returns an error wrapping
// ErrProtocol or ErrInvalidReport.
func ValidateReport(rep *agentproto.Report, now time.Time) error {
	if rep == nil {
		return fmt.Errorf("%w: nil report", ErrInvalidReport)
	}
	major, err := ParseProtocolMajor(rep.ProtocolVersion)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	if major != agentproto.ProtocolMajor {
		return fmt.Errorf("%w: agent speaks %q, hub requires %d.x", ErrProtocol, rep.ProtocolVersion, agentproto.ProtocolMajor)
	}
	if rep.SampledAt.IsZero() {
		return fmt.Errorf("%w: sampledAt is missing", ErrInvalidReport)
	}
	if limit := now.Add(maxFutureSkew); rep.SampledAt.After(limit) {
		return fmt.Errorf("%w: sampledAt %s is more than %s in the future", ErrInvalidReport, rep.SampledAt.UTC().Format(time.RFC3339), maxFutureSkew)
	}
	return nil
}

// ParseProtocolMajor extracts the major component of a "major.minor"
// protocol version string such as agentproto.ProtocolVersion.
func ParseProtocolMajor(v string) (int, error) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return 0, errors.New("protocolVersion is empty")
	}
	majorStr, _, _ := strings.Cut(v, ".")
	major, err := strconv.Atoi(majorStr)
	if err != nil || major < 0 {
		return 0, fmt.Errorf("protocolVersion %q is not major.minor", v)
	}
	return major, nil
}

// IsUnreachable reports whether err is a connectivity-class failure (dial
// errors, connection refused/reset, host or network unreachable, timeouts,
// context deadline) as opposed to a protocol, authorization or validation
// failure. Collectors use it to back off from an agent rather than
// treating the failure as a misconfiguration.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		// The caller gave up; that says nothing about the agent.
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "dial" {
		return true
	}
	for _, target := range []error{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.EHOSTUNREACH,
		syscall.EHOSTDOWN,
		syscall.ENETUNREACH,
		syscall.ENETDOWN,
		syscall.ETIMEDOUT,
		syscall.EPIPE,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// endpointURL builds http://<host>:<port>/v1/metrics, bracketing IPv6
// literals. IPv4-mapped IPv6 addresses are unmapped.
func endpointURL(ip string, port int) (string, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return "", fmt.Errorf("invalid agent ip %q: %w", ip, err)
	}
	if port == 0 {
		port = agentproto.DefaultPort
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid agent port %d", port)
	}
	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(addr.Unmap().String(), strconv.Itoa(port)),
		Path:   agentproto.PathMetrics,
	}
	return u.String(), nil
}

// isJSONContentType accepts "application/json" and its parameterised or
// suffixed forms (e.g. "application/json; charset=utf-8").
func isJSONContentType(ct string) bool {
	mediaType, _, _ := strings.Cut(ct, ";")
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mediaType)), "application/json")
}

// readLimited reads at most limit bytes from r and returns ErrBodyTooLarge
// if more were available.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrBodyTooLarge
	}
	return b, nil
}

// readSnippet returns up to n bytes of r as printable text for diagnostics.
func readSnippet(r io.Reader, n int) string {
	b, _ := io.ReadAll(io.LimitReader(r, int64(n)))
	s := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(b), ""))
	return strings.TrimSpace(s)
}
