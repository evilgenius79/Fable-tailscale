# Tailwatch architecture & package contracts

Tailwatch is two Go binaries and one embedded web UI:

* `tailwatch` (hub) — runs on one tailnet node. Polls the local `tailscaled`
  LocalAPI, optionally the Tailscale control API, and `tailwatch-agent`
  instances; stores time series in SQLite; evaluates watchdog rules; serves the
  UI + API over the tailnet with Tailscale-identity auth.
* `tailwatch-agent` — tiny per-device daemon exposing system metrics (CPU,
  memory, disk, network counters, temperatures, uptime) over the tailnet,
  authenticated by Tailscale identity (WhoIs) and/or a shared token.

```
                 ┌───────────────── hub node ───────────────────┐
 browser ──WhoIs─▶ httpapi ──▶ store (SQLite) ◀── collector ◀──▶ tslocal (LocalAPI)
 (tailnet)       │    ▲             ▲              │   │  └─────▶ tsapi (api.tailscale.com)
                 │    │ SSE         │              │   └────────▶ agentclient ──▶ agents on peers
                 │  bus ◀── alerts ◀┘ ticks ◀──────┘
                 └──────────────────────────────────────────────┘
```

## Module map (owner = the package that must be built)

| Package | Responsibility |
|---|---|
| `internal/model` | Shared domain + API types. **Frozen contract.** |
| `internal/agentproto` | Agent wire types. **Frozen contract.** |
| `internal/source` | Interfaces `LocalSource`, `ControlAPI`, `AgentClient`. **Frozen contract.** |
| `internal/bus` | Generic pub/sub. Done. |
| `internal/config` | Flag/env parsing for the hub (`Config` struct below). |
| `internal/tslocal` | `source.LocalSource` using `tailscale.com/client/local`. |
| `internal/tsapi` | `source.ControlAPI` using net/http (API key or OAuth client credentials). |
| `internal/agentclient` | `source.AgentClient` (HTTP, timeouts, size limits, token). |
| `internal/sysmetrics` | gopsutil sampler producing `agentproto.Report` (used by agent). |
| `internal/store` | SQLite persistence (modernc.org/sqlite, pure Go). |
| `internal/collector` | Poll loop, merge, rates, uptime, events, in-memory snapshot. |
| `internal/alerts` | Rule engine + notifiers. |
| `internal/demo` | Simulated `LocalSource`/`ControlAPI`/`AgentClient` + history backfill. |
| `internal/httpapi` | HTTP server, auth, security middleware, REST, SSE, embedded UI. |
| `cmd/tailwatch` | Hub main: wires everything. |
| `cmd/tailwatch-agent` | Agent main: sysmetrics sampler + hardened HTTP listener. |
| `web/` | Vite + React + TypeScript + Tailwind UI, built to `web/dist`, embedded by `web/embed.go`. |

Rules for all packages:

* Go 1.26, `log/slog` for logging (accept a `*slog.Logger`), `context` everywhere.
* Never log secrets (API keys, OAuth secrets, tokens, webhook URLs with tokens).
* Every exported function documented; table-driven tests with `testing` only (no testify).
* Do not run `go mod tidy` (integration does). Use `go build ./internal/<pkg>/... && go vet ./internal/<pkg>/... && go test ./internal/<pkg>/...`.

## Contracts

### `internal/config`

