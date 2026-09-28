// Package config parses the Tailwatch hub configuration from command-line
// flags and environment variables.
//
// Every flag has an environment fallback named TAILWATCH_<FLAG> (upper-case,
// '-' replaced by '_'); flags override the environment. Secrets (the control
// API key or OAuth client, the agent token, the webhook secret and the ntfy
// token) are read from the environment only and are rejected as flags so
// they never appear in process listings. List-valued flags are repeatable and
// also accept comma-separated values. Durations accept Go syntax plus a day
// suffix ("7d").
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/evilgenius79/fable-tailscale/internal/agentproto"
	"github.com/evilgenius79/fable-tailscale/internal/model"
)

// Environment variable names.
const (
	// EnvPrefix is prepended to the upper-cased flag name to form the
	// environment fallback, e.g. --poll-interval -> TAILWATCH_POLL_INTERVAL.
	EnvPrefix = "TAILWATCH_"

	// EnvAPIKey holds a Tailscale API access token (env only).
	EnvAPIKey = "TS_API_KEY"
	// EnvOAuthClientID holds the OAuth client ID (env only).
	EnvOAuthClientID = "TS_OAUTH_CLIENT_ID"
	// EnvOAuthClientSecret holds the OAuth client secret (env only).
	EnvOAuthClientSecret = "TS_OAUTH_CLIENT_SECRET"
	// EnvAgentToken holds the shared token sent to tailwatch-agents (env only).
	EnvAgentToken = "TAILWATCH_AGENT_TOKEN"
	// EnvWebhookSecret holds the HMAC key for webhook signatures (env only).
	EnvWebhookSecret = "TAILWATCH_WEBHOOK_SECRET"
	// EnvNtfyToken holds the ntfy access token (env only).
	EnvNtfyToken = "TAILWATCH_NTFY_TOKEN"
)

// Defaults.
const (
	// ListenAuto selects the hub's first Tailscale IPv4 address.
	ListenAuto = "auto"
	// DefaultPort is the port used when --listen is auto.
	DefaultPort = 8484
	// DefaultDataDir is the default data directory.
	DefaultDataDir = "./data"
	// DefaultTailnet is the control API's "default tailnet of the key".
	DefaultTailnet = "-"
	// DefaultAPIBaseURL is the Tailscale control API base URL.
	DefaultAPIBaseURL = "https://api.tailscale.com"
	// DBFileName is the SQLite file name inside DataDir.
	DBFileName = "tailwatch.db"

	defaultPollInterval     = 15 * time.Second
	defaultAPIInterval      = 60 * time.Second
	defaultPingInterval     = 30 * time.Second
	defaultConcurrency      = 8
	defaultAgentTimeout     = 5 * time.Second
	defaultRawRetention     = 48 * time.Hour
	defaultRollupRetention  = 90 * 24 * time.Hour
	defaultEventRetention   = 90 * 24 * time.Hour
	defaultLogLevel         = "info"
	defaultViewers          = "*"
	minPollInterval         = 5 * time.Second
	maxPollInterval         = time.Hour
	minAPIInterval          = 30 * time.Second
	maxAPIInterval          = 24 * time.Hour
	minPingInterval         = 10 * time.Second
	maxPingInterval         = time.Hour
	minAgentTimeout         = time.Second
	maxAgentTimeout         = 2 * time.Minute
	maxConcurrency          = 256
	minRetention            = time.Hour
	authModeTailscale       = "tailscale"
	authModeNone            = "none"
	programName             = "tailwatch"
	secretFlagRejectMessage = "must be given via the %s environment variable, never as a flag (it would be visible in process listings)"
)

// secretFlags maps flag names that are refused on the command line to the
// environment variable that must be used instead.
var secretFlags = map[string]string{
	"api-key":             EnvAPIKey,
	"oauth-client-id":     EnvOAuthClientID,
	"oauth-client-secret": EnvOAuthClientSecret,
	"agent-token":         EnvAgentToken,
	"webhook-secret":      EnvWebhookSecret,
	"ntfy-token":          EnvNtfyToken,
}

