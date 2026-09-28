// Package httpapi is the Tailwatch hub's HTTP layer: Tailscale-identity
// authentication (WhoIs), browser hardening, the versioned JSON API from
// docs/API.md, the Server-Sent Events stream and the embedded single-page
// UI.
//
// Every response passes through one middleware chain: panic recovery,
// request logging, security headers and, under /api/, a Host allow-list
// (the DNS-rebinding defence), a 64 KiB body limit, cross-site request
// rejection, authentication with per-IP identity caching, a per-identity
// token-bucket rate limit and a 30 second handler timeout (the SSE stream
// is exempt from the timeout but capped in number per identity and in
// total). /healthz answers without authentication; every other non-API
// path serves the UI.
package httpapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/alerts"
	"github.com/evilgenius79/fable-tailscale/internal/collector"
	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10
	shutdownTimeout   = 10 * time.Second

	// autoRetryInterval and autoRetryTimeout bound how long "auto" waits
	// for tailscaled to report a Tailscale IPv4 address.
	autoRetryInterval = 2 * time.Second
	autoRetryTimeout  = 30 * time.Second
	demoListen        = "127.0.0.1:8484"
)

// Tailscale address ranges: CGNAT IPv4 and the Tailscale IPv6 ULA prefix.
var (
	tailscaleCGNAT = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleULA   = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// Deps are the collaborators the server needs. Cfg, Store, Collector and
// Alerts are required. API may be nil or unconfigured (admin actions then
// answer not_configured). Local is required for Tailscale authentication
// and for resolving --listen auto. UI is the web/dist file system (nil
// serves the "UI not built" page). Auth overrides the authenticator that
// would otherwise be derived from Cfg (NewNoAuth in demo or
// --insecure-no-auth mode, NewTailscaleAuth otherwise); it is meant for
// tests. A nil Log uses slog.Default().
type Deps struct {
	Cfg       *config.Config
	Store     *store.Store
	Collector *collector.Collector
	Alerts    *alerts.Engine
	API       source.ControlAPI
	Local     source.LocalSource
	UI        fs.FS
	Log       *slog.Logger
	Version   string
	Auth      Authenticator
}

// Server is the hub HTTP server.
type Server struct {
	d       Deps
	cfg     *config.Config
	log     *slog.Logger
	auth    Authenticator
	limiter *rateLimiter
	hosts   *hostPolicy
	streams streamGauge
	handler http.Handler
	tls     bool

	// now is the clock; tests override it.
	now func() time.Time
	// heartbeat is the SSE ping interval; tests shorten it.
	heartbeat time.Duration
	// handlerTimeout bounds non-streaming handlers; tests shorten it.
	handlerTimeout time.Duration
	// maxStreamsPerIdentity and maxStreamsTotal cap concurrently open SSE
	// streams; tests lower them.
	maxStreamsPerIdentity, maxStreamsTotal int
	// autoRetry and autoTimeout tune --listen auto resolution.
	autoRetry, autoTimeout time.Duration

	// shutdown is closed when ListenAndServe starts shutting down so SSE
	// handlers return promptly and Shutdown can complete.
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// NewServer validates deps and builds the handler chain.
func NewServer(d Deps) (*Server, error) {
	switch {
	case d.Cfg == nil:
		return nil, errors.New("httpapi: Deps.Cfg is required")
	case d.Store == nil:
		return nil, errors.New("httpapi: Deps.Store is required")
	case d.Collector == nil:
		return nil, errors.New("httpapi: Deps.Collector is required")
	case d.Alerts == nil:
		return nil, errors.New("httpapi: Deps.Alerts is required")
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	noAuth := d.Cfg.Demo || d.Cfg.InsecureNoAuth
	if d.Cfg.InsecureNoAuth && !config.IsLoopbackListen(d.Cfg.Listen) {
		return nil, errors.New("httpapi: --insecure-no-auth is only allowed with a loopback --listen address")
	}
	auth := d.Auth
	if auth == nil {
		switch {
		case noAuth:
			auth = NewNoAuth()
		case d.Local == nil:
			return nil, errors.New("httpapi: Deps.Local is required for Tailscale authentication")
		default:
			auth = NewTailscaleAuth(d.Local, d.Cfg)
		}
	}
	s := &Server{
		d:              d,
		cfg:            d.Cfg,
		log:            d.Log,
		auth:           auth,
		limiter:        newRateLimiter(rateLimitPerSecond, rateLimitBurst, nil),
		hosts:          newHostPolicy(d.Cfg),
		tls:            d.Cfg.TLSCert != "" && d.Cfg.TLSKey != "",
		now:            time.Now,
		heartbeat:      sseHeartbeat,
		handlerTimeout: handlerTimeout,

		maxStreamsPerIdentity: maxStreamsPerIdentity,
		maxStreamsTotal:       maxStreamsTotal,
		autoRetry:             autoRetryInterval,
		autoTimeout:           autoRetryTimeout,
		shutdown:              make(chan struct{}),
	}
	s.handler = s.buildHandler()
	return s, nil
}

// Handler returns the complete handler: middleware chain, API, SSE stream,
// health check and UI.
func (s *Server) Handler() http.Handler { return s.handler }

// buildHandler assembles the middleware chain and top-level routing.
func (s *Server) buildHandler() http.Handler {
	api := s.requireKnownHost(bodyLimit(browserHardening(s.authenticate(s.rateLimit(s.apiMux())))))
	ui := newUIHandler(s.d.UI)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			s.handleHealthz(w, r)
		case isAPIPath(r.URL.Path) || r.URL.Path == "/api":
			api.ServeHTTP(w, r)
		default:
			ui.ServeHTTP(w, r)
		}
	})
	return s.recoverer(s.logging(s.securityHeaders(root)))
}

