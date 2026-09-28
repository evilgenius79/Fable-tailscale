# Tailwatch HTTP API (v1)

All endpoints live under `/api/v1/`. Responses are JSON (`application/json; charset=utf-8`).
Timestamps are RFC 3339 strings unless the field is documented as unix seconds.
JSON field names are exactly the `json:` tags in `internal/model/model.go`; the
TypeScript mirror is `web/src/api/types.ts`.

## Authentication & authorization

There are no passwords, sessions or cookies. The hub listens on its Tailscale
IP only, and every request is attributed to a tailnet identity by asking the
local `tailscaled` `WhoIs(remoteAddr)`.

* `authMode: "tailscale"` (default): requests whose source IP cannot be
  resolved by WhoIs are rejected with `401`.
* `authMode: "none"`: only allowed with `--demo` or when listening on loopback
  with `--insecure-no-auth`. Every caller is the fixed identity
  `demo@example.com` with role `admin`.

Roles:

* `viewer` – read-only. Granted to identities matching `--viewers` (default
  `*` = any tailnet identity) or `--viewer-tags`.
* `admin` – may edit alert rules, acknowledge alerts and (when
  `--enable-admin-actions` is set and a control API key is configured) run
  device admin actions. Granted to identities in `--admins` (login names) or
  nodes carrying a tag in `--admin-tags`.

Identities that match neither list get `403`.

### Browser hardening (applies to every `/api/` request)

* The `Host` header must name this hub: the listen address, the hub's
  Tailscale IPs and MagicDNS name, loopback when listening on loopback, or a
  value from `--allowed-hosts` (e.g. a reverse-proxy or TLS name). Anything
  else, including an attacker's domain re-pointed at the hub (DNS rebinding),
  gets `403` before any other processing.
* If `Sec-Fetch-Site` is present and is `cross-site`, respond `403`.
* If `Origin` is present and its host does not equal the request `Host`, `403`.
* Every non-GET request must carry the header `X-Requested-With: tailwatch`,
  otherwise `403`. (Forces a CORS preflight, which the hub never answers.)