// Config is the hub's parsed configuration. Secret fields are only ever
// populated from the environment and are excluded from Settings.
type Config struct {
	// Listen is "auto" (first Tailscale IPv4 + ":8484") or "host:port".
	Listen string
	// DataDir holds the SQLite database (DataDir/tailwatch.db); created 0700.
	DataDir string
	// Demo runs against the internal simulator with authentication disabled.
	Demo bool
	// InsecureNoAuth disables authentication; only honoured on a loopback
	// Listen address.
	InsecureNoAuth bool
	// InsecureListenAny allows binding a non-Tailscale, non-loopback address
	// such as 0.0.0.0. Not recommended.
	InsecureListenAny bool
	// Socket overrides the tailscaled LocalAPI socket path ("" = default).
	Socket string
	// AllowedHosts are extra Host header values ("host" or "host:port",
	// lower-cased, no trailing dot) the hub accepts in addition to its
	// listen address, Tailscale IPs and MagicDNS name, for reverse-proxy
	// or custom TLS names. An entry without a port matches any port.
	AllowedHosts []string

	// Admins are login names granted the admin role.
	Admins []string
	// AdminTags are "tag:..." values granting the admin role to tagged nodes.
	AdminTags []string
	// Viewers are login names granted the viewer role; "*" means any
	// tailnet identity.
	Viewers []string
	// ViewerTags are "tag:..." values granting the viewer role.
	ViewerTags []string
	// EnableAdminActions allows device admin actions through the API.
	EnableAdminActions bool

	// Tailnet is the control API tailnet name ("-" = the key's default).
	Tailnet string
	// APIKey is a Tailscale API access token (TS_API_KEY). Secret.
	APIKey string
	// OAuthClientID identifies an OAuth client (TS_OAUTH_CLIENT_ID). Secret.
	OAuthClientID string
	// OAuthClientSecret is the OAuth client secret (TS_OAUTH_CLIENT_SECRET). Secret.
	OAuthClientSecret string
	// APIBaseURL is the control API base URL.
	APIBaseURL string

	// PollInterval is the LocalAPI poll period (>= 5s).
	PollInterval time.Duration
	// APIInterval is the control API poll period (>= 30s).
	APIInterval time.Duration
	// PingInterval is the disco ping period; 0 disables pings.
	PingInterval time.Duration
	// PingConcurrency bounds concurrent pings.
	PingConcurrency int
	// AgentEnabled turns per-device agent collection on.
	AgentEnabled bool
	// AgentPort is the TCP port tailwatch-agents listen on.
	AgentPort int
	// AgentToken is the shared token sent to agents (TAILWATCH_AGENT_TOKEN). Secret.
	AgentToken string
	// AgentConcurrency bounds concurrent agent fetches.
	AgentConcurrency int
	// AgentTimeout bounds one agent fetch.
	AgentTimeout time.Duration

	// RawRetention is how long raw samples are kept.
	RawRetention time.Duration
	// RollupRetention is how long 5-minute rollups are kept.
	RollupRetention time.Duration
	// EventRetention is how long events and resolved alerts are kept.
	EventRetention time.Duration

	// WebhookURL receives generic JSON notifications.
	WebhookURL string
	// WebhookSecret signs webhook bodies (TAILWATCH_WEBHOOK_SECRET). Secret.
	WebhookSecret string
	// SlackWebhookURL receives Slack/Discord-compatible notifications.
	SlackWebhookURL string
	// NtfyURL is an ntfy topic URL.
	NtfyURL string
	// NtfyToken is the ntfy access token (TAILWATCH_NTFY_TOKEN). Secret.
	NtfyToken string

	// TLSCert and TLSKey are optional PEM files; when both are set the hub
	// serves HTTPS.
	TLSCert string
	TLSKey  string
	// LogLevel is debug, info, warn or error.
	LogLevel string
	// LogJSON switches the log format to JSON.
	LogJSON bool

	// ShowVersion is set by --version; the caller prints the version and exits.
	ShowVersion bool
}

