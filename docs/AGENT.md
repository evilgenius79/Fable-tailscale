# tailwatch-agent

`tailwatch-agent` is a tiny, read-only daemon you run on the devices you want
system metrics from. The hub polls it over the tailnet every poll interval and
turns the numbers into charts, uptime history and alerts.

Without an agent a device still shows up in Tailwatch with everything
`tailscaled` knows (online state, direct/relay path, latency, bandwidth to the
hub, client version, key expiry, routes …). The agent adds:

| Metric | Source |
|---|---|
| CPU % (total and per core), load averages, core count, model | gopsutil |
| Memory / swap usage | gopsutil |
| Disk usage per mount, with a "primary" volume for the headline number | gopsutil |
| Per-interface network counters (the hub derives byte rates); physical vs. virtual detection | gopsutil |
| Temperatures (where the OS exposes sensors) | gopsutil |
| Boot time / uptime, process count, kernel, platform, arch | gopsutil |
| Local Tailscale version, backend state, IPs, health warnings | tailscaled LocalAPI |

## How it works

* Listens on the device's **Tailscale IPv4 only**, port **41820** by default.
  It refuses to bind a non-Tailscale, non-loopback address unless you pass
  `--insecure-listen-any`.
* Serves two read-only endpoints (JSON):
  * `GET /v1/metrics` → a full `agentproto.Report`
  * `GET /v1/health`  → `{"ok":true,"agentVersion":"…","protocolVersion":"1.0"}`
* Samples every `--interval` (default 5s) in the background; a request never
  triggers expensive work, it just returns the latest snapshot.
* Every request is **authenticated** (see below), rate limited to 10 req/s,
  and read with a 5s timeout. Rejected requests are logged at `warn` with the
  caller identity (rate-limited so a scanner cannot fill your logs).
* The wire format is versioned (`protocolVersion`); the hub only accepts
  reports whose major version it understands.

## Authentication (`--auth`)

| Mode | Who is accepted |
|---|---|
| `whois` (default) | The local `tailscaled` is asked *who is behind this source IP?* Then, if no allow-list is configured: nodes owned by the **same user** as this device, or tagged nodes carrying **`tag:tailwatch`**. With allow-lists, only identities matching `--allow-user` / `--allow-tag` / `--allow-node`. |
| `token` | Requests carrying the shared secret in `X-Tailwatch-Token` (the hub sends it when `TAILWATCH_AGENT_TOKEN` is set). |
| `both` | WhoIs **and** token must pass. Recommended for tagged/shared environments. |

Allow-list flags are repeatable:

```sh
tailwatch-agent --allow-tag tag:tailwatch-hub --allow-user alice@example.com --allow-node hub
```

`--allow-node` takes MagicDNS names (base name or FQDN).

Recommended setups:

* **Personal tailnet, everything owned by you:** defaults are enough. The hub
  is owned by the same user, agents accept it.
* **Hub is a tagged node:** either tag it `tag:tailwatch` (accepted by default)
  or tag it `tag:tailwatch-hub` and run agents with
  `--allow-tag tag:tailwatch-hub` (the ACL example in `docs/DEPLOY.md`).
* **Shared tailnet / defence in depth:** `--auth both` with a random token
  (`openssl rand -hex 32`) set on the hub (`TAILWATCH_AGENT_TOKEN`) and on
  every agent.

Even with a valid identity the agent only ever returns metrics; there are no
write endpoints.

## Flags and environment

Every flag can be given as `TAILWATCH_AGENT_<FLAG>` (upper-case, `-` → `_`).
Flags override the environment. Repeatable flags accept comma-separated values
in the environment.

| Flag | Env | Default | Description |
|---|---|---|---|
| `--listen` | `TAILWATCH_AGENT_LISTEN` | `auto` | `auto` = first Tailscale IPv4 + `--port`; or `host:port`. |
| `--port` | `TAILWATCH_AGENT_PORT` | `41820` | Port used by `auto`. Must match the hub's `--agent-port`. |
| `--auth` | `TAILWATCH_AGENT_AUTH` | `whois` | `whois`, `token` or `both`. |
| *(none)* | `TAILWATCH_AGENT_TOKEN` | – | Shared secret for `token`/`both`. **Environment only** — never a flag, so it does not show in `ps`. |
| `--allow-user` | `TAILWATCH_AGENT_ALLOW_USER` | – | Login names allowed (repeatable). |
| `--allow-tag` | `TAILWATCH_AGENT_ALLOW_TAG` | – | Tags allowed (repeatable). |
| `--allow-node` | `TAILWATCH_AGENT_ALLOW_NODE` | – | MagicDNS node names allowed (repeatable). |
| `--interval` | `TAILWATCH_AGENT_INTERVAL` | `5s` | Sampling interval. |
| `--socket` | `TAILWATCH_AGENT_SOCKET` | platform default | tailscaled LocalAPI socket path. |
| `--insecure-listen-any` | `TAILWATCH_AGENT_INSECURE_LISTEN_ANY` | `false` | Allow binding a non-Tailscale address. Do not use outside tests. |

