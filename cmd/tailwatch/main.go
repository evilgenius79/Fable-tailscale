// Command tailwatch is the Tailwatch hub: it watches a tailnet through the
// local tailscaled, the Tailscale control API and tailwatch-agent instances,
// stores history in SQLite, evaluates watchdog rules and serves the web UI and
// API over the tailnet.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentclient"
	"github.com/evilgenius79/fable-tailscale/internal/alerts"
	"github.com/evilgenius79/fable-tailscale/internal/collector"
	"github.com/evilgenius79/fable-tailscale/internal/config"
	"github.com/evilgenius79/fable-tailscale/internal/demo"
	"github.com/evilgenius79/fable-tailscale/internal/httpapi"
	"github.com/evilgenius79/fable-tailscale/internal/model"
	"github.com/evilgenius79/fable-tailscale/internal/source"
	"github.com/evilgenius79/fable-tailscale/internal/store"
	"github.com/evilgenius79/fable-tailscale/internal/tsapi"
	"github.com/evilgenius79/fable-tailscale/internal/tslocal"
	"github.com/evilgenius79/fable-tailscale/internal/version"
	"github.com/evilgenius79/fable-tailscale/web"
)

// demoBackfill is how much synthetic history a fresh demo database gets. It
// is a variable so tests can shrink it: a full day of samples takes tens of
// seconds to generate under the race detector.
var demoBackfill = 24 * time.Hour

const (
	exitOK    = 0
	exitFatal = 1
	exitUsage = 2

	// demoSeed makes every demo instance identical, which keeps screenshots
	// and documentation reproducible.
	demoSeed = 1

	// shutdownGrace bounds how long components may take to stop after a
	// signal before the process exits anyway.
	shutdownGrace = 12 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is main without the process-level plumbing so it can be tested. It
// returns the process exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(args, getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, config.Usage())
			return exitOK
		}
		fmt.Fprintf(stderr, "tailwatch: %v\n", err)
		fmt.Fprintf(stderr, "Run 'tailwatch --help' for the full flag reference.\n")
		return exitUsage
	}
	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "tailwatch %s\n", version.String())
		return exitOK
	}

	log := newLogger(stderr, cfg)
	log.Info("starting tailwatch",
		"version", version.Version,
		"commit", version.Commit,
		"demo", cfg.Demo,
		"auth", cfg.AuthMode(),
		"controlApi", cfg.HasControlAPI(),
		"adminActions", cfg.EnableAdminActions,
		"agents", cfg.AgentEnabled,
	)
	if cfg.InsecureNoAuth {
		log.Warn("authentication is DISABLED (--insecure-no-auth); every caller is treated as an admin")
	}
	if cfg.InsecureListenAny {
		log.Warn("--insecure-listen-any is set; the hub may be reachable from outside the tailnet")
	}
	if !cfg.Demo && !cfg.InsecureNoAuth && len(cfg.Admins) == 0 && len(cfg.AdminTags) == 0 {
		log.Warn("no --admins or --admin-tags configured; every caller is a viewer and nobody can ack alerts or edit rules")
	}
	if !cfg.Demo && !cfg.InsecureNoAuth {
		for _, v := range cfg.Viewers {
			if v == "*" {
				log.Warn("--viewers defaults to *; any tailnet identity that can reach the hub can read inventory and metrics (narrow --viewers / --viewer-tags on shared tailnets)")
				break
			}
		}
	}
	for _, u := range []struct{ name, raw string }{
		{"webhook", cfg.WebhookURL},
		{"slack", cfg.SlackWebhookURL},
		{"ntfy", cfg.NtfyURL},
	} {
		if strings.HasPrefix(u.raw, "http://") {
			log.Warn("notifier URL is plain HTTP; prefer HTTPS so tokens and alert bodies are not sent in the clear", "notifier", u.name)
		}
	}

	if err := serve(ctx, cfg, log); err != nil {
		log.Error("tailwatch exited with error", "err", err)
		fmt.Fprintf(stderr, "tailwatch: %v\n", err)
		return exitFatal
	}
	log.Info("tailwatch stopped")
	return exitOK
}