```go
type Config struct {
    Listen           string        // default "auto" → first Tailscale IPv4 + ":8484"; or "host:port"
    DataDir          string        // default "./data" (0700). DB at DataDir/tailwatch.db
    Demo             bool          // --demo: use internal/demo sources, auth none
    InsecureNoAuth   bool          // --insecure-no-auth (only honoured when Listen is loopback)
    Socket           string        // tailscaled socket path override ("" = platform default)

    Admins           []string      // login names
    AdminTags        []string      // "tag:..." values
    Viewers          []string      // default ["*"]
    ViewerTags       []string
    EnableAdminActions bool

    Tailnet          string        // "-" = default tailnet of the key
    APIKey           string        // env TS_API_KEY
    OAuthClientID    string        // env TS_OAUTH_CLIENT_ID
    OAuthClientSecret string       // env TS_OAUTH_CLIENT_SECRET
    APIBaseURL       string        // default https://api.tailscale.com

    PollInterval     time.Duration // default 15s (min 5s)
    APIInterval      time.Duration // default 60s
    PingInterval     time.Duration // default 30s; 0 disables pings
    PingConcurrency  int           // default 8
    AgentEnabled     bool          // default true
    AgentPort        int           // default agentproto.DefaultPort
    AgentToken       string        // env TAILWATCH_AGENT_TOKEN
    AgentConcurrency int           // default 8
    AgentTimeout     time.Duration // default 5s

    RawRetention     time.Duration // default 48h
    RollupRetention  time.Duration // default 90*24h
    EventRetention   time.Duration // default 90*24h

    WebhookURL, WebhookSecret, SlackWebhookURL, NtfyURL, NtfyToken string

    TLSCert, TLSKey  string        // optional PEM files; if set, serve HTTPS
    LogLevel         string        // debug|info|warn|error
    LogJSON          bool
}
func Load(args []string, getenv func(string) string) (*Config, error) // flags override env; env names TAILWATCH_* (e.g. TAILWATCH_LISTEN) plus TS_API_KEY/TS_OAUTH_CLIENT_ID/TS_OAUTH_CLIENT_SECRET/TAILWATCH_AGENT_TOKEN
func (c *Config) Validate() error
func (c *Config) Settings(store model.StoreStats) model.Settings // non-secret projection
```

### `internal/tslocal`

```go
func New(socketPath string, log *slog.Logger) *Client   // implements source.LocalSource
func (c *Client) Status(ctx) (*source.LocalStatus, error)
func (c *Client) WhoIs(ctx, remoteAddr string) (*source.WhoIs, error)
func (c *Client) Ping(ctx, ip string, timeout time.Duration) (*source.PingReply, error)
```
Mapping notes: `PeerStatus.ID` → `DeviceID`; `DNSName` strip trailing dot; user via
`Status.User[peer.UserID].LoginName`; `Tags.AsSlice()`; `PrimaryRoutes.AsSlice()`
stringified; `PingResult.LatencySeconds*1000`; direct iff `Endpoint != ""` and
`DERPRegionID == 0`. WhoIs tagged nodes: `LoginName = "tagged-device"`, IsTagged true.

### `internal/tsapi`

```go
type Options struct { BaseURL, Tailnet, APIKey, OAuthClientID, OAuthClientSecret string; HTTPClient *http.Client }
func New(o Options, log *slog.Logger) *Client            // implements source.ControlAPI
```
Endpoints: `GET /api/v2/tailnet/{tailnet}/devices?fields=all`;
`POST /api/v2/oauth/token` (client_credentials, cache until expiry-60s);
`POST /api/v2/device/{id}/authorized {"authorized":bool}`;
`POST /api/v2/device/{id}/tags {"tags":[]}`; `POST /api/v2/device/{id}/key {"keyExpiryDisabled":bool}`;
`POST /api/v2/device/{id}/routes {"routes":[]}`; `POST /api/v2/device/{id}/name {"name":""}`; `DELETE /api/v2/device/{id}`.
Retry once on 429/5xx with backoff, respect `Retry-After`. Errors wrap `source.ErrNotFound` on 404.
Device JSON: `addresses, id, nodeId, user, name, hostname, clientVersion, updateAvailable, os, created, lastSeen, keyExpiryDisabled, expires, authorized, isExternal, tags, advertisedRoutes, enabledRoutes, blocksIncomingConnections, tailnetLockError, clientConnectivity{endpoints, mappingVariesByDestIP, latency{<Region>:{preferred,latencyMs}}, clientSupports{hairPinning,ipv6,pcp,pmp,udp,upnp}}`.
DERP region names in `latency` are like "New York City"; map to codes via the
public DERP region list (nyc, sfo, ord, dfw, sea, lax, mia, den, hnl, tor, lhr,
fra, ams, par, mad, waw, hkg, sin, tok, syd, sao, jnb, blr, nue, dxb, nai, iad,
sao). Unknown names: lowercase, spaces → `-`.

