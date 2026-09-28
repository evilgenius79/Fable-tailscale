# Tailwatch security: threat model and hardening guide

This is the long version of [`SECURITY.md`](../SECURITY.md). It explains what
Tailwatch trusts, what it defends against, how, and what you can tighten
further. Where behaviour is a contract of the code base, the section names
the package that implements it.

## 1. Assets

| Asset | Sensitivity |
|---|---|
| Tailnet inventory: device names, owners, IPs, tags, routes, versions, key expiry | Medium — a map of your network |
| Per-device metrics and history (CPU, disks, uptime, bandwidth) | Medium — reveals workloads and habits |
| Events, alerts, audit log | Medium |
| Control-API credential (OAuth client secret / API key) | **High** — can change the tailnet |
| Agent token, webhook secret, ntfy token | Medium |
| Admin actions (authorize / tag / routes / delete devices) | **High** |

## 2. Actors

* **Anonymous internet** — should never reach anything.
* **Tailnet member (viewer)** — can read the dashboard if the ACL lets them
  connect and `--viewers` includes them (default `*`).
* **Tailnet admin (Tailwatch admin role)** — edits rules, acks alerts, runs
  admin actions if enabled.
* **Malicious tailnet peer** — a compromised device on the tailnet, or a
  shared-in external node.
* **Malicious web content** — a page on another site opened in an admin's
  browser (CSRF, clickjacking, drive-by requests to `100.x` addresses).
* **Compromised agent device** — reports garbage.
* **Supply chain** — malicious dependency, tampered release asset.

## 3. Trust boundaries and controls

### 3.1 Network exposure

* The hub listens on **its Tailscale IP only** (`--listen auto`) and
  **refuses** to bind a non-Tailscale, non-loopback address (`0.0.0.0`, a LAN
  IP) unless `--insecure-listen-any` is given (`internal/config`,
  `internal/httpapi`); the error names the flag. The container image and
  compose files keep that behaviour (host networking or a tailscale sidecar;
  no `0.0.0.0`).
* Agents behave the same: a non-Tailscale, non-loopback address is refused
  unless `--insecure-listen-any` is given (`cmd/tailwatch-agent`).
* Reachability is further governed by your **Tailscale ACL** — see
  `docs/DEPLOY.md` for a policy that only opens `8484` to members and `41820`
  to the hub.

### 3.2 Authentication: WhoIs, not passwords

Every request's source IP is resolved through `tailscaled`'s LocalAPI
`WhoIs` (`internal/tslocal`, `internal/httpapi`). Tailscale already
authenticated that peer's WireGuard key against your identity provider, so the
hub inherits SSO, MFA, device authorization and key expiry for free.

Consequences:

* No password database, no session cookies, no bearer tokens, no login page.
  There is nothing to phish or replay.
* A request whose source cannot be resolved (not a tailnet peer, or
  `tailscaled` down) gets `401`. Fail closed.
* Identities are cached per source IP for 60s to keep WhoIs cheap; a device
  that is removed from the tailnet loses access within a minute (and cannot
  connect at all once the netmap updates).
* Reverse proxies are unsupported by design: they would collapse every caller
  into `127.0.0.1`. (`--insecure-no-auth` exists for local development and is
  only honoured when listening on loopback; `--demo` implies it.)

### 3.3 Authorization: two roles

| Role | Granted by | May |
|---|---|---|
| `viewer` | `--viewers` (logins; default `*`) or `--viewer-tags` | read everything except the audit log; on-demand ping |
| `admin` | `--admins` (logins) or `--admin-tags` | edit alert rules, ack alerts, send test notifications, force refresh, read audit log, run device actions (if enabled) |

Identities matching neither list receive `403`. Tagged nodes have no user
login — they are only matched by tag, so a tagged server cannot accidentally
inherit its creator's admin role.

**Admin actions** (authorize, tags, key expiry, routes, rename, delete) need
all three: admin role, `--enable-admin-actions`, and a configured control-API
credential with write scope. Every attempt — success or failure — is written
to the audit table with actor, node, remote IP, action, target and details,
and emitted as an `admin.action` event.

### 3.4 Browser hardening (`internal/httpapi`)

The UI is a same-origin single-page app. The API enforces, on every `/api/`
request:

* `Sec-Fetch-Site: cross-site` → `403`.
* `Origin` present and host ≠ request `Host` → `403`.
* Any non-GET request without `X-Requested-With: tailwatch` → `403`. Browsers
  cannot add that header cross-origin without a CORS preflight, and the hub
  never answers preflights, so **no cross-site write is possible** even from a
  page that knows the hub's address.
