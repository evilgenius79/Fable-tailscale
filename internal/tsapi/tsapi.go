// Package tsapi implements source.ControlAPI against the Tailscale control
// API (https://api.tailscale.com, v2) using only net/http and encoding/json.
//
// Authentication is either a static API key (sent as "Authorization: Bearer")
// or an OAuth client using the client_credentials grant against
// /api/v2/oauth/token. OAuth access tokens are cached in memory and refreshed
// shortly before they expire; concurrent callers share a single refresh.
//
// Requests that fail with 429 or a 5xx status are retried once after a delay
// derived from Retry-After (bounded to [1s, 10s]); context cancellation is
// honoured while waiting. Credentials and access tokens are never logged and
// are scrubbed from every error string this package returns.
package tsapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/source"
)

const (
	// DefaultBaseURL is the public Tailscale control API endpoint.
	DefaultBaseURL = "https://api.tailscale.com"
	// DefaultTailnet selects the default tailnet of the credentials in use.
	DefaultTailnet = "-"
	// DefaultTimeout is the timeout of the http.Client created when
	// Options.HTTPClient is nil.
	DefaultTimeout = 30 * time.Second

	defaultVersion     = "dev"
	minRetryDelay      = 1 * time.Second
	maxRetryDelay      = 10 * time.Second
	defaultMaxBody     = 32 << 20 // 32 MiB; a large tailnet with fields=all is a few MiB
	maxErrorMessageLen = 512
)

// Options configures a Client. The zero value is an unconfigured client whose
// methods return source.ErrNotConfigured.
type Options struct {
	// BaseURL of the control API. Defaults to DefaultBaseURL.
	BaseURL string
	// Tailnet is the tailnet name (e.g. "example.com"). Defaults to "-",
	// meaning the default tailnet of the credentials.
	Tailnet string
	// APIKey is a Tailscale API access token ("tskey-api-..."). Takes
	// precedence over the OAuth client when both are set.
	APIKey string
	// OAuthClientID and OAuthClientSecret identify an OAuth client; both must
	// be set for OAuth to be used.
	OAuthClientID     string
	OAuthClientSecret string
	// HTTPClient to use. When nil a client with DefaultTimeout is created.
	HTTPClient *http.Client
	// Version is reported in the User-Agent header as "tailwatch/<Version>".
	// Defaults to "dev".
	Version string
}