// Load parses args (without the program name) on a fresh flag set, applying
// environment fallbacks first (TAILWATCH_<FLAG>, plus the secret-only
// variables) so that flags override the environment, and then validates the
// result. getenv may be nil. A request for help returns an error wrapping
// flag.ErrHelp; the caller prints Usage().
func Load(args []string, getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if err := rejectSecretFlags(args); err != nil {
		return nil, err
	}
	b := newBuilder(getenv, io.Discard)
	b.define()
	if err := b.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, flag.ErrHelp
		}
		return nil, fmt.Errorf("config: %w", err)
	}
	if b.fs.NArg() > 0 {
		return nil, fmt.Errorf("config: unexpected argument %q", b.fs.Arg(0))
	}
	if len(b.envErrs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(b.envErrs...))
	}
	cfg := b.build()
	if cfg.ShowVersion {
		return cfg, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Usage returns the grouped flag reference for the hub.
func Usage() string {
	var sb strings.Builder
	b := newBuilder(func(string) string { return "" }, &sb)
	b.define()
	b.printUsage()
	return sb.String()
}

// rejectSecretFlags fails when a secret is passed on the command line.
func rejectSecretFlags(args []string) error {
	for _, a := range args {
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if env, ok := secretFlags[name]; ok {
			return fmt.Errorf("config: --%s "+secretFlagRejectMessage, name, env)
		}
	}
	return nil
}

// --- flag builder -----------------------------------------------------------

// group is a section of the usage text.
type group struct {
	name  string
	flags []string
	// envOnly lists environment variables documented under the group.
	envOnly []string
}

// builder registers flags on a FlagSet with environment fallbacks and keeps
// the group structure for Usage.
type builder struct {
	fs      *flag.FlagSet
	getenv  func(string) string
	out     io.Writer
	envErrs []error
	groups  []*group
	lists   map[string]*listValue
}

func newBuilder(getenv func(string) string, out io.Writer) *builder {
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {} // the caller prints Usage()
	return &builder{fs: fs, getenv: getenv, out: out, lists: map[string]*listValue{}}
}

// EnvName maps a flag name to its environment variable.
func EnvName(flagName string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

func (b *builder) env(name string) string {
	return strings.TrimSpace(b.getenv(EnvName(name)))
}

func (b *builder) group(name string, envOnly ...string) *group {
	g := &group{name: name, envOnly: envOnly}
	b.groups = append(b.groups, g)
	return g
}

func (b *builder) str(g *group, name, def, usage string) {
	if v := b.env(name); v != "" {
		def = v
	}
	g.flags = append(g.flags, name)
	b.fs.String(name, def, usage)
}

func (b *builder) boolean(g *group, name string, def bool, usage string) {
	if v := b.env(name); v != "" {
		p, err := strconv.ParseBool(v)
		if err != nil {
			b.envErrs = append(b.envErrs, fmt.Errorf("%s: %q is not a boolean", EnvName(name), v))
		} else {
			def = p
		}
	}
	g.flags = append(g.flags, name)
	b.fs.Bool(name, def, usage)
}

func (b *builder) integer(g *group, name string, def int, usage string) {
	if v := b.env(name); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			b.envErrs = append(b.envErrs, fmt.Errorf("%s: %q is not an integer", EnvName(name), v))
		} else {
			def = p
		}
	}
	g.flags = append(g.flags, name)
	b.fs.Int(name, def, usage)
}

func (b *builder) duration(g *group, name string, def time.Duration, usage string) {
	if v := b.env(name); v != "" {
		p, err := ParseDuration(v)
		if err != nil {
			b.envErrs = append(b.envErrs, fmt.Errorf("%s: %q is not a duration", EnvName(name), v))
		} else {
			def = p
		}
	}
	g.flags = append(g.flags, name)
	d := durationValue(def)
	b.fs.Var(&d, name, usage)
}

func (b *builder) list(g *group, name, def, usage string) {
	if v := b.env(name); v != "" {
		def = v
	}
	l := &listValue{}
	_ = l.Set(def)
	l.set = false
	g.flags = append(g.flags, name)
	b.fs.Var(l, name, usage)
	b.lists[name] = l
}

