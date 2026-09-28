package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/source"
)

// HTTP server hardening constants.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 5 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 16 << 10
	shutdownTimeout   = 5 * time.Second

	// whoisRequestTimeout bounds one identity lookup within a request.
	whoisRequestTimeout = 3 * time.Second
)

// reportSource is what the metrics handler needs from the sampler.
type reportSource interface {
	Latest() (agentproto.Report, bool)
}

// serverOptions configure newServer.
type serverOptions struct {
	Version string
	Policy  *policy
	// WhoIs is required when Policy.mode uses WhoIs.
	WhoIs whoisSource
	// Local returns this node's own identity (owner login, MagicDNS
	// suffix). It is consulted when no allow-lists are configured or when
	// --allow-node is set.
	Local   func(ctx context.Context) localIdentity
	Reports reportSource
	Log     *slog.Logger
	Now     func() time.Time

	// RateLimit / RateBurst override the per-IP budget (tests); zero means
	// the defaults.
	RateLimit float64
	RateBurst int
	// WhoisTTL overrides the identity cache TTL (tests).
	WhoisTTL time.Duration
	// WarnEvery overrides the rejected-request warning spacing (tests).
	WarnEvery time.Duration
}

// server is the agent's HTTP handler: security headers, per-IP rate limit,
// authentication, then two read-only JSON routes.
type server struct {
	version string
	policy  *policy
	whois   *whoisCache
	local   func(ctx context.Context) localIdentity
	reports reportSource
	log     *slog.Logger
	now     func() time.Time
	limiter *rateLimiter
	warns   *logLimiter
	health  []byte
}

// newServer validates options and returns a server.
func newServer(o serverOptions) (*server, error) {
	if o.Policy == nil {
		return nil, errors.New("server: policy is required")
	}
	if o.Reports == nil {
		return nil, errors.New("server: report source is required")
	}
	if o.Policy.mode.usesWhoIs() && o.WhoIs == nil {
		return nil, errors.New("server: whois source is required for auth mode " + string(o.Policy.mode))
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Local == nil {
		o.Local = func(context.Context) localIdentity { return localIdentity{} }
	}
	health, err := json.Marshal(agentproto.HealthResponse{
		OK:              true,
		AgentVersion:    o.Version,
		ProtocolVersion: agentproto.ProtocolVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("server: encode health: %w", err)
	}
	return &server{
		version: o.Version,
		policy:  o.Policy,
		whois:   newWhoisCache(o.WhoIs, o.WhoisTTL, o.Now),
		local:   o.Local,
		reports: o.Reports,
		log:     o.Log.With("component", "server"),
		now:     o.Now,
		limiter: newRateLimiter(o.RateLimit, o.RateBurst, o.Now),
		warns:   newLogLimiter(o.WarnEvery, o.Now),
		health:  health,
	}, nil
}

// Handler returns the http.Handler for the agent.
func (s *server) Handler() http.Handler { return s }

// httpServer returns a hardened *http.Server for addr using this handler.
func (s *server) httpServer(ctx context.Context, addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
}

// ServeHTTP implements http.Handler.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())

	ip := remoteIP(r.RemoteAddr)
	if !s.limiter.allow(ip) {
		w.Header().Set("Retry-After", "1")
		s.reject(w, r, ip, nil, http.StatusTooManyRequests, "rate limited", "rate limited")
		return
	}

	ident, status, reason := s.authenticate(r, ip)
	if status != 0 {
		msg := "forbidden"
		if status == http.StatusServiceUnavailable {
			msg = "identity lookup unavailable"
		}
		s.reject(w, r, ip, ident, status, msg, reason)
		return
	}

	switch r.URL.Path {
	case agentproto.PathMetrics, agentproto.PathHealth:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, errorBody("method not allowed"))
			return
		}
	default:
		writeJSON(w, http.StatusNotFound, errorBody("not found"))
		return
	}

	s.log.DebugContext(r.Context(), "request", "path", r.URL.Path, "ip", ip, "identity", identityAttrs(ident))
	if r.URL.Path == agentproto.PathHealth {
		writeRaw(w, http.StatusOK, s.health)
		return
	}
	rep, ok := s.reports.Latest()
	if !ok {
		w.Header().Set("Retry-After", "2")
		writeJSON(w, http.StatusServiceUnavailable, errorBody("not ready"))
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// authenticate applies the configured policy. It returns the identity (when
// resolved), an HTTP status to send on failure (0 on success) and a
// log-only reason.
func (s *server) authenticate(r *http.Request, ip string) (*source.WhoIs, int, string) {
	if s.policy.mode.usesToken() {
		if !s.policy.tokenOK(r.Header.Get(agentproto.HeaderToken)) {
			if r.Header.Get(agentproto.HeaderToken) == "" {
				return nil, http.StatusForbidden, "missing token"
			}
			return nil, http.StatusForbidden, "token mismatch"
		}
	}
	if !s.policy.mode.usesWhoIs() {
		return nil, 0, ""
	}
	if ip == "" {
		return nil, http.StatusForbidden, "unparsable remote address"
	}
	ctx, cancel := context.WithTimeout(r.Context(), whoisRequestTimeout)
	defer cancel()
	w, err := s.whois.lookup(ctx, ip, r.RemoteAddr)
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			return nil, http.StatusForbidden, "source address is not a tailnet peer"
		}
		return nil, http.StatusServiceUnavailable, "whois failed: " + err.Error()
	}
	var local localIdentity
	if s.policy.needsLocalIdentity() {
		local = s.local(ctx)
	}
	ok, reason := s.policy.identityAllowed(w, local)
	if !ok {
		return w, http.StatusForbidden, reason
	}
	return w, 0, reason
}

// reject writes an error response and logs it at warn, rate-limited per IP.
func (s *server) reject(w http.ResponseWriter, r *http.Request, ip string, ident *source.WhoIs, status int, msg, reason string) {
	if s.warns.allow(ip + "|" + strconv.Itoa(status)) {
		s.log.WarnContext(r.Context(), "request rejected",
			"status", status,
			"reason", reason,
			"ip", ip,
			"method", r.Method,
			"path", r.URL.Path,
			"identity", identityAttrs(ident),
		)
	}
	writeJSON(w, status, errorBody(msg))
}

// identityAttrs renders an identity for logs (never secrets).
func identityAttrs(w *source.WhoIs) slog.Value {
	if w == nil {
		return slog.StringValue("")
	}
	return slog.GroupValue(
		slog.String("login", w.LoginName),
		slog.String("node", w.NodeName),
		slog.Any("tags", w.Tags),
	)
}

// setSecurityHeaders adds the fixed response headers.
func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
}

// errorBody is the JSON error shape.
func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

// writeJSON encodes v and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		writeRaw(w, http.StatusInternalServerError, []byte(`{"error":"internal"}`+"\n"))
		return
	}
	writeRaw(w, status, buf.Bytes())
}

// writeRaw writes a pre-encoded JSON body.
func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// remoteIP extracts the IP from an "ip:port" remote address; it returns ""
// when the address cannot be parsed.
func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
