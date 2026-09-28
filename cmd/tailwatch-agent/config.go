package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
)

const (
	// envPrefix is prepended to the upper-cased flag name to form the
	// environment fallback, e.g. --allow-tag -> TAILWATCH_AGENT_ALLOW_TAG.
	envPrefix = "TAILWATCH_AGENT_"

	// envToken is the only way to supply the shared token: never a flag, so
	// it does not appear in process listings.
	envToken = envPrefix + "TOKEN"

	// listenAuto selects the device's first Tailscale IPv4 address.
	listenAuto = "auto"

	defaultInterval = 5 * time.Second
	minInterval     = time.Second
	maxInterval     = time.Hour

	// minTokenLen is the length below which a token triggers a warning.
	minTokenLen = 16
)

// config is the agent's parsed, validated configuration.
type config struct {
	Listen            string
	Port              int
	Auth              authMode
	Token             string // from the environment only; never logged
	AllowUsers        []string
	AllowTags         []string
	AllowNodes        []string
	Interval          time.Duration
	Socket            string
	InsecureListenAny bool
	LogLevel          string
	LogJSON           bool
	ShowVersion       bool

	// Warnings are non-fatal findings to log once the logger exists.
	Warnings []string
}

// stringList is a repeatable flag; each occurrence may also hold a
// comma-separated list (used for the environment fallback).
type stringList []string

// String implements flag.Value.
func (l *stringList) String() string { return strings.Join(*l, ",") }

// Set implements flag.Value.
func (l *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// envName maps a flag name to its environment variable.
func envName(flagName string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// parseConfig parses command-line args with environment fallbacks
// (TAILWATCH_AGENT_<FLAG>); flags override the environment. Usage text is
// written to out. It returns flag.ErrHelp (wrapped) when help was requested.
func parseConfig(args []string, getenv func(string) string, out io.Writer) (*config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--token") || strings.HasPrefix(a, "-token") {
			return nil, fmt.Errorf("the shared token must be given via the %s environment variable, not as a flag", envToken)
		}
	}

	fs := flag.NewFlagSet("tailwatch-agent", flag.ContinueOnError)
	fs.SetOutput(out)

	cfg := &config{}
	var (
		listen    string
		port      int
		auth      string
		users     stringList
		tags      stringList
		nodes     stringList
		interval  time.Duration
		socket    string
		insecure  bool
		logLevel  string
		logJSON   bool
		version   bool
		envErrs   []error
		envString = func(name, def string) string {
			if v := strings.TrimSpace(getenv(envName(name))); v != "" {
				return v
			}
			return def
		}
		envInt = func(name string, def int) int {
			v := strings.TrimSpace(getenv(envName(name)))
			if v == "" {
				return def
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				envErrs = append(envErrs, fmt.Errorf("%s: %q is not an integer", envName(name), v))
				return def
			}
			return n
		}
		envBool = func(name string, def bool) bool {
			v := strings.TrimSpace(getenv(envName(name)))
			if v == "" {
				return def
			}
			b, err := strconv.ParseBool(v)
			if err != nil {
				envErrs = append(envErrs, fmt.Errorf("%s: %q is not a boolean", envName(name), v))
				return def
			}
			return b
		}
		envDuration = func(name string, def time.Duration) time.Duration {
			v := strings.TrimSpace(getenv(envName(name)))
			if v == "" {
				return def
			}
			d, err := parseDuration(v)
			if err != nil {
				envErrs = append(envErrs, fmt.Errorf("%s: %q is not a duration", envName(name), v))
				return def
			}
			return d
		}
	)
	if v := envString("allow-user", ""); v != "" {
		_ = users.Set(v)
	}
	if v := envString("allow-tag", ""); v != "" {
		_ = tags.Set(v)
	}
	if v := envString("allow-node", ""); v != "" {
		_ = nodes.Set(v)
	}

	fs.StringVar(&listen, "listen", envString("listen", listenAuto), "listen address: \"auto\" (first Tailscale IPv4 + --port) or host:port")
	fs.IntVar(&port, "port", envInt("port", agentproto.DefaultPort), "port used when --listen is auto")
	fs.StringVar(&auth, "auth", envString("auth", string(authWhois)), "authentication mode: whois, token or both (token via "+envToken+")")
	fs.Var(&users, "allow-user", "login name allowed to read metrics (repeatable)")
	fs.Var(&tags, "allow-tag", "tag (tag:...) allowed to read metrics (repeatable)")
	fs.Var(&nodes, "allow-node", "MagicDNS node name allowed to read metrics (repeatable)")
	fs.DurationVar(&interval, "interval", envDuration("interval", defaultInterval), "sampling interval")
	fs.StringVar(&socket, "socket", envString("socket", ""), "tailscaled LocalAPI socket path (default: platform default)")
	fs.BoolVar(&insecure, "insecure-listen-any", envBool("insecure-listen-any", false), "allow binding a non-Tailscale, non-loopback address (not recommended)")
	fs.StringVar(&logLevel, "log-level", envString("log-level", "info"), "log level: debug, info, warn or error")
	fs.BoolVar(&logJSON, "log-json", envBool("log-json", false), "log as JSON instead of text")
	fs.BoolVar(&version, "version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: tailwatch-agent [flags]\n\nServes system metrics to the Tailwatch hub over the tailnet.\nEvery flag may also be set via %s<FLAG> (upper-case, '-' -> '_');\nflags override the environment. The shared token is read from %s only.\n\nFlags:\n", envPrefix, envToken)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, fmt.Errorf("%w", flag.ErrHelp)
		}
		return nil, fmt.Errorf("parse flags: %w", err)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if len(envErrs) > 0 {
		return nil, errors.Join(envErrs...)
	}

	cfg.Listen = strings.TrimSpace(listen)
	cfg.Port = port
	cfg.Interval = interval
	cfg.Socket = strings.TrimSpace(socket)
	cfg.InsecureListenAny = insecure
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(logLevel))
	cfg.LogJSON = logJSON
	cfg.ShowVersion = version
	cfg.Token = strings.TrimSpace(getenv(envToken))
	cfg.AllowUsers = normalizeList(users, strings.ToLower)
	cfg.AllowTags = normalizeList(tags, strings.ToLower)
	cfg.AllowNodes = normalizeList(nodes, normalizeNodeName)

	mode, err := parseAuthMode(auth)
	if err != nil {
		return nil, err
	}
	cfg.Auth = mode

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate checks cross-field constraints and records non-fatal warnings.
func (c *config) validate() error {
	if c.Listen == "" {
		return errors.New("--listen must not be empty (use \"auto\" or host:port)")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("--port %d is out of range 1-65535", c.Port)
	}
	if c.Listen != listenAuto {
		if _, _, err := splitListen(c.Listen, c.Port); err != nil {
			return fmt.Errorf("--listen %q: %w", c.Listen, err)
		}
	}
	if c.Interval < minInterval || c.Interval > maxInterval {
		return fmt.Errorf("--interval %s must be between %s and %s", c.Interval, minInterval, maxInterval)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("--log-level %q must be debug, info, warn or error", c.LogLevel)
	}
	for _, t := range c.AllowTags {
		if !strings.HasPrefix(t, "tag:") {
			return fmt.Errorf("--allow-tag %q must start with \"tag:\"", t)
		}
	}
	if c.Auth.usesToken() {
		if c.Token == "" {
			return fmt.Errorf("--auth %s requires the shared token in the %s environment variable", c.Auth, envToken)
		}
		if len(c.Token) < minTokenLen {
			c.Warnings = append(c.Warnings, fmt.Sprintf("the shared token is shorter than %d characters; generate one with: openssl rand -hex 32", minTokenLen))
		}
	} else if c.Token != "" {
		c.Warnings = append(c.Warnings, envToken+" is set but --auth whois ignores it (use --auth token or --auth both)")
	}
	if !c.Auth.usesWhoIs() && (len(c.AllowUsers)+len(c.AllowTags)+len(c.AllowNodes)) > 0 {
		c.Warnings = append(c.Warnings, "--allow-user/--allow-tag/--allow-node are ignored with --auth token")
	}
	if c.InsecureListenAny {
		c.Warnings = append(c.Warnings, "--insecure-listen-any is set: the agent may be reachable from outside the tailnet")
	}
	return nil
}