// define registers every flag with its environment fallback as default.
func (b *builder) define() {
	server := b.group("Server")
	b.str(server, "listen", ListenAuto, "listen address: \"auto\" (first Tailscale IPv4 + :8484) or host:port")
	b.str(server, "data-dir", DefaultDataDir, "data directory holding the SQLite database (created 0700)")
	b.str(server, "tls-cert", "", "PEM certificate file; with --tls-key serves HTTPS")
	b.str(server, "tls-key", "", "PEM private key file")
	b.str(server, "socket", "", "tailscaled LocalAPI socket path (default: platform default)")
	b.boolean(server, "demo", false, "run against a simulated tailnet with authentication disabled")
	b.boolean(server, "insecure-no-auth", false, "disable authentication (only allowed with a loopback --listen)")
	b.boolean(server, "insecure-listen-any", false, "allow binding a non-Tailscale, non-loopback address (not recommended)")
	b.list(server, "allowed-hosts", "", "`list` of extra Host header values (host or host:port) the hub answers to, e.g. a reverse-proxy or TLS name; the listen address, Tailscale IPs and MagicDNS name are always accepted")

	access := b.group("Access control")
	b.list(access, "admins", "", "login names granted the admin role (repeatable `list`, comma-separated)")
	b.list(access, "admin-tags", "", "`list` of tags (tag:...) granting the admin role to tagged nodes")
	b.list(access, "viewers", defaultViewers, "`list` of login names granted the viewer role; \"*\" = any tailnet identity")
	b.list(access, "viewer-tags", "", "`list` of tags (tag:...) granting the viewer role")
	b.boolean(access, "enable-admin-actions", false, "allow device admin actions (requires control API credentials)")

	api := b.group("Tailscale control API", EnvAPIKey, EnvOAuthClientID, EnvOAuthClientSecret)
	b.str(api, "tailnet", DefaultTailnet, "tailnet name for the control API (\"-\" = the key's default tailnet)")
	b.str(api, "api-base-url", DefaultAPIBaseURL, "control API base URL")

	collection := b.group("Collection", EnvAgentToken)
	b.duration(collection, "poll-interval", defaultPollInterval, "tailscaled poll interval as a `duration` (>= 5s)")
	b.duration(collection, "api-interval", defaultAPIInterval, "control API poll interval as a `duration` (>= 30s)")
	b.duration(collection, "ping-interval", defaultPingInterval, "disco ping interval as a `duration` (>= 10s; 0 disables pings)")
	b.integer(collection, "ping-concurrency", defaultConcurrency, "maximum concurrent pings")
	b.boolean(collection, "agent-enabled", true, "collect system metrics from tailwatch-agents")
	b.integer(collection, "agent-port", agentproto.DefaultPort, "TCP port tailwatch-agents listen on")
	b.integer(collection, "agent-concurrency", defaultConcurrency, "maximum concurrent agent fetches")
	b.duration(collection, "agent-timeout", defaultAgentTimeout, "timeout of one agent fetch as a `duration`")

	retention := b.group("Retention")
	b.duration(retention, "raw-retention", defaultRawRetention, "how long raw samples are kept, as a `duration` (>= 1h)")
	b.duration(retention, "rollup-retention", defaultRollupRetention, "how long 5-minute rollups are kept, as a `duration` (>= 1h)")
	b.duration(retention, "event-retention", defaultEventRetention, "how long events and resolved alerts are kept, as a `duration` (>= 1h)")

	notify := b.group("Notifications", EnvWebhookSecret, EnvNtfyToken)
	b.str(notify, "webhook-url", "", "generic JSON webhook URL (signed when "+EnvWebhookSecret+" is set)")
	b.str(notify, "slack-webhook-url", "", "Slack/Discord-compatible incoming webhook URL")
	b.str(notify, "ntfy-url", "", "ntfy topic URL, e.g. https://ntfy.sh/mytopic")

	logging := b.group("Logging")
	b.str(logging, "log-level", defaultLogLevel, "log level: debug, info, warn or error")
	b.boolean(logging, "log-json", false, "log as JSON instead of text")

	other := b.group("Other")
	b.boolean(other, "version", false, "print the version and exit")
}