Run `tailwatch-agent --help` for the authoritative list of your version.

## Install

### Linux with systemd (recommended)

```sh
curl -fsSLO https://raw.githubusercontent.com/evilgenius79/fable-tailscale/main/scripts/install-agent.sh
less install-agent.sh                                   # read it first
sudo sh install-agent.sh --allow-tag tag:tailwatch-hub  # interactive; add --yes for automation
```

The installer:

1. detects OS/arch and downloads `tailwatch-agent_<os>_<arch>` from the GitHub
   release (`--version vX.Y.Z` to pin; `--binary ./tailwatch-agent` to use a
   local build), verifying it against `SHA256SUMS` when the release ships one
   (`--require-checksum` to insist);
2. installs it to `/usr/local/bin/tailwatch-agent`;
3. creates the unprivileged system user `tailwatch-agent`;
4. writes `/etc/tailwatch/agent.env` (0600) with `TAILWATCH_AGENT_OPTS` and,
   if `TAILWATCH_AGENT_TOKEN` was in the installer's environment, the token;
5. installs the hardened unit (`deploy/systemd/tailwatch-agent.service`,
   identical to `install-agent.sh --print-unit`) and runs
   `systemctl enable --now tailwatch-agent`.

It prints the plan first and asks for confirmation; piping it into `sh`
without `--yes` aborts on purpose. `--dry-run` shows the plan without root.
`--uninstall` removes the unit and binary.

With a token:

```sh
sudo TAILWATCH_AGENT_TOKEN="$(cat /path/to/token)" sh install-agent.sh --auth both --allow-tag tag:tailwatch-hub
```

Change settings later by editing `/etc/tailwatch/agent.env` and running
`sudo systemctl restart tailwatch-agent`.

### Manual (any Linux/BSD)

```sh
sudo install -m 0755 tailwatch-agent_linux_amd64 /usr/local/bin/tailwatch-agent
sudo useradd --system --no-create-home --shell /usr/sbin/nologin tailwatch-agent
sudo cp deploy/systemd/tailwatch-agent.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now tailwatch-agent
```

The unit runs as `tailwatch-agent` with no capabilities, a read-only view of
the filesystem, a syscall allow-list and no home/tmp. It only needs the
`tailscaled` socket (world-connectable on Linux by default; uncomment
`SupplementaryGroups=` in the unit if yours is group-restricted).

### Docker

The container image ships the agent binary too, but the agent needs the
**host's** `/proc`, `/sys`, mounts and `tailscaled`, so containerising it
mostly defeats the purpose. Install it on the host instead.

### macOS

Use the open-source `tailscaled` (`brew install tailscale`, run as a service)
rather than the App Store app: the agent and the hub talk to the LocalAPI
socket, and the sandboxed GUI app only exposes it to the local user via the
`tailscale` CLI. Then run `tailwatch-agent` under `launchd`, for example with a
`LaunchDaemon` plist calling `/usr/local/bin/tailwatch-agent` — there is no
official plist yet (contributions welcome).

### Windows

`tailwatch-agent_windows_amd64.exe` runs from a console or as a service (via
`sc.exe create` or NSSM). WhoIs works with the standard Tailscale client. It is
less tested than Linux; please report issues.

### FreeBSD

`tailwatch-agent_freebsd_amd64` works with the `net/tailscale` package. Write an
`rc.d` script or run it under `daemon(8)`; a sample script is welcome.

## Tailscale ACL

The hub must be able to reach TCP `41820` on every agent device:

```jsonc
{"action": "accept", "src": ["tag:tailwatch-hub"], "dst": ["tag:tailwatch:41820", "autogroup:member:41820"]}
```

See `docs/DEPLOY.md` for the full policy example.

## Troubleshooting

| Symptom | Check |
|---|---|
| Device shows agent `unreachable` | `sudo journalctl -u tailwatch-agent -n 50`. Is the ACL letting the hub in on 41820? Is the hub identity allowed (`--allow-*`, `tag:tailwatch`, same user)? |
| `whois failed` in agent logs | The agent cannot reach `tailscaled` (socket path, `--socket`) or the source IP is not a tailnet peer (are you curling from the wrong interface?). |
| `refusing to listen on non-Tailscale address` | `--listen` points at a LAN IP. Use `auto` or the device's 100.x address. |
| Token rejected | Same value on hub (`TAILWATCH_AGENT_TOKEN`) and agent? Whitespace or quotes in the env file? |
| No temperatures | The platform exposes none to gopsutil (common on VMs, macOS, Windows). |
| Test from the hub | `curl -s http://<agent-ts-ip>:41820/v1/health` from the hub node. |