### `internal/agentclient`

```go
func New(token string, timeout time.Duration, log *slog.Logger) *Client // implements source.AgentClient
```
Limits: 1 MiB body, `Accept: application/json`, rejects wrong protocol major.

### `internal/sysmetrics`

```go
type Sampler struct{ ... }
func NewSampler(interval time.Duration, agentVersion string, log *slog.Logger) *Sampler
func (s *Sampler) Run(ctx)                         // background loop
func (s *Sampler) Latest() (agentproto.Report, bool) // copy of latest report
func (s *Sampler) SetTailscaleInfo(fn func(ctx) (*agentproto.TailscaleInfo, error))
```
Physical interface heuristic: not loopback, not `tailscale*`/`utun*`/`wg*`/`docker*`/`br-*`/`veth*`/`virbr*`/`lo*`/`vmnet*`.

### `internal/store`

```go
var ErrNotFound = errors.New("store: not found")
func Open(ctx, path string, log *slog.Logger) (*Store, error) // ":memory:" ok; WAL, busy_timeout=5000, foreign keys; migrations in schema versions table
func (s *Store) Close() error
func (s *Store) UpsertDevices(ctx, devs []model.Device) error
func (s *Store) GetDevice(ctx, id model.DeviceID) (*model.Device, error)
func (s *Store) ListDevices(ctx) ([]model.Device, error)
func (s *Store) DeleteDevice(ctx, id model.DeviceID) error
func (s *Store) InsertSamples(ctx, samples []model.Sample) error
func (s *Store) LatestSample(ctx, id model.DeviceID) (*model.Sample, error)
func (s *Store) QuerySeries(ctx, id model.DeviceID, from, to time.Time) (*model.Series, error) // picks source/step per docs/API.md table
func (s *Store) NetworkSparklines(ctx, from, to time.Time, step time.Duration) (*model.OverviewSparklines, error)
func (s *Store) Rollup(ctx, olderThan time.Time) (int, error) // aggregate raw → 5m rollups (idempotent, INSERT OR REPLACE)
func (s *Store) Prune(ctx, rawRetention, rollupRetention, eventRetention time.Duration) (PruneResult, error)
type PruneResult struct{ Samples, Rollups, Events, Alerts int64 }
func (s *Store) UptimeRatios(ctx, since time.Time) (map[model.DeviceID]float64, error) // 0..1; raw+rollups union
func (s *Store) OnlineTimeline(ctx, id model.DeviceID, from, to time.Time) (*model.UptimeReport, error)
func (s *Store) InsertEvent(ctx, e *model.Event) error // sets e.ID
func (s *Store) ListEvents(ctx, q model.EventQuery) ([]model.Event, error)
func (s *Store) OpenAlert(ctx, a *model.Alert) error   // sets a.ID
func (s *Store) UpdateAlert(ctx, a *model.Alert) error // state/value/message/acked
func (s *Store) GetAlert(ctx, id int64) (*model.Alert, error)
func (s *Store) ListAlerts(ctx, q model.AlertQuery) ([]model.Alert, error)
func (s *Store) OpenAlertsByKey(ctx) (map[string]*model.Alert, error)
func (s *Store) ListRules(ctx) ([]model.AlertRule, error)
func (s *Store) SaveRule(ctx, r *model.AlertRule) error
func (s *Store) InsertAudit(ctx, a *model.AuditEntry) error
func (s *Store) ListAudit(ctx, limit int) ([]model.AuditEntry, error)
func (s *Store) Stats(ctx) (model.StoreStats, error)
func (s *Store) GetKV(ctx, key string) (string, bool, error)
func (s *Store) SetKV(ctx, key, value string) error
```
Schema: `devices(id PK, name, online, data JSON, first_seen, last_seen, updated_at)`,
`samples(device_id, ts, online, latency_ms, relay, direct, ts_rx_bytes, ts_tx_bytes, ts_rx_rate, ts_tx_rate, agent_ok, cpu, mem, disk, load1, net_rx_rate, net_tx_rate, temp_c, uptime_s, PRIMARY KEY(device_id, ts))`,
`rollups(device_id, bucket, step, samples, online_ratio, direct_ratio, latency_avg, latency_max, ts_rx_rate_avg, ts_tx_rate_avg, cpu_avg, cpu_max, mem_avg, disk_avg, load1_avg, net_rx_rate_avg, net_tx_rate_avg, temp_avg, PRIMARY KEY(device_id, bucket))`,
`events(id PK AUTOINCREMENT, ts, type, severity, device_id, device_name, title, message, data JSON)`,
`alerts(id PK, rule_id, rule_type, device_id, device_name, state, severity, title, message, value, opened_at, updated_at, resolved_at, acked_at, acked_by, data JSON)`,
`alert_rules(id PK, data JSON, updated_at)`, `audit(id PK, ts, actor, actor_node, action, target, details JSON, ok, error, remote_ip)`, `kv(key PK, value)`.
Indexes on `samples(ts)`, `events(ts)`, `events(device_id, ts)`, `alerts(state)`, `rollups(bucket)`.
All time columns are unix seconds INTEGER.