// splitListen parses a non-auto listen value: "host:port", "[v6]:port" or a
// bare IP literal (which gets defaultPort). Host names other than
// "localhost" are refused so the bind target is always an explicit address.
func splitListen(listen string, defaultPort int) (host string, port int, err error) {
	h, p, splitErr := net.SplitHostPort(listen)
	if splitErr != nil {
		// Bare IP (v4, or v6 without brackets)?
		if ip := net.ParseIP(strings.Trim(listen, "[]")); ip != nil {
			return ip.String(), defaultPort, nil
		}
		return "", 0, errors.New("expected host:port or an IP literal")
	}
	if p == "" {
		port = defaultPort
	} else {
		port, err = strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port %q", p)
		}
	}
	h = strings.TrimSpace(h)
	if strings.EqualFold(h, "localhost") {
		h = "127.0.0.1"
	}
	if h == "" {
		return "", 0, errors.New("empty host (use auto or an explicit IP)")
	}
	if net.ParseIP(h) == nil {
		return "", 0, fmt.Errorf("host %q must be an IP literal (use auto or the device's Tailscale IP)", h)
	}
	return h, port, nil
}

// parseDuration accepts Go durations ("5s", "1m") and bare integers, which
// are read as seconds.
func parseDuration(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
}

// normalizeList trims, transforms, drops empties and deduplicates while
// keeping order.
func normalizeList(in []string, fn func(string) string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = fn(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// normalizeNodeName lower-cases a MagicDNS name and strips a trailing dot.
func normalizeNodeName(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}