// newLogger builds the process logger from the configuration.
func newLogger(w io.Writer, cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.SlogLevel()}
	var h slog.Handler
	if cfg.LogJSON {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// serve wires the store, sources, collector, alert engine and HTTP server
// together and runs them until ctx is cancelled or one of them fails.
func serve(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	st, err := store.Open(ctx, cfg.DBPath(), log.With("component", "store"))
	if err != nil {
		return fmt.Errorf("open store %q: %w", cfg.DBPath(), err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Warn("closing store", "err", err)
		}
	}()

	local, api, agents, err := buildSources(ctx, cfg, st, log)
	if err != nil {
		return err
	}

	col := collector.New(collector.Config{
		PollInterval:     cfg.PollInterval,
		APIInterval:      cfg.APIInterval,
		PingInterval:     cfg.PingInterval,
		AgentTimeout:     cfg.AgentTimeout,
		PingConcurrency:  cfg.PingConcurrency,
		AgentConcurrency: cfg.AgentConcurrency,
		AgentPort:        cfg.AgentPort,
		AgentEnabled:     cfg.AgentEnabled && agents != nil,
		RawRetention:     cfg.RawRetention,
		RollupRetention:  cfg.RollupRetention,
		EventRetention:   cfg.EventRetention,
		Version:          version.Version,
		DemoMode:         cfg.Demo,
		AdminActions:     cfg.EnableAdminActions && api != nil && api.Configured(),
	}, st, local, api, agents, log.With("component", "collector"))

	engine := alerts.NewEngine(st, col, buildNotifiers(cfg), log.With("component", "alerts"))

	ui, err := web.FS()
	if err != nil {
		return fmt.Errorf("embedded ui: %w", err)
	}
	if !web.Built() {
		log.Warn("no web UI is embedded in this binary; run 'make web' before building the hub to include it")
	}

	srv, err := httpapi.NewServer(httpapi.Deps{
		Cfg:       cfg,
		Store:     st,
		Collector: col,
		Alerts:    engine,
		API:       api,
		Local:     local,
		UI:        ui,
		Log:       log.With("component", "http"),
		Version:   version.Version,
	})
	if err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	col.Emit(ctx, model.Event{
		Type:     model.EventHubStarted,
		Severity: model.SeverityInfo,
		Title:    "Tailwatch started",
		Message:  fmt.Sprintf("Version %s", version.Version),
		Data:     map[string]any{"version": version.Version, "demo": cfg.Demo},
	})

	// Run every long-lived component; the first failure (or ctx cancellation)
	// stops the rest. Each Run returns nil on a clean, ctx-driven shutdown.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		name string
		err  error
	}
	results := make(chan result, 3)
	var wg sync.WaitGroup
	start := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fn(runCtx)
			results <- result{name, err}
			cancel()
		}()
	}
	// The engine subscribes to collector ticks when it starts, so start it
	// before the collector's first poll.
	start("alerts", engine.Run)
	start("collector", col.Run)
	start("http", srv.ListenAndServe)

	// Wait for the first component to stop, then for the others to drain
	// within the grace period.
	first := <-results
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		log.Warn("components did not stop within the shutdown grace period")
	}

	var errs []error
	if first.err != nil && !errors.Is(first.err, context.Canceled) {
		errs = append(errs, fmt.Errorf("%s: %w", first.name, first.err))
	}
drain:
	for {
		select {
		case r := <-results:
			if r.err != nil && !errors.Is(r.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("%s: %w", r.name, r.err))
			}
		default:
			break drain
		}
	}
	return errors.Join(errs...)
}

// buildSources returns the local tailscaled, control API and agent sources,
// either real ones or the demo simulator.
func buildSources(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) (source.LocalSource, source.ControlAPI, source.AgentClient, error) {
	if cfg.Demo {
		sim := demo.New(demoSeed, log.With("component", "demo"))
		log.Info("demo mode: using a simulated tailnet; no real devices are contacted")
		if err := sim.Backfill(ctx, st, demoBackfill, cfg.PollInterval); err != nil {
			return nil, nil, nil, fmt.Errorf("demo backfill: %w", err)
		}
		var agents source.AgentClient
		if cfg.AgentEnabled {
			agents = sim
		}
		return sim, sim, agents, nil
	}

	local := tslocal.New(cfg.Socket, log.With("component", "tslocal"))

	var api source.ControlAPI
	if cfg.HasControlAPI() {
		api = tsapi.New(tsapi.Options{
			BaseURL:           cfg.APIBaseURL,
			Tailnet:           cfg.Tailnet,
			APIKey:            cfg.APIKey,
			OAuthClientID:     cfg.OAuthClientID,
			OAuthClientSecret: cfg.OAuthClientSecret,
			Version:           version.Version,
		}, log.With("component", "tsapi"))
	} else {
		log.Info("control API not configured; client versions, key expiry and admin actions are unavailable (set TS_API_KEY or TS_OAUTH_CLIENT_ID/SECRET)")
	}

	var agents source.AgentClient
	if cfg.AgentEnabled {
		agents = agentclient.New(cfg.AgentToken, cfg.AgentTimeout, log.With("component", "agentclient"))
	}
	return local, api, agents, nil
}

// buildNotifiers returns the configured alert notifiers. Each gets the
// alerts package's own HTTP client (nil here), which has a timeout and never
// follows redirects, so a payload or token is never re-sent to a host the
// endpoint redirects to.
func buildNotifiers(cfg *config.Config) []alerts.Notifier {
	var out []alerts.Notifier
	if cfg.WebhookURL != "" {
		out = append(out, alerts.NewWebhook(cfg.WebhookURL, cfg.WebhookSecret, nil))
	}
	if cfg.SlackWebhookURL != "" {
		out = append(out, alerts.NewSlack(cfg.SlackWebhookURL, nil))
	}
	if cfg.NtfyURL != "" {
		out = append(out, alerts.NewNtfy(cfg.NtfyURL, cfg.NtfyToken, nil))
	}
	return out
}