* No `Access-Control-*` headers, ever.
* Response headers: `Content-Security-Policy: default-src 'self'; script-src
  'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src
  'self'; font-src 'self'; frame-ancestors 'none'; object-src 'none';
  base-uri 'self'; form-action 'self'`, `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
  `Cross-Origin-Opener-Policy: same-origin`,
  `Cross-Origin-Resource-Policy: same-origin`, `Permissions-Policy: camera=(),
  microphone=(), geolocation=()`, `Cache-Control: no-store` for `/api/`.
* No inline scripts, no third-party assets: fonts and all JS ship inside the
  binary.

DNS rebinding: every `/api/` request must carry a `Host` header that names
this hub (the listen address, its Tailscale IPs, its MagicDNS name, loopback
when listening on loopback, or an entry from `--allowed-hosts`). A page on an
attacker's domain whose DNS is re-pointed at the hub sends the attacker's
domain as `Host` and is rejected with `403` before authentication, so the
`Origin` comparison can never be satisfied by a rebound name. Independently,
such a page cannot read responses (CSP/CORP/no CORS) and cannot write (custom
header requirement).

### 3.5 Abuse resistance

* Request bodies capped at 64 KiB and decoded into fixed, typed structs.
* Server timeouts: read-header 10s, read 30s, idle 120s. SSE streams have no
  write timeout but are capped at 8 concurrent streams per identity and 256
  in total (`429`, `Retry-After: 5` beyond that), so one viewer cannot pin
  the hub's goroutines and bus subscriptions.
* Per-identity rate limit 20 req/s (burst 60) → `429`.
* Agents: 10 req/s, 5s read timeout; the hub caps agent responses at 1 MiB
  and rejects reports with an unknown protocol major
  (`internal/agentclient`).
* Health endpoint `/healthz` is unauthenticated but returns only `ok`.

### 3.6 Agents (`cmd/tailwatch-agent`, `internal/sysmetrics`)

* **Read-only by construction**: two GET endpoints returning a fixed JSON
  schema. No config endpoint, no exec, no file access beyond `/proc`, `/sys`
  and `statfs`.
* Authentication per request: `whois` (identity from the agent's own
  `tailscaled`), `token` (`X-Tailwatch-Token`), or `both`.
* Default allow policy for `whois`: same owner as the device, or a tagged node
  carrying `tag:tailwatch`. Everything else needs an explicit
  `--allow-user/--allow-tag/--allow-node`.
* Rejected requests are logged with the caller identity (rate-limited).
* Runs as an unprivileged user; the systemd unit strips all capabilities.
  Temperatures and counters are world-readable on Linux, so no root is needed.

The hub treats agent reports as **untrusted input**: bounded size, typed
decoding, rates computed defensively (negative deltas → 0), strings used only
for display (the UI renders text, never HTML).

### 3.7 Control API (`internal/tsapi`)

* Credentials come from the environment only (`TS_API_KEY`,
  `TS_OAUTH_CLIENT_ID`/`TS_OAUTH_CLIENT_SECRET`). Prefer an **OAuth client
  with the minimum scopes**: `devices:core` read for inventory; add
  `devices:core` write and `devices:routes` write only when enabling admin
  actions.
* Token responses are cached in memory until shortly before expiry; nothing
  is persisted.
* Requests go to `https://api.tailscale.com` (override `--api-base-url` only
  for testing) with retry-once on 429/5xx and `Retry-After` respect — the hub
  will not hammer the API.

### 3.8 Secrets handling

* Accepted from environment variables or `EnvironmentFile` (mode 0600) only.
  Passing a secret as a flag (`--api-key`, `--oauth-client-secret`,
  `--agent-token`, `--webhook-secret`, `--ntfy-token`) is rejected at startup
  with a message naming the environment variable, because `ps` shows argv.
* Never logged (`log/slog` with explicit fields; secret fields are not
  attached), never stored in SQLite, never returned by `/api/v1/settings`
  (which reports only *which* notifier kinds are configured).
* Webhook notifications can be HMAC-signed (`X-Tailwatch-Signature:
  sha256=<hex>`) with `TAILWATCH_WEBHOOK_SECRET` so receivers can verify origin.

### 3.9 Storage

* SQLite (pure Go `modernc.org/sqlite`, no CGO), WAL mode, foreign keys, in a
  0700 data directory owned by the service user.
* Contains inventory, metrics, events, alerts, rules, audit — no credentials.
* Retention/pruning runs hourly (raw 48h, rollups 90d, events 90d by default)
  so a stale hub does not grow forever.

### 3.10 Process and host hardening

**systemd** (`deploy/systemd/*.service`, `systemd-analyze security` ≈ 1.2/1.5
"OK"): dedicated users, `NoNewPrivileges`, empty `CapabilityBoundingSet`,
`ProtectSystem=strict` (+ `StateDirectory` for the hub only), `ProtectHome`,
`PrivateTmp`, `PrivateDevices`, `ProtectKernel{Tunables,Modules,Logs}`,
`ProtectControlGroups`, `ProtectClock`, `ProtectHostname`, `ProtectProc`
(hub), `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` (+ `AF_NETLINK`
for the agent only),
`SystemCallFilter=@system-service` minus `@privileged @resources`,
`SystemCallArchitectures=native`, `RestrictNamespaces`, `RestrictRealtime`,
`RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute`, `RemoveIPC`,
`UMask=0077`, memory/task limits, `EnvironmentFile` 0600.