// handleHealthz answers "ok" without authentication or details.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// ListenAndServe resolves the listen address, binds it, serves until ctx
// is cancelled and then shuts down gracefully (10 second budget). It
// returns nil after a clean shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr, err := s.resolveListen(ctx)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("httpapi: listen on %s: %w", addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on an existing listener with the same lifecycle as
// ListenAndServe. The listener is closed when Serve returns.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      0, // SSE streams; handlers use http.TimeoutHandler instead
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	scheme := "http"
	if s.tls {
		cert, err := tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("httpapi: load TLS certificate: %w", err)
		}
		srv.TLSConfig = tlsConfig(cert)
		scheme = "https"
	}
	s.hosts.setBound(ln.Addr().String())
	s.log.Info("httpapi: listening", "url", scheme+"://"+ln.Addr().String(), "authMode", s.cfg.AuthMode(), "tls", s.tls, "version", s.d.Version)
	if s.cfg.AuthMode() == authModeNone && !s.cfg.Demo {
		s.log.Warn("httpapi: authentication is DISABLED (--insecure-no-auth); every caller is an admin")
	}

	errCh := make(chan error, 1)
	go func() {
		var err error
		if s.tls {
			err = srv.ServeTLS(ln, "", "")
		} else {
			err = srv.Serve(ln)
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("httpapi: serve: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	s.shutdownOnce.Do(func() { close(s.shutdown) })
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		<-errCh
		return fmt.Errorf("httpapi: shutdown: %w", err)
	}
	<-errCh
	s.log.Info("httpapi: stopped")
	return nil
}

// tlsConfig returns a TLS 1.2+ configuration with modern cipher suites.
func tlsConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
}

// --- listen resolution -----------------------------------------------------

