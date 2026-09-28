// Command tailwatch-agent is the per-device metrics daemon for Tailwatch.
// It samples system metrics with internal/sysmetrics and serves them as
// JSON on the device's Tailscale IP, authenticating every request with the
// local tailscaled (WhoIs) and/or a shared token. It is read-only by
// construction: two GET endpoints, no configuration surface, no file access
// beyond what the metrics need.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/sysmetrics"
	"github.com/evilgenius79/fable-tailscale/internal/tslocal"
	"github.com/evilgenius79/fable-tailscale/internal/version"
)

// Exit codes.
const (
	exitOK    = 0
	exitFatal = 1
	exitUsage = 2
)

// statusCacheTTL is how long the cached tailscaled status (owner login,
// version, IPs, health) is reused.
const statusCacheTTL = 60 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is main without the process-level plumbing so it can be tested.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := parseConfig(args, getenv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintf(stderr, "tailwatch-agent: %v\n", err)
		return exitUsage
	}
	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "tailwatch-agent %s\n", version.String())
		return exitOK
	}

	log := newLogger(cfg.LogLevel, cfg.LogJSON, stderr)
	for _, w := range cfg.Warnings {
		log.Warn(w)
	}

	local := tslocal.New(cfg.Socket, log)
	state := newLocalState(local, statusCacheTTL, log)

	addr, err := newListenResolver(local, log).resolve(ctx, cfg.Listen, cfg.Port, cfg.InsecureListenAny)
	if err != nil {
		log.Error("cannot determine listen address", "err", err)
		return exitFatal
	}

	pol, err := newPolicy(cfg.Auth, cfg.Token, cfg.AllowUsers, cfg.AllowTags, cfg.AllowNodes)
	if err != nil {
		log.Error("invalid authentication configuration", "err", err)
		return exitUsage
	}

	sampler := sysmetrics.NewSampler(cfg.Interval, version.Version, log)
	sampler.SetTailscaleInfo(state.tailscaleInfo)

	srv, err := newServer(serverOptions{
		Version: version.Version,
		Policy:  pol,
		WhoIs:   local,
		Local:   state.identity,
		Reports: sampler,
		Log:     log,
	})
	if err != nil {
		log.Error("cannot build server", "err", err)
		return exitFatal
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("cannot listen", "addr", addr, "err", err)
		return exitFatal
	}

	if cfg.Auth.usesWhoIs() && !pol.hasAllowLists() {
		if owner := state.ownerLogin(ctx); owner == "" {
			log.Warn("this node has no owner login (tagged node or tailscaled not ready); without allow-lists only callers tagged " + hubTag + " are accepted")
		} else {
			log.Info("accepting callers owned by the node owner or tagged "+hubTag, "owner", owner)
		}
	}
	log.Info("tailwatch-agent listening",
		"version", version.String(),
		"addr", ln.Addr().String(),
		"auth", string(cfg.Auth),
		"allowUsers", cfg.AllowUsers,
		"allowTags", cfg.AllowTags,
		"allowNodes", cfg.AllowNodes,
		"interval", cfg.Interval,
		"protocol", agentproto.ProtocolVersion,
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sampler.Run(ctx)
	}()

	hs := srv.httpServer(ctx, addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- hs.Serve(ln) }()

	code := exitOK
	select {
	case <-ctx.Done():
		log.Info("shutting down", "reason", "signal")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "err", err)
			code = exitFatal
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown incomplete; closing connections", "err", err)
		_ = hs.Close()
	}
	wg.Wait()
	log.Info("stopped")
	return code
}

// newLogger builds the process logger.
func newLogger(level string, asJSON bool, w io.Writer) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if asJSON {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}

// localState caches the local tailscaled status for statusCacheTTL and
// derives what the agent needs from it: the node owner's login and MagicDNS
// suffix (for the WhoIs policy) and the TailscaleInfo section of reports.
type localState struct {
	src statusSource
	ttl time.Duration
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	st      *source.LocalStatus
	fetched time.Time
}

// newLocalState returns a state cache over src.
func newLocalState(src statusSource, ttl time.Duration, log *slog.Logger) *localState {
	if log == nil {
		log = slog.Default()
	}
	if ttl <= 0 {
		ttl = statusCacheTTL
	}
	return &localState{src: src, ttl: ttl, log: log.With("component", "localstate"), now: time.Now}
}

// status returns the cached status, refreshing it when older than ttl. On a
// refresh failure the stale value (if any) is returned with a nil error so
// a transient daemon hiccup does not lock the hub out. The daemon is never
// called while holding the lock, so a hung tailscaled cannot stall other
// callers beyond their own context deadline.
func (l *localState) status(ctx context.Context) (*source.LocalStatus, error) {
	l.mu.Lock()
	cached, fetched := l.st, l.fetched
	l.mu.Unlock()
	if cached != nil && l.now().Sub(fetched) < l.ttl {
		return cached, nil
	}
	st, err := l.fetch(ctx)
	if err != nil {
		if cached != nil {
			l.log.DebugContext(ctx, "status refresh failed; using cached value", "err", err)
			return cached, nil
		}
		return nil, err
	}
	return st, nil
}

// fetch asks tailscaled for a fresh status and caches it on success.
func (l *localState) fetch(ctx context.Context) (*source.LocalStatus, error) {
	st, err := l.src.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	if st == nil {
		return nil, errors.New("status: empty response")
	}
	l.mu.Lock()
	l.st, l.fetched = st, l.now()
	l.mu.Unlock()
	return st, nil
}

// identity returns this node's owner login ("" when unknown or when the
// node is tagged) and the local MagicDNS suffix ("" when unknown).
func (l *localState) identity(ctx context.Context) localIdentity {
	ctx, cancel := context.WithTimeout(ctx, whoisRequestTimeout)
	defer cancel()
	st, err := l.status(ctx)
	if err != nil {
		l.log.DebugContext(ctx, "local identity unavailable", "err", err)
		return localIdentity{}
	}
	return localIdentity{Owner: st.Self.UserLogin, MagicDNSSuffix: st.MagicDNSSuffix}
}

// ownerLogin returns this node's owner login, or "" when unknown or when
// the node is tagged.
func (l *localState) ownerLogin(ctx context.Context) string {
	return l.identity(ctx).Owner
}

// tailscaleInfo builds the report's Tailscale section from a fresh daemon
// status (the sampler already limits calls to once a minute). On failure
// the error is returned and the sampler keeps its previous value.
func (l *localState) tailscaleInfo(ctx context.Context) (*agentproto.TailscaleInfo, error) {
	st, err := l.fetch(ctx)
	if err != nil {
		return nil, err
	}
	return &agentproto.TailscaleInfo{
		Version:      st.Version,
		BackendState: st.BackendState,
		IPs:          append([]string(nil), st.TailscaleIPs...),
		Health:       append([]string(nil), st.Health...),
	}, nil
}