// build assembles the Config from the parsed flags and the secret-only
// environment variables, trimming and normalizing values.
func (b *builder) build() *Config {
	cfg := &Config{}
	get := func(name string) flag.Getter {
		f := b.fs.Lookup(name)
		if f == nil {
			panic("config: unknown flag " + name) // programming error, never input-dependent
		}
		return f.Value.(flag.Getter)
	}
	str := func(name string) string { return strings.TrimSpace(get(name).Get().(string)) }
	boolean := func(name string) bool { return get(name).Get().(bool) }
	integer := func(name string) int { return get(name).Get().(int) }
	duration := func(name string) time.Duration { return get(name).Get().(time.Duration) }

	cfg.Listen = str("listen")
	cfg.DataDir = str("data-dir")
	cfg.TLSCert = str("tls-cert")
	cfg.TLSKey = str("tls-key")
	cfg.Socket = str("socket")
	cfg.Demo = boolean("demo")
	cfg.InsecureNoAuth = boolean("insecure-no-auth")
	cfg.InsecureListenAny = boolean("insecure-listen-any")
	cfg.AllowedHosts = normalizeList(b.lists["allowed-hosts"].vals, NormalizeHost)

	cfg.Admins = normalizeList(b.lists["admins"].vals, strings.ToLower)
	cfg.AdminTags = normalizeList(b.lists["admin-tags"].vals, strings.ToLower)
	cfg.Viewers = normalizeList(b.lists["viewers"].vals, strings.ToLower)
	cfg.ViewerTags = normalizeList(b.lists["viewer-tags"].vals, strings.ToLower)
	cfg.EnableAdminActions = boolean("enable-admin-actions")

	cfg.Tailnet = str("tailnet")
	cfg.APIBaseURL = strings.TrimRight(str("api-base-url"), "/")
	cfg.APIKey = strings.TrimSpace(b.getenv(EnvAPIKey))
	cfg.OAuthClientID = strings.TrimSpace(b.getenv(EnvOAuthClientID))
	cfg.OAuthClientSecret = strings.TrimSpace(b.getenv(EnvOAuthClientSecret))

	cfg.PollInterval = duration("poll-interval")
	cfg.APIInterval = duration("api-interval")
	cfg.PingInterval = duration("ping-interval")
	cfg.PingConcurrency = integer("ping-concurrency")
	cfg.AgentEnabled = boolean("agent-enabled")
	cfg.AgentPort = integer("agent-port")
	cfg.AgentToken = strings.TrimSpace(b.getenv(EnvAgentToken))
	cfg.AgentConcurrency = integer("agent-concurrency")
	cfg.AgentTimeout = duration("agent-timeout")

	cfg.RawRetention = duration("raw-retention")
	cfg.RollupRetention = duration("rollup-retention")
	cfg.EventRetention = duration("event-retention")

	cfg.WebhookURL = str("webhook-url")
	cfg.WebhookSecret = strings.TrimSpace(b.getenv(EnvWebhookSecret))
	cfg.SlackWebhookURL = str("slack-webhook-url")
	cfg.NtfyURL = str("ntfy-url")
	cfg.NtfyToken = strings.TrimSpace(b.getenv(EnvNtfyToken))

	cfg.LogLevel = strings.ToLower(str("log-level"))
	cfg.LogJSON = boolean("log-json")
	cfg.ShowVersion = boolean("version")
	return cfg
}

// printUsage writes the grouped flag reference to b.out.
func (b *builder) printUsage() {
	w := b.out
	fmt.Fprintf(w, "Usage: %s [flags]\n\n", programName)
	fmt.Fprintf(w, "Tailwatch hub: watches a tailnet and serves the dashboard over it.\n\n")
	fmt.Fprintf(w, "Every flag may also be set via %s<FLAG> (upper-case, '-' -> '_');\n", EnvPrefix)
	fmt.Fprintf(w, "flags override the environment. List flags are repeatable and accept\n")
	fmt.Fprintf(w, "comma-separated values. Durations accept Go syntax (15s, 1h30m) plus a\n")
	fmt.Fprintf(w, "day suffix (7d). Secrets are read from the environment only.\n")
	for _, g := range b.groups {
		fmt.Fprintf(w, "\n%s:\n", g.name)
		for _, name := range g.flags {
			f := b.fs.Lookup(name)
			if f == nil {
				continue
			}
			typ, usage := flag.UnquoteUsage(f)
			line := "  --" + f.Name
			if typ != "" {
				line += " " + typ
			}
			fmt.Fprintf(w, "%s\n    \t%s", line, usage)
			if def := f.DefValue; def != "" && def != "false" && def != "0" && def != "0s" {
				fmt.Fprintf(w, " (default %s)", quoteDefault(f, def))
			}
			fmt.Fprintf(w, "\n    \tenv %s\n", EnvName(f.Name))
		}
		for _, env := range g.envOnly {
			fmt.Fprintf(w, "  env %s\n    \t%s\n", env, envOnlyUsage[env])
		}
	}
}

// envOnlyUsage documents the secret-only environment variables.
var envOnlyUsage = map[string]string{
	EnvAPIKey:            "Tailscale API access token (tskey-api-...)",
	EnvOAuthClientID:     "OAuth client ID (with " + EnvOAuthClientSecret + ")",
	EnvOAuthClientSecret: "OAuth client secret",
	EnvAgentToken:        "shared token sent to tailwatch-agents (X-Tailwatch-Token)",
	EnvWebhookSecret:     "HMAC-SHA256 key used to sign generic webhook bodies",
	EnvNtfyToken:         "ntfy access token",
}