// resolveListen turns cfg.Listen into a bind address. "auto" becomes the
// hub's first Tailscale IPv4 address (127.0.0.1 in demo mode); an explicit
// address must be a Tailscale or loopback address unless
// --insecure-listen-any is set.
func (s *Server) resolveListen(ctx context.Context) (string, error) {
	listen := strings.TrimSpace(s.cfg.Listen)
	if listen == config.ListenAuto {
		if s.cfg.Demo {
			return demoListen, nil
		}
		ip, err := s.autoIPv4(ctx)
		if err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(config.DefaultPort)), nil
	}
	host, port, err := config.SplitListen(listen)
	if err != nil {
		return "", fmt.Errorf("httpapi: --listen %q: %w", listen, err)
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else if strings.EqualFold(host, "localhost") {
		addrs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	} else {
		resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return "", fmt.Errorf("httpapi: --listen %q: cannot resolve host %q", listen, host)
		}
		addrs = resolved
	}
	for _, a := range addrs {
		kind := classifyListenIP(a)
		if kind == listenTailscale || kind == listenLoopback {
			continue
		}
		if !s.cfg.InsecureListenAny {
			return "", fmt.Errorf("httpapi: refusing to listen on %s address %s: use --listen auto, the hub's Tailscale IP (100.x.y.z:%d), a loopback address, or --insecure-listen-any", kind, a, config.DefaultPort)
		}
		s.log.Warn("httpapi: listening on a non-Tailscale address because --insecure-listen-any is set; the hub may be reachable from outside the tailnet and WhoIs authentication will reject such callers", "addr", a.String(), "kind", string(kind))
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// autoIPv4 asks tailscaled for the hub's first Tailscale IPv4 address,
// retrying briefly while the daemon comes up.
func (s *Server) autoIPv4(ctx context.Context) (netip.Addr, error) {
	if s.d.Local == nil {
		return netip.Addr{}, errors.New("httpapi: --listen auto needs a tailscaled client (pass --listen <ip>:8484 or --demo)")
	}
	// Wall clock on purpose: this waits for a real daemon, not for the
	// (injectable) request clock.
	deadline := time.Now().Add(s.autoTimeout)
	var lastErr error
	for attempt := 1; ; attempt++ {
		st, err := s.d.Local.Status(ctx)
		switch {
		case err != nil:
			lastErr = err
		case st == nil:
			lastErr = errors.New("empty status")
		default:
			if ip, ok := firstIPv4(st.TailscaleIPs); ok {
				return ip, nil
			}
			lastErr = fmt.Errorf("tailscaled reports no IPv4 address yet (state %q)", st.BackendState)
		}
		if ctx.Err() != nil {
			return netip.Addr{}, fmt.Errorf("httpapi: --listen auto: %w (last error: %v)", ctx.Err(), lastErr)
		}
		if time.Now().Add(s.autoRetry).After(deadline) {
			return netip.Addr{}, fmt.Errorf("httpapi: --listen auto: could not determine the hub's Tailscale IPv4 address: %w; is tailscaled running and logged in? Pass --listen <tailscale-ip>:%d, or --demo to run without a tailnet", lastErr, config.DefaultPort)
		}
		if attempt == 1 {
			s.log.Info("httpapi: waiting for tailscaled to report a Tailscale IPv4 address", "retryEvery", s.autoRetry, "timeout", s.autoTimeout, "err", lastErr)
		}
		t := time.NewTimer(s.autoRetry)
		select {
		case <-ctx.Done():
			t.Stop()
			return netip.Addr{}, fmt.Errorf("httpapi: --listen auto: %w (last error: %v)", ctx.Err(), lastErr)
		case <-t.C:
		}
	}
}

// listenKind classifies a bind address.
type listenKind string

const (
	listenTailscale listenKind = "tailscale"
	listenLoopback  listenKind = "loopback"
	listenAny       listenKind = "unspecified"
	listenOther     listenKind = "non-tailscale"
)

// classifyListenIP reports whether ip is a Tailscale address, loopback,
// unspecified (0.0.0.0 / ::) or something else.
func classifyListenIP(ip netip.Addr) listenKind {
	ip = ip.Unmap()
	switch {
	case tailscaleCGNAT.Contains(ip) || tailscaleULA.Contains(ip):
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
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		if a = a.Unmap(); a.Is4() {
			return a, true
		}
	}
	return netip.Addr{}, false
}