* No CORS headers are ever emitted.
* Security headers on every response: strict CSP (`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'`), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=()`, `Cache-Control: no-store` for `/api/`.
* Request bodies are limited to 64 KiB. Server timeouts: read header 10s, read 30s, idle 120s; SSE handlers use no write timeout.
* Per-identity rate limit: 20 requests/second burst 60 (`429` when exceeded).
* Concurrent SSE streams are capped at 8 per identity and 256 in total;
  further connects get `429` with `Retry-After: 5`.

### Errors

```json
{ "error": { "code": "not_found", "message": "device not found" } }
```

Codes and their HTTP statuses: `bad_request` (400; also 413 for an oversized
body), `unauthorized` (401), `forbidden` (403), `not_found` (404; also 405 with
an `Allow` header for a wrong method on a known path), `rate_limited` (429),
`not_configured` (501; control API / admin actions disabled), `upstream` (502;
Tailscale API or LocalAPI failure), `internal` (500).

## Endpoints

| Method | Path | Role | Response |
|---|---|---|---|
| GET | `/healthz` | none | `ok` (text). No auth, no details. |
| GET | `/api/v1/me` | viewer | `Identity` |
| GET | `/api/v1/overview` | viewer | `Overview` |
| GET | `/api/v1/settings` | viewer | `Settings` (non-secret) |
| GET | `/api/v1/devices` | viewer | `Device[]` sorted by name |
| GET | `/api/v1/devices/{id}` | viewer | `DeviceDetail` |
| GET | `/api/v1/devices/{id}/series?range=1h` | viewer | `Series` |
| GET | `/api/v1/devices/{id}/uptime?range=24h` | viewer | `UptimeReport` |
| GET | `/api/v1/devices/{id}/events?limit=50` | viewer | `Event[]` |
| POST | `/api/v1/devices/{id}/ping` | viewer | `PingResult` (on-demand disco ping; 10s timeout) |
| POST | `/api/v1/devices/{id}/authorize` | admin* | `{ "authorized": true }` → `Device` |
| POST | `/api/v1/devices/{id}/tags` | admin* | `{ "tags": ["tag:server"] }` → `Device` |
| POST | `/api/v1/devices/{id}/key-expiry` | admin* | `{ "disabled": true }` → `Device` |
| POST | `/api/v1/devices/{id}/routes` | admin* | `{ "routes": ["10.0.0.0/24"] }` → `Device` |
| POST | `/api/v1/devices/{id}/name` | admin* | `{ "name": "new-name" }` → `Device` |
| DELETE | `/api/v1/devices/{id}` | admin* | `204` |
| GET | `/api/v1/events?limit=100&since=RFC3339&before=RFC3339&type=device.offline,device.online&device=ID` | viewer | `Event[]` newest first (`before` pages backwards; `type` is a comma-separated list) |
| GET | `/api/v1/alerts?state=open\|resolved\|all&device=ID&limit=200` | viewer | `Alert[]` newest first |
| POST | `/api/v1/alerts/{id}/ack` | admin | `Alert` |
| GET | `/api/v1/alerts/rules` | viewer | `AlertRule[]` |
| PUT | `/api/v1/alerts/rules/{id}` | admin | body `AlertRule` (id in path wins) → `AlertRule` |
| POST | `/api/v1/alerts/test` | admin | `{ "message": "..." }` → `{ "sent": ["webhook"] , "errors": {} }` sends a test notification |
| GET | `/api/v1/network/topology` | viewer | `Topology` |
| GET | `/api/v1/audit?limit=100` | admin | `AuditEntry[]` newest first |
| POST | `/api/v1/refresh` | admin | `202` triggers an immediate poll |
| GET | `/api/v1/stream` | viewer | SSE (see below) |

`*` requires `--enable-admin-actions` and a configured control API; otherwise `not_configured`.
Every admin action writes an `AuditEntry` and an `admin.action` event, whether it succeeded or not.

`{id}` is a `DeviceID` (stable node id). The hub also accepts the device's
MagicDNS base name for convenience.

### `range` parameter

Accepted values: `15m`, `1h`, `3h`, `6h`, `12h`, `24h`, `2d`, `7d`, `14d`, `30d`
(also any Go duration ≤ 90d). The series endpoint picks the step:

| range | source | step |
|---|---|---|
| ≤ 3h | raw samples | poll interval (15s) |
| ≤ 24h | raw samples bucketed | 60s |
| ≤ 48h | raw samples bucketed | 300s |
| > 48h | rollups | 300s (≤7d), 1800s (≤14d), 3600s (>14d) |

Missing values are `null` (JSON) / omitted; the UI renders gaps.

### SSE `/api/v1/stream`

`Content-Type: text/event-stream`. Heartbeat comment `: ping` every 15s.

Events:

* `event: tick` — data: `{ "overview": Overview, "devices": Device[] }` after each collector poll.
* `event: event` — data: `Event` for each new event.
* `event: alert` — data: `Alert` whenever an alert opens, resolves or is acked.
* `event: hello` — data: `{ "identity": Identity, "hub": HubInfo }` immediately on connect.

Clients reconnect automatically (EventSource); no `Last-Event-ID` replay — the
UI refetches on reconnect.

## Alert rule parameters

| type | threshold meaning | forSeconds | default |
|---|---|---|---|
| `device_offline` | – | offline duration before firing | 300s, warning |
| `high_cpu` | percent | sustain | 90%, 600s, warning |
| `high_memory` | percent | sustain | 90%, 600s, warning |
| `disk_full` | percent | sustain | 90%, 0, warning; ≥97% escalates to critical |
| `high_latency` | ms | sustain | 250ms, 300s, info |
| `relay_only` | – | sustain | 900s, info |
| `key_expiring` | days | – | 7d, warning; <2d critical |
| `update_available` | – | – | enabled, info |
| `agent_unreachable` | – | sustain | 300s, warning |
| `new_device` | – | auto-resolve after | 86400s, info |
| `unauthorized_device` | – | – | enabled, warning |
| `high_temperature` | °C | sustain | 85°C, 300s, warning |
| `high_load` | load1 / cpuCount ratio | sustain | 2.0, 600s, info |

An alert is `open` while its condition holds and `resolved` once it clears;
one open alert per (rule, device). Acknowledging suppresses notifications
until it resolves.

## Notifications

Configured via flags/env (never exposed by the API; secrets are environment-only):

* `--webhook-url` generic JSON POST `{ "type": "alert.opened"|"alert.resolved", "alert": Alert, "hub": HubInfo }`.
  With `TAILWATCH_WEBHOOK_SECRET` set, header `X-Tailwatch-Signature: sha256=<hex hmac of body>` is added.
* `--slack-webhook-url` Slack/Discord-compatible `{ "text": "..." }` payload.
* `--ntfy-url` (e.g. `https://ntfy.sh/mytopic`) with optional `TAILWATCH_NTFY_TOKEN`.

## Agent protocol

See `internal/agentproto`. The hub calls `GET http://<tailscale-ip>:<port>/v1/metrics`
with a 5s timeout and, if `TAILWATCH_AGENT_TOKEN` is set, header `X-Tailwatch-Token`.