func quoteDefault(f *flag.Flag, def string) string {
	if _, ok := f.Value.(*listValue); ok {
		return strconv.Quote(def)
	}
	if g, ok := f.Value.(flag.Getter); ok {
		if _, isStr := g.Get().(string); isStr {
			return strconv.Quote(def)
		}
	}
	return def
}

// --- flag value types --------------------------------------------------------

// listValue is a repeatable flag that also splits comma-separated values.
// The first command-line occurrence replaces the environment-provided
// default so flags override the environment.
type listValue struct {
	vals []string
	set  bool
}

// String implements flag.Value.
func (l *listValue) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(l.vals, ",")
}

// Set implements flag.Value.
func (l *listValue) Set(v string) error {
	if !l.set {
		l.vals = nil
		l.set = true
	}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			l.vals = append(l.vals, p)
		}
	}
	return nil
}

// Get implements flag.Getter.
func (l *listValue) Get() any { return slices.Clone(l.vals) }

// durationValue is a flag.Value accepting Go durations and a day suffix.
type durationValue time.Duration

// String implements flag.Value.
func (d *durationValue) String() string {
	if d == nil {
		return "0s"
	}
	return FormatDuration(time.Duration(*d))
}

// Set implements flag.Value.
func (d *durationValue) Set(s string) error {
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = durationValue(v)
	return nil
}

// Get implements flag.Getter.
func (d *durationValue) Get() any { return time.Duration(*d) }

// ParseDuration parses a Go duration ("90s", "1h30m") or a whole number of
// days with a "d" suffix ("7d").
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil || n < 0 || n > 100_000 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

// FormatDuration renders whole days as "Nd" and everything else in Go syntax.
func FormatDuration(d time.Duration) string {
	if d >= 24*time.Hour && d%(24*time.Hour) == 0 {
		return strconv.FormatInt(int64(d/(24*time.Hour)), 10) + "d"
	}
	return d.String()
}