### `internal/collector`

```go
type Config struct { PollInterval, APIInterval, PingInterval, AgentTimeout time.Duration; PingConcurrency, AgentConcurrency, AgentPort int; AgentEnabled bool; RawRetention, RollupRetention, EventRetention time.Duration; Version string; DemoMode, AdminActions bool }
func New(cfg Config, st *store.Store, local source.LocalSource, api source.ControlAPI, agents source.AgentClient, log *slog.Logger) *Collector
func (c *Collector) Run(ctx) error                  // blocks; poll loop + hourly rollup/prune
func (c *Collector) Ticks() *bus.Bus[model.Snapshot] // published after each poll
func (c *Collector) Events() *bus.Bus[model.Event]   // every event also persisted to store
func (c *Collector) Snapshot() model.Snapshot        // latest in-memory snapshot (copy)
func (c *Collector) Device(id model.DeviceID) (model.Device, bool) // by id or base name
func (c *Collector) Hub() model.HubInfo
func (c *Collector) PingNow(ctx, id model.DeviceID) (*model.PingResult, error)
func (c *Collector) RefreshNow()                     // non-blocking trigger
func (c *Collector) Topology() model.Topology
func (c *Collector) Emit(ctx, e model.Event)          // persist + publish (used by httpapi/alerts)
```
Merge precedence: LocalAPI is the source of truth for online/path/bytes/IPs;
control API adds clientVersion, updateAvailable, authorized, created, expires,
keyExpiryDisabled, isExternal, advertised/enabled routes, endpoints, DERP
latency, NAT support. Devices seen only by the control API (not in the
hub's netmap, e.g. ACL-hidden) are still listed with `online` from the API's
`lastSeen` (< 5 min) and Path `unknown`.
Rates: `(bytes_now - bytes_prev) / seconds`; negative deltas (counter reset) → 0.
Device `Uptime.Pct*` from `store.UptimeRatios` refreshed every 5 minutes.
Transitions produce events: online/offline, new device, removed (absent from
both sources for 3 consecutive polls → `device.removed`, row kept), path change,
agent reachable/unreachable, version change (`device.updated`).

### `internal/alerts`

```go
type Notifier interface { Name() string; Send(ctx, n Notification) error }
type Notification struct { Type model.EventType; Alert model.Alert; Hub model.HubInfo }
func NewWebhook(url, secret string, hc *http.Client) Notifier
func NewSlack(url string, hc *http.Client) Notifier
func NewNtfy(url, token string, hc *http.Client) Notifier
func DefaultRules() []model.AlertRule // ids == type
// Source is what the engine needs from the collector (kept as an interface so
// alerts does not import collector).
type Source interface { Ticks() *bus.Bus[model.Snapshot]; Emit(ctx context.Context, e model.Event); Hub() model.HubInfo }
type Engine struct{ ... }
func NewEngine(st *store.Store, src Source, notifiers []Notifier, log *slog.Logger) *Engine
func (e *Engine) Run(ctx) error      // subscribes to col.Ticks(), evaluates, persists, publishes
func (e *Engine) Changes() *bus.Bus[model.Alert]
func (e *Engine) Rules() []model.AlertRule
func (e *Engine) SaveRule(ctx, r model.AlertRule) (model.AlertRule, error) // validates, persists, reloads
func (e *Engine) Ack(ctx, id int64, by string) (*model.Alert, error)
func (e *Engine) Test(ctx, msg string) (sent []string, errs map[string]string)
```
Pending state (condition start time) is held in memory keyed by alert key;
on restart, open alerts are loaded from the store so they resolve properly.

### `internal/demo`

```go
func New(seed int64, log *slog.Logger) *Sim // implements source.LocalSource, source.ControlAPI, source.AgentClient
func (s *Sim) Backfill(ctx, st *store.Store, d time.Duration, step time.Duration) error // synthetic history if store empty
```
~16 devices with realistic names/OSes (hub "netwatch-hub" linux, a NAS, a
Raspberry Pi, laptops macOS/windows, phones iOS/android, a cloud VM tagged
`tag:server` acting as exit node, a subnet router, a shared-in external node,
one unauthorized node, one with key expiring in 3 days, one running an old
version). Metrics evolve smoothly over time (diurnal sinusoid + noise, deterministic
from seed + time). One device flaps offline for a few minutes every ~40 minutes.
Mix of direct and DERP paths (nyc, fra, sfo).

### `internal/httpapi`

```go
type Deps struct { Cfg *config.Config; Store *store.Store; Collector *collector.Collector; Alerts *alerts.Engine; API source.ControlAPI; Local source.LocalSource; UI fs.FS; Log *slog.Logger; Version string }
func NewServer(d Deps) (*Server, error)
func (s *Server) Handler() http.Handler       // full mux incl. UI (SPA fallback to index.html for non-/api paths)
func (s *Server) ListenAndServe(ctx) error    // graceful shutdown on ctx cancel
type Authenticator interface { Authenticate(ctx, remoteAddr string) (*model.Identity, error) }
func NewTailscaleAuth(local source.LocalSource, cfg *config.Config) Authenticator
func NewNoAuth() Authenticator
```
Identity is cached per source IP for 60s. Role resolution per docs/API.md.

### `web/`

`web/embed.go` (package `web`) exposes `var Dist embed.FS` via `//go:embed all:dist`
and `func FS() (fs.FS, error)` returning the `dist` subtree. When `dist/index.html`
is missing the hub serves a plain page: "UI not built — run `make web`".

### `cmd/tailwatch`

Startup order: config → logger → store → sources (demo or real) → collector →
alert engine → httpapi. `hub.started` event. Signal handling (SIGINT/SIGTERM)
cancels the root context; graceful shutdown ≤ 10s. Exit non-zero on fatal.
In demo mode: `Backfill(24h, poll interval)` if the store has no samples.

### `cmd/tailwatch-agent`

Flags/env (`TAILWATCH_AGENT_*`): `--listen` (default auto = Tailscale IPv4:41820),
`--port`, `--auth` (`whois`|`token`|`both`, default `whois`), `--token`
(env only: `TAILWATCH_AGENT_TOKEN`), `--allow-user` (repeatable login names),
`--allow-tag` (repeatable), `--allow-node` (repeatable MagicDNS names), `--interval`
(default 5s), `--socket`. With `whois` and no allow-lists, only requests from
nodes owned by the same user as this node, or from tagged nodes carrying
`tag:tailwatch`, are accepted. Refuses to bind a non-Tailscale, non-loopback
address unless `--insecure-listen-any`. Rate limit 10 req/s. Read timeout 5s.
Logs identity of rejected requests at warn (rate-limited).