Why `AF_NETLINK` only for the agent: the agent enumerates interfaces
(gopsutil, over netlink) for the per-interface counters and Tailscale-IP
detection. The hub never does — for `--listen auto` it asks `tailscaled` for
its Tailscale IP over the LocalAPI socket (`internal/httpapi`) — so the hub
unit does not grant netlink at all (verified under `strace`: only `AF_UNIX`
and `AF_INET`/`AF_INET6` sockets are opened).

Why **no** `tailscale set --operator=tailwatch`: `tailscaled` serves status,
WhoIs and the disco-ping endpoint to any local user that can connect to its
socket, so the hub runs with read-only LocalAPI access. Operator rights would
additionally let it run `tailscale up/down/logout` and change routes or exit
nodes; grant them only if your `tailscaled` build denies pings
(`docs/DEPLOY.md`).

**Container** (`deploy/docker/Dockerfile`): multi-stage build; final image is
`gcr.io/distroless/static-debian12:nonroot` (no shell, no libc, uid 65532),
binaries built with `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`. The compose
file adds `read_only`, `cap_drop: ALL`, `no-new-privileges`. The
`tailscaled` socket is the only host resource mounted.

### 3.11 Supply chain

* Go modules with `go.sum`; pure-Go dependencies (no CGO) so the static
  binary contains exactly what `go.sum` says.
* CI runs `govulncheck`, `go vet`, race tests, `npm ci` from the lockfile,
  shellcheck, and builds the image on every PR.
* Dependabot updates Go, npm, GitHub Actions and base images weekly.
* Releases publish `SHA256SUMS` and **SLSA provenance attestations**
  (`actions/attest-build-provenance`) for binaries and the container image.
  Verify with `gh attestation verify <file> --owner evilgenius79` or
  `gh attestation verify oci://ghcr.io/evilgenius79/tailwatch:X.Y.Z --owner evilgenius79`
  (image tags have no `v` prefix).
* Every GitHub Action in the workflows is pinned to a full commit SHA (with
  the version in a trailing comment so Dependabot keeps it current), and the
  release itself is created with the `gh` CLI and the job's `GITHUB_TOKEN`
  rather than a third-party action, so no floating tag can inject code into
  the job that holds `contents: write` / `id-token: write`.
* Install scripts never pipe into a root shell silently: they print the plan,
  require `--yes` when non-interactive, verify `SHA256SUMS`, and can install a
  local binary (`--binary`) for air-gapped hosts. Note that checksum
  verification proves integrity of the download, not authorship — the
  attestation does that.

## 4. Residual risks and non-goals

| Risk | Status |
|---|---|
| Compromised hub host or `tailscaled` on the hub | Not defended; the hub trusts local `tailscaled` for identity. Harden the host, keep the hub on a dedicated node/VM. |
| Compromised tailnet admin account | Not defended; equivalent to Tailscale admin console access. |
| A viewer learns the shape of your network | By design; restrict `--viewers`/`--viewer-tags` and the ACL if the dashboard should be private. |
| False metrics from a compromised device | Detected only as anomalies; treat agent data as informational. |
| Notification webhooks over plain HTTP | Use HTTPS endpoints; the signature protects integrity, not confidentiality. |
| Control-API credential with write scope on the hub | Only grant write scopes if you use admin actions; rotate on suspicion; the audit log shows every use. |
| Shared-in external nodes | Appear as `isExternal`; they can only view if the ACL lets them reach the hub and `--viewers` matches their login (`*` may — tighten it in multi-tenant tailnets). |

## 5. Hardening checklist

* [ ] Hub on a dedicated node, tagged `tag:tailwatch-hub`; ACL opens `8484`
      to the intended group only.
* [ ] `--admins` / `--admin-tags` set to named people; `--viewers` narrowed
      from `*` if the dashboard is sensitive.
* [ ] Control-API credential is an OAuth client with read-only scope unless
      admin actions are required; `--enable-admin-actions` off otherwise.
* [ ] `hub.env` is 0600 root:root; no secrets on the command line.
* [ ] Agents use `--auth both` with a random token on shared tailnets, and
      allow-list the hub identity.
* [ ] TLS via `tailscale cert` (browser secure context) — optional.
* [ ] `systemd-analyze security tailwatch.service` still says "OK" after your
      edits.
* [ ] Release assets verified (`SHA256SUMS` + `gh attestation verify`).
* [ ] Dependabot PRs merged regularly; `govulncheck` green.

## 6. Reporting

See [`SECURITY.md`](../SECURITY.md) at the repository root.