// APIError is returned (wrapped) when the control API answers with a non-2xx
// status. Message is the API's {"message": ...} body when present.
type APIError struct {
	Status  int
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	text := http.StatusText(e.Status)
	var b strings.Builder
	b.WriteString("tailscale api: ")
	if text == "" {
		b.WriteString("status ")
	}
	b.WriteString(strconv.Itoa(e.Status))
	if text != "" {
		b.WriteString(" ")
		b.WriteString(text)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Client talks to the Tailscale control API. It is safe for concurrent use.
type Client struct {
	base    *url.URL
	baseErr error
	credErr error
	tailnet string

	apiKey      string
	oauthID     string
	oauthSecret string

	hc      *http.Client
	ua      string
	log     *slog.Logger
	maxBody int64

	// now and sleep are indirected so tests can drive time deterministically.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// OAuth access token cache. tokMu guards tok/tokExp and is never held
	// across network calls; refreshMu serialises token fetches so that
	// concurrent callers share a single refresh.
	tokMu     sync.RWMutex
	tok       string
	tokExp    time.Time
	refreshMu sync.Mutex
	useBasic  bool // guarded by refreshMu: send client credentials as HTTP basic auth
}

var _ source.ControlAPI = (*Client)(nil)

// New creates a Client. It never fails: configuration problems surface as
// errors from the individual methods. A nil logger uses slog.Default().
func New(o Options, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{
		tailnet:     strings.TrimSpace(o.Tailnet),
		apiKey:      strings.TrimSpace(o.APIKey),
		oauthID:     strings.TrimSpace(o.OAuthClientID),
		oauthSecret: strings.TrimSpace(o.OAuthClientSecret),
		hc:          o.HTTPClient,
		log:         log.With("component", "tsapi"),
		maxBody:     defaultMaxBody,
		now:         time.Now,
		sleep:       sleepCtx,
	}
	if c.tailnet == "" {
		c.tailnet = DefaultTailnet
	}
	if c.hc == nil {
		c.hc = &http.Client{
			Timeout: DefaultTimeout,
			// The API never redirects; refusing to follow makes sure a bearer
			// token is never replayed to an unexpected location.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	version := strings.TrimSpace(o.Version)
	if version == "" || !headerSafe(version) {
		version = defaultVersion
	}
	c.ua = "tailwatch/" + version

	base := strings.TrimSpace(o.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	switch {
	case err != nil:
		c.baseErr = fmt.Errorf("invalid base URL: %w", err)
	case (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		c.baseErr = fmt.Errorf("invalid base URL %q: must be http(s)://host", base)
	default:
		u.RawQuery, u.Fragment = "", ""
		c.base = u
		if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
			c.log.Warn("control API base URL is not https; credentials would be sent in clear text", "baseURL", base)
		}
	}

	switch {
	case c.apiKey != "" && !headerSafe(c.apiKey):
		c.credErr = errors.New("API key contains characters that are not allowed in an HTTP header")
	case c.apiKey == "" && c.oauthID != "" && c.oauthSecret != "" && (!headerSafe(c.oauthID) || !headerSafe(c.oauthSecret)):
		c.credErr = errors.New("OAuth client credentials contain characters that are not allowed in an HTTP header")
	}
	if c.apiKey == "" && (c.oauthID != "") != (c.oauthSecret != "") {
		c.log.Warn("incomplete OAuth client configuration: both client id and secret are required; control API disabled")
	}
	return c
}

// Configured reports whether credentials are available: an API key, or both
// OAuth client id and secret.
func (c *Client) Configured() bool {
	return c.apiKey != "" || (c.oauthID != "" && c.oauthSecret != "")
}

// Tailnet returns the tailnet name used in API paths ("-" for the default).
func (c *Client) Tailnet() string { return c.tailnet }

// ready returns the error every method must return before touching the
// network, prefixed with the operation name.
func (c *Client) ready(op string) error {
	switch {
	case !c.Configured():
		return fmt.Errorf("tsapi: %s: %w", op, source.ErrNotConfigured)
	case c.baseErr != nil:
		return fmt.Errorf("tsapi: %s: %w", op, c.baseErr)
	case c.credErr != nil:
		return fmt.Errorf("tsapi: %s: %w", op, c.credErr)
	}
	return nil
}

// request describes one API call. body must be fully materialised so the
// request can be replayed on retry.
type request struct {
	method      string
	path        string // absolute path, e.g. "/api/v2/device/123/tags"
	query       url.Values
	body        []byte
	contentType string
	// noAuth skips the bearer token (used for the token endpoint itself).
	noAuth bool
	// basicUser/basicPass, when set, add HTTP basic authentication.
	basicUser, basicPass string
}

// response is a fully read API response.
type response struct {
	status int
	header http.Header
	body   []byte
}

// do performs a request with the package's retry and re-authentication
// policy. Errors returned are already redacted and carry no "tsapi:" prefix;
// callers add the operation context.
func (c *Client) do(ctx context.Context, op string, r request) (*response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target := c.base.String() + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	var retried, reauthed bool
	for {
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, target, body)
		if err != nil {
			return nil, c.redact(fmt.Errorf("build request: %w", err))
		}
		req.Header.Set("User-Agent", c.ua)
		req.Header.Set("Accept", "application/json")
		if r.body != nil && r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		var usedToken string
		if !r.noAuth {
			tok, err := c.bearer(ctx)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			usedToken = tok
		}
		if r.basicUser != "" {
			req.SetBasicAuth(r.basicUser, r.basicPass)
		}

		start := c.now()
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, c.redact(fmt.Errorf("%s %s: %w", r.method, r.path, err))
		}
		data, readErr := readBody(resp.Body, c.maxBody)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, c.redact(fmt.Errorf("%s %s: read response: %w", r.method, r.path, readErr))
		}
		c.log.Debug("control api request",
			"op", op, "method", r.method, "path", r.path,
			"status", resp.StatusCode, "bytes", len(data), "duration", c.now().Sub(start))

		switch {
		case (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && !retried:
			retried = true
			delay := retryDelay(resp.Header.Get("Retry-After"), c.now())
			c.log.Warn("control api request failed; retrying once",
				"op", op, "status", resp.StatusCode, "delay", delay)
			if err := c.sleep(ctx, delay); err != nil {
				return nil, fmt.Errorf("%s %s: retry aborted: %w", r.method, r.path, err)
			}
			continue
		case resp.StatusCode == http.StatusUnauthorized && usedToken != "" && c.apiKey == "" && !reauthed:
			// The cached OAuth token was rejected (revoked or expired early).
			// Drop it and try once more with a fresh one.
			reauthed = true
			c.invalidateToken(usedToken)
			c.log.Info("control api rejected the cached access token; refreshing", "op", op)
			continue
		}
		return &response{status: resp.StatusCode, header: resp.Header, body: data}, nil
	}
}

// checkStatus converts a non-2xx response into an error. 404 wraps
// source.ErrNotFound; every failure wraps an *APIError.
func checkStatus(resp *response) error {
	if resp.status >= 200 && resp.status < 300 {
		return nil
	}
	apiErr := &APIError{Status: resp.status, Message: parseMessage(resp.body)}
	if resp.status == http.StatusNotFound {
		return fmt.Errorf("%w: %w", source.ErrNotFound, apiErr)
	}
	return apiErr
}

// parseMessage extracts a human readable message from an error body. It
// understands {"message": ...} (Tailscale) as well as RFC 6749 style
// {"error": ..., "error_description": ...} bodies.
func parseMessage(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	for _, key := range []string{"message", "error_description", "error"} {
		switch v := m[key].(type) {
		case string:
			if s := sanitizeMessage(v); s != "" {
				return s
			}
		case map[string]any:
			if s, ok := v["message"].(string); ok {
				if s = sanitizeMessage(s); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// sanitizeMessage strips control characters (log injection) and bounds the
// length of a message taken from an upstream response.
func sanitizeMessage(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxErrorMessageLen {
		s = string(r[:maxErrorMessageLen]) + "…"
	}
	return s
}

// oauthErrorCode returns the RFC 6749 "error" code of a token response body.
func oauthErrorCode(body []byte) string {
	var m struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	return m.Error
}

// readBody reads at most limit bytes and fails if the body is larger.
func readBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return data, nil
}

// retryDelay derives the wait before a retry from a Retry-After header
// (seconds or HTTP-date), bounded to [minRetryDelay, maxRetryDelay].
func retryDelay(header string, now time.Time) time.Duration {
	d := minRetryDelay
	if h := strings.TrimSpace(header); h != "" {
		// On overflow ParseInt reports ErrRange and clamps to MaxInt64/MinInt64,
		// which the bounds below turn into the cap or the minimum.
		if secs, err := strconv.ParseInt(h, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
			if secs >= int64(maxRetryDelay/time.Second) {
				return maxRetryDelay
			}
			if secs > 0 {
				d = time.Duration(secs) * time.Second
			}
		} else if t, err := http.ParseTime(h); err == nil {
			d = t.Sub(now)
		}
	}
	if d < minRetryDelay {
		d = minRetryDelay
	}
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	return d
}

// sleepCtx waits for d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// headerSafe reports whether s can be sent as an HTTP header value without
// leaking or breaking anything: printable ASCII only, no whitespace.
func headerSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] >= 0x7f {
			return false
		}
	}
	return s != ""
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// bearerRe matches bearer/basic credentials wherever they appear in text.
var bearerRe = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)

// secrets lists every value that must never appear in an error string.
func (c *Client) secrets() []string {
	s := make([]string, 0, 4)
	if c.apiKey != "" {
		s = append(s, c.apiKey)
	}
	if c.oauthSecret != "" {
		s = append(s, c.oauthSecret)
		if c.oauthID != "" {
			s = append(s, base64.StdEncoding.EncodeToString([]byte(c.oauthID+":"+c.oauthSecret)))
		}
	}
	// The raw token is scrubbed even when it is expired or about to be:
	// it may still be accepted upstream for a little while.
	if tok := c.peekToken(); tok != "" {
		s = append(s, tok)
	}
	return s
}

// redactString scrubs credentials and Authorization values from s.
func (c *Client) redactString(s string) string {
	for _, sec := range c.secrets() {
		s = strings.ReplaceAll(s, sec, "[REDACTED]")
	}
	return bearerRe.ReplaceAllString(s, "$1 [REDACTED]")
}

// redact returns err unchanged when its text is clean; otherwise a new error
// with the scrubbed text that still satisfies errors.Is for context errors.
// The original error is deliberately dropped from the chain so the secret
// cannot be recovered through errors.As.
func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	r := c.redactString(s)
	if r == s {
		return err
	}
	var cause error
	switch {
	case errors.Is(err, context.Canceled):
		cause = context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		cause = context.DeadlineExceeded
	}
	return &redactedError{msg: r, cause: cause}
}

// redactedError replaces an error whose text contained a credential.
type redactedError struct {
	msg   string
	cause error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.cause }