// normalizeList trims, transforms, drops empty entries and de-duplicates
// while preserving order. The result is never nil.
func normalizeList(in []string, transform func(string) string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if transform != nil {
			s = transform(s)
		}
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// --- validation ---------------------------------------------------------------

// Validate checks bounds and cross-field constraints and returns every
// problem found joined into one error.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch {
	case c.Listen == "":
		add("--listen must not be empty (use \"auto\" or host:port)")
	case c.Listen != ListenAuto:
		if _, _, err := SplitListen(c.Listen); err != nil {
			add("--listen %q: %v", c.Listen, err)
		}
	}
	if c.InsecureNoAuth && !IsLoopbackListen(c.Listen) {
		add("--insecure-no-auth is only allowed with a loopback --listen address such as 127.0.0.1:%d", DefaultPort)
	}
	for _, h := range c.AllowedHosts {
		if _, _, err := SplitHostOptionalPort(h); err != nil {
			add("--allowed-hosts %q: %v", h, err)
		}
	}
	if c.DataDir == "" {
		add("--data-dir must not be empty")
	}
	hasCert, hasKey := c.TLSCert != "", c.TLSKey != ""
	switch {
	case hasCert && !hasKey:
		add("--tls-cert requires --tls-key")
	case hasKey && !hasCert:
		add("--tls-key requires --tls-cert")
	case hasCert && hasKey:
		for _, f := range []struct{ flag, path string }{{"--tls-cert", c.TLSCert}, {"--tls-key", c.TLSKey}} {
			fi, err := os.Stat(f.path)
			switch {
			case err != nil:
				add("%s %q: %v", f.flag, f.path, err)
			case fi.IsDir():
				add("%s %q is a directory", f.flag, f.path)
			}
		}
	}

	if slices.Contains(c.Admins, "*") {
		add("--admins does not accept \"*\"; list admin login names explicitly")
	}
	for _, t := range c.AdminTags {
		if !strings.HasPrefix(t, "tag:") {
			add("--admin-tags %q must start with \"tag:\"", t)
		}
	}
	for _, t := range c.ViewerTags {
		if !strings.HasPrefix(t, "tag:") {
			add("--viewer-tags %q must start with \"tag:\"", t)
		}
	}

	if c.Tailnet == "" {
		add("--tailnet must not be empty (use \"-\" for the key's default tailnet)")
	}
	if err := validateURL(c.APIBaseURL); err != nil {
		add("--api-base-url: %v", err)
	}
	if (c.OAuthClientID != "") != (c.OAuthClientSecret != "") {
		add("%s and %s must be set together", EnvOAuthClientID, EnvOAuthClientSecret)
	}
	if c.EnableAdminActions && !c.Demo && !c.HasControlAPI() {
		add("--enable-admin-actions requires control API credentials (%s, or %s and %s)", EnvAPIKey, EnvOAuthClientID, EnvOAuthClientSecret)
	}

	if c.PollInterval < minPollInterval || c.PollInterval > maxPollInterval {
		add("--poll-interval %s must be between %s and %s", c.PollInterval, minPollInterval, maxPollInterval)
	}
	if c.APIInterval < minAPIInterval || c.APIInterval > maxAPIInterval {
		add("--api-interval %s must be between %s and %s", c.APIInterval, minAPIInterval, maxAPIInterval)
	}
	if c.PingInterval != 0 && (c.PingInterval < minPingInterval || c.PingInterval > maxPingInterval) {
		add("--ping-interval %s must be 0 (disabled) or between %s and %s", c.PingInterval, minPingInterval, maxPingInterval)
	}
	if c.PingConcurrency < 1 || c.PingConcurrency > maxConcurrency {
		add("--ping-concurrency %d must be between 1 and %d", c.PingConcurrency, maxConcurrency)
	}
	if c.AgentPort < 1 || c.AgentPort > 65535 {
		add("--agent-port %d must be between 1 and 65535", c.AgentPort)
	}
	if c.AgentConcurrency < 1 || c.AgentConcurrency > maxConcurrency {
		add("--agent-concurrency %d must be between 1 and %d", c.AgentConcurrency, maxConcurrency)
	}
	if c.AgentTimeout < minAgentTimeout || c.AgentTimeout > maxAgentTimeout {
		add("--agent-timeout %s must be between %s and %s", c.AgentTimeout, minAgentTimeout, maxAgentTimeout)
	}

	for _, r := range []struct {
		flag string
		d    time.Duration
	}{{"--raw-retention", c.RawRetention}, {"--rollup-retention", c.RollupRetention}, {"--event-retention", c.EventRetention}} {
		if r.d < minRetention {
			add("%s %s must be at least %s", r.flag, FormatDuration(r.d), FormatDuration(minRetention))
		}
	}

	for _, u := range []struct{ flag, url string }{{"--webhook-url", c.WebhookURL}, {"--slack-webhook-url", c.SlackWebhookURL}, {"--ntfy-url", c.NtfyURL}} {
		if u.url == "" {
			continue
		}
		if err := validateURL(u.url); err != nil {
			add("%s: %v", u.flag, err)
		}
	}

	if _, err := ParseLogLevel(c.LogLevel); err != nil {
		add("--log-level: %v", err)
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("config: %w", errors.Join(errs...))
}

// validateURL requires an absolute http(s) URL with a host.
func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host")
	}
	return nil
}

// SplitListen splits a "host:port" listen address and validates the port.
// The host may be an IP literal (IPv6 in brackets) or a hostname.
func SplitListen(listen string) (host string, port int, err error) {
	host, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, errors.New("must be host:port")
	}
	if host == "" {
		return "", 0, errors.New("host must not be empty (use 127.0.0.1 or the Tailscale IP)")
	}
	port, err = strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("port %q must be between 1 and 65535", portStr)
	}
	return host, port, nil
}

// SplitHostOptionalPort splits "host", "host:port", "[v6]" or "[v6]:port"
// into its host and port ("" when absent). The host must not be empty or
// carry a scheme or path, and a port must be between 1 and 65535.
func SplitHostOptionalPort(s string) (host, port string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("must not be empty")
	}
	if strings.ContainsAny(s, "/ \t?#@") {
		return "", "", errors.New("must be host or host:port without a scheme or path")
	}
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return "", "", errors.New("unterminated IPv6 literal")
		}
		host = s[1:end]
		rest := s[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", "", errors.New("unexpected text after the IPv6 literal")
			}
			port = rest[1:]
		}
		if ip, perr := netip.ParseAddr(host); perr != nil || !ip.Is6() {
			return "", "", errors.New("invalid IPv6 literal")
		}
	case strings.Count(s, ":") > 1:
		return "", "", errors.New("an IPv6 literal must be bracketed, e.g. [fd7a::1]:8484")
	default:
		host, port, _ = strings.Cut(s, ":")
	}
	if host == "" {
		return "", "", errors.New("host must not be empty")
	}
	if port != "" {
		if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 {
			return "", "", fmt.Errorf("port %q must be between 1 and 65535", port)
		}
	}
	return host, port, nil
}

// NormalizeHost lower-cases a "host" or "host:port" value and strips a
// trailing dot from the name so equal hosts compare equal. Invalid values
// are returned trimmed and lower-cased only (Validate reports them).
func NormalizeHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	host, port, err := SplitHostOptionalPort(s)
	if err != nil {
		return s
	}
	if ip, perr := netip.ParseAddr(host); perr == nil {
		host = ip.String()
		if ip.Is6() {
			host = "[" + host + "]"
		}
	} else {
		host = strings.TrimSuffix(host, ".")
	}
	if port != "" {
		return host + ":" + port
	}
	return host
}

// IsLoopbackListen reports whether listen names a loopback address
// ("localhost" or a loopback IP literal) with a port.
func IsLoopbackListen(listen string) bool {
	host, _, err := SplitListen(listen)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}

// ParseLogLevel maps a level name to slog.Level ("warning" is accepted for
// "warn").
func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("%q must be debug, info, warn or error", s)
}

// --- derived values -----------------------------------------------------------

// HasControlAPI reports whether control API credentials are configured.
func (c *Config) HasControlAPI() bool {
	return c.APIKey != "" || (c.OAuthClientID != "" && c.OAuthClientSecret != "")
}

// AuthMode returns "none" when authentication is disabled (demo or
// --insecure-no-auth) and "tailscale" otherwise.
func (c *Config) AuthMode() string {
	if c.Demo || c.InsecureNoAuth {
		return authModeNone
	}
	return authModeTailscale
}

// Notifiers lists the configured notifier kinds ("webhook", "slack",
// "ntfy"). The result is never nil.
func (c *Config) Notifiers() []string {
	out := []string{}
	if c.WebhookURL != "" {
		out = append(out, "webhook")
	}
	if c.SlackWebhookURL != "" {
		out = append(out, "slack")
	}
	if c.NtfyURL != "" {
		out = append(out, "ntfy")
	}
	return out
}

// DBPath returns the SQLite database path inside DataDir.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, DBFileName)
}

// SlogLevel returns the configured log level (info when unparsable).
func (c *Config) SlogLevel() slog.Level {
	lvl, err := ParseLogLevel(c.LogLevel)
	if err != nil {
		return slog.LevelInfo
	}
	return lvl
}

// Settings returns the non-secret projection of the configuration for the
// UI. Credentials, tokens and secrets are never included; list fields and
// Notifiers are never nil.
func (c *Config) Settings(stats model.StoreStats) model.Settings {
	return model.Settings{
		AuthMode:            c.AuthMode(),
		AdminUsers:          cloneOrEmpty(c.Admins),
		AdminTags:           cloneOrEmpty(c.AdminTags),
		ViewerUsers:         cloneOrEmpty(c.Viewers),
		ViewerTags:          cloneOrEmpty(c.ViewerTags),
		AdminActions:        c.EnableAdminActions,
		ControlAPI:          c.Demo || c.HasControlAPI(),
		Tailnet:             c.Tailnet,
		PollIntervalSec:     int(c.PollInterval / time.Second),
		APIIntervalSec:      int(c.APIInterval / time.Second),
		PingIntervalSec:     int(c.PingInterval / time.Second),
		AgentPort:           c.AgentPort,
		AgentEnabled:        c.AgentEnabled,
		RawRetentionHours:   int(c.RawRetention / time.Hour),
		RollupRetentionDays: int(c.RollupRetention / (24 * time.Hour)),
		EventRetentionDays:  int(c.EventRetention / (24 * time.Hour)),
		Notifiers:           c.Notifiers(),
		Listen:              c.Listen,
		DemoMode:            c.Demo,
		Store:               stats,
	}
}

func cloneOrEmpty(s []string) []string {
	if len(s) == 0 {
		return []string{}
	}
	return slices.Clone(s)
}
