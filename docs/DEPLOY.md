# Deploying the Tailwatch hub

The hub is one static binary (`tailwatch`) that embeds the web UI. It needs:

1. a node on your tailnet running `tailscaled` (the hub authenticates every
   request by asking `tailscaled` who the peer is);
2. a writable data directory for the SQLite database;
3. optionally, a Tailscale control-API credential for extra inventory data
   and admin actions.

Pick one of: [bare metal](#bare-metal), [systemd](#systemd-recommended),
[Docker](#docker). Then do the [Tailscale ACL](#tailscale-acl), optionally the
[control API](#control-api-credential) and [TLS](#tls-with-tailscale-cert).

---

## Bare metal

```sh
# from a release
curl -fsSLO https://github.com/evilgenius79/fable-tailscale/releases/latest/download/tailwatch_linux_amd64
curl -fsSLO https://github.com/evilgenius79/fable-tailscale/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
sudo install -m 0755 tailwatch_linux_amd64 /usr/local/bin/tailwatch

# or from source (needs Go 1.26 + Node 22)
make build && sudo install -m 0755 bin/tailwatch /usr/local/bin/

# run
tailwatch --data-dir /var/lib/tailwatch --admins you@example.com
```

Defaults: listen on the node's first Tailscale IPv4, port 8484; data in
`./data`; every tailnet identity that can reach the port is a viewer; nobody
is admin until you name one.

Open `http://<hub-tailscale-ip>:8484/` (or `http://<hub>.<tailnet>.ts.net:8484/`
with MagicDNS) from any device on the tailnet.

## systemd (recommended)

`scripts/install-hub.sh` does all of the following; run it with `--dry-run`
to see the plan, or do it by hand:

```sh
sudo useradd --system --user-group --no-create-home --home-dir /var/lib/tailwatch \
     --shell /usr/sbin/nologin tailwatch
sudo install -m 0755 tailwatch /usr/local/bin/tailwatch
sudo install -d -m 0755 /etc/tailwatch
sudo install -m 0600 deploy/docker/hub.env.example /etc/tailwatch/hub.env   # then edit it
sudo cp deploy/systemd/tailwatch.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now tailwatch
journalctl -u tailwatch -f
```

What the unit gives you:

* runs as the unprivileged `tailwatch` user; `StateDirectory=tailwatch`
  creates `/var/lib/tailwatch` (0700) and the hub writes only there;
* `EnvironmentFile=-/etc/tailwatch/hub.env` — all configuration and secrets
  live in one root-only file; `TAILWATCH_OPTS` adds arbitrary flags;
* `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`,
  `NoNewPrivileges`, empty `CapabilityBoundingSet`, `MemoryDenyWriteExecute`,
  `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` (no netlink: the hub
  asks `tailscaled` for its IP instead of enumerating interfaces),
  `SystemCallFilter=@system-service` minus `@privileged @resources`,
  `RestrictNamespaces`, `LockPersonality`, `ProtectKernel*`, `ProtectClock`,
  `ProtectHostname`, `ProtectProc=invisible`, `UMask=0077`;
* `Restart=on-failure`, `Requires=tailscaled.service`;
* `systemd-analyze security tailwatch.service` scores it "OK" (≈1.2).

### Disco pings and tailscaled permissions

The hub measures latency with LocalAPI disco pings. On Linux `tailscaled`
serves status, WhoIs **and** the ping endpoint to every local user that can
connect to its socket (world-connectable by default; verified against
tailscaled 1.102, whose ping handler has no write gate). The unprivileged
`tailwatch` user therefore needs **no operator rights**, and you should not
grant them: an operator can also run `tailscale up/down/logout`, switch exit
nodes and change routes, which a read-only dashboard has no business doing.

Only if your `tailscaled` build denies pings (the hub log shows permission
errors from `ping` while status works), either

```sh
sudo tailscale set --operator=tailwatch     # once; survives restarts
```

or disable pings with `TAILWATCH_PING_INTERVAL=0` (you still get DERP-latency
from the control API and everything else). `install-hub.sh --operator` runs
the command for you.

### Group-restricted sockets

Some distributions put the LocalAPI socket behind a group. Uncomment
`SupplementaryGroups=tailscale` in the unit (adjust the group name) if the hub
logs a permission error opening `/var/run/tailscale/tailscaled.sock`.

## Docker

Image: `ghcr.io/evilgenius79/tailwatch` (`linux/amd64`, `linux/arm64`), built
from `deploy/docker/Dockerfile`:

* stage 1 `node:22-alpine` builds `web/`;
* stage 2 `golang:1.26-alpine` builds both binaries with `CGO_ENABLED=0
  -trimpath -ldflags "-s -w"`;
* final `gcr.io/distroless/static-debian12:nonroot` — no shell, no package
  manager, uid 65532, `/data` volume, entrypoint `tailwatch`.

Build locally: `make docker` (or `docker build -f deploy/docker/Dockerfile .`).

### Variant A — share the host's tailscaled (default compose file)

```sh
cd deploy/docker
cp hub.env.example hub.env && chmod 600 hub.env      # edit
docker compose up -d
```

`docker-compose.yml` runs the hub with `network_mode: host` (so it binds the
host's Tailscale IP and WhoIs sees real peer addresses), bind-mounts
`/var/run/tailscale/tailscaled.sock` read-write (unix sockets need write
permission to `connect()`), a named volume on `/data`, `read_only: true`,
`cap_drop: ALL` and `no-new-privileges`.

The hub appears on the tailnet as the host's node.

### Variant B — tailscale sidecar

The commented block in `docker-compose.yml` runs the official
`tailscale/tailscale` image as a sidecar and the hub with
`network_mode: service:tailscale`. The hub becomes its **own** tailnet node
(hostname `tailwatch`, tagged `tag:tailwatch-hub` through
`TS_EXTRA_ARGS=--advertise-tags=tag:tailwatch-hub`) and shares the sidecar's
LocalAPI socket through a volume.

Requirements: an auth key for that tag (`TS_AUTHKEY`, reusable + preauthorised
is convenient), **kernel networking** (`TS_USERSPACE=false`, `/dev/net/tun`,
`NET_ADMIN`) because the hub must bind the node's Tailscale IP and dial agents;
userspace networking cannot do that.

### Notes for both variants

* **No `HEALTHCHECK`.** Distroless has no shell or curl. Probe from outside:
  `curl -fsS http://$(tailscale ip -4):8484/healthz` (host mode) or from
  another container in the same namespace. `/healthz` needs no auth and
  returns only `ok`.
* **Pings** work with the plain socket access the container user (uid 65532)
  has: `tailscaled` serves the ping endpoint to read-only clients (see
  [Disco pings](#disco-pings-and-tailscaled-permissions)). Set
  `TAILWATCH_PING_INTERVAL=0` only if your `tailscaled` denies them.
* **Bind mounts** for `/data` must be owned by `65532:65532`.
* **Images are signed**: verify with
  `gh attestation verify oci://ghcr.io/evilgenius79/tailwatch:X.Y.Z --owner evilgenius79`
  (image tags carry no `v` prefix: a release `v1.2.3` is published as `1.2.3`,
  `1.2` and `latest`).
* Logs go to **stderr** as JSON (`docker logs` shows them;
  `TAILWATCH_LOG_JSON=true` is set in the image). Stdout only ever carries
  `--help` / `--version` output, so redirect `2>&1` when running the binary
  by hand.

## Tailscale ACL

Tailwatch is only as reachable as your policy lets it be. This example uses
two tags — `tag:tailwatch-hub` for the hub node and `tag:tailwatch` for tagged
servers that run the agent — and lets every member open the UI:

```jsonc
{
  "tagOwners": {
    // who may apply the tags (and whose OAuth clients may create tagged nodes)
    "tag:tailwatch-hub": ["autogroup:admin"],
    "tag:tailwatch":     ["autogroup:admin"],
  },

  "acls": [
    // Members reach the hub UI/API. Restrict src to a group for a private dashboard.
    {"action": "accept", "src": ["autogroup:member"], "dst": ["tag:tailwatch-hub:8484"]},

    // The hub scrapes agents on tagged servers and on members' devices.
    {"action": "accept", "src": ["tag:tailwatch-hub"], "dst": ["tag:tailwatch:41820", "autogroup:member:41820"]},

    // ...your existing rules...
  ],

  // Optional: SSH etc. unchanged. Nothing else needs to reach 41820 or 8484.
}
```

Things to know:

* A peer only appears in the hub's netmap (and therefore gets online/path/
  latency/bandwidth data) if the policy allows **some** traffic between it and
  the hub in either direction. Devices with no relation to the hub are still
  listed when the control API is configured (path `unknown`, online derived
  from `lastSeen`).
* With the hub tagged `tag:tailwatch-hub`, run agents with
  `--allow-tag tag:tailwatch-hub` (agents accept `tag:tailwatch` and same-user
  nodes by default; see `docs/AGENT.md`).
* Using TLS on another port? Change `8484` above accordingly.
* For an untagged hub owned by `you@example.com`, replace `tag:tailwatch-hub`
  with `you@example.com` (or the hub's MagicDNS name).

## Control-API credential

Optional. It adds client version / update-available, authorized state, key
expiry, advertised vs. enabled routes, control-plane endpoints, per-DERP
latency, NAT traits, devices the hub cannot see, and enables admin actions.

Create an **OAuth client** (Admin console → Settings → OAuth clients):

| Scope | Needed for |
|---|---|
| `devices:core` **read** | inventory enrichment (the default, read-only) |
| `devices:core` **write** | admin actions: authorize, set tags, rename, key expiry, delete |
| `devices:routes` **write** | admin action: enable/disable subnet routes |

Also give the client the tag(s) it may assign (e.g. `tag:tailwatch-hub`,
`tag:server`): the API refuses to set tags the client does not own.

```sh
# /etc/tailwatch/hub.env (0600)
TS_OAUTH_CLIENT_ID='k…'
TS_OAUTH_CLIENT_SECRET='tskey-client-…'
TAILWATCH_TAILNET=-                     # "-" = the client's own tailnet
TAILWATCH_ENABLE_ADMIN_ACTIONS=true     # only if you want write actions
TAILWATCH_ADMINS=you@example.com        # who may run them
```

A personal API key (`TS_API_KEY=tskey-api-…`) works too but expires after 90
days and carries the full rights of your account — prefer the OAuth client.

Admin actions are off by default. `--enable-admin-actions` turns them on and
**requires** a credential: the hub refuses to start (`config: ... requires
control API credentials`, exit 2) when the flag is set without `TS_API_KEY`
or `TS_OAUTH_CLIENT_ID`/`TS_OAUTH_CLIENT_SECRET`, so add the credential to
`hub.env` before (or together with) `TAILWATCH_ENABLE_ADMIN_ACTIONS=true`.
Every action (success or failure) is recorded in the audit log
(`/api/v1/audit`, admin-only) and as an `admin.action` event.

## TLS with `tailscale cert`

Traffic on the tailnet is already WireGuard-encrypted; TLS mainly removes
browser "not secure" warnings and enables features that require a secure
context. Tailscale can issue a Let's Encrypt certificate for the node's
MagicDNS name once **HTTPS certificates** are enabled in the admin console
(DNS → HTTPS Certificates).

```sh
sudo install -d -m 0750 -o root -g tailwatch /etc/tailwatch/tls
sudo tailscale cert --cert-file /etc/tailwatch/tls/hub.crt \
                    --key-file  /etc/tailwatch/tls/hub.key  hub.tail1234.ts.net
sudo chown root:tailwatch /etc/tailwatch/tls/hub.*
sudo chmod 0640 /etc/tailwatch/tls/hub.*
```

Then in `hub.env`:

```sh
TAILWATCH_TLS_CERT=/etc/tailwatch/tls/hub.crt
TAILWATCH_TLS_KEY=/etc/tailwatch/tls/hub.key
```

and open `https://hub.tail1234.ts.net:8484/`. Certificates last 90 days;
renew with a timer that re-runs `tailscale cert` and restarts the hub
(the hub reads the PEM files at start):

```ini
# /etc/systemd/system/tailwatch-cert.service
[Unit]
Description=Renew Tailwatch TLS certificate
[Service]
Type=oneshot
ExecStart=/usr/bin/tailscale cert --cert-file /etc/tailwatch/tls/hub.crt --key-file /etc/tailwatch/tls/hub.key hub.tail1234.ts.net
ExecStartPost=/bin/chown root:tailwatch /etc/tailwatch/tls/hub.crt /etc/tailwatch/tls/hub.key
ExecStartPost=/bin/chmod 0640 /etc/tailwatch/tls/hub.crt /etc/tailwatch/tls/hub.key
ExecStartPost=/bin/systemctl try-restart tailwatch.service

# /etc/systemd/system/tailwatch-cert.timer
[Unit]
Description=Renew Tailwatch TLS certificate weekly
[Timer]
OnCalendar=weekly
RandomizedDelaySec=1h
Persistent=true
[Install]
WantedBy=timers.target
```

Do **not** front the hub with `tailscale serve` or another reverse proxy: the
hub authenticates by the *source IP* of the connection, and a proxy would make
every request look like it comes from localhost (auth fails closed with 401).

## Upgrading

* systemd: re-run `install-hub.sh` (it keeps `hub.env`) or replace the binary
  and `systemctl restart tailwatch`. Database migrations run automatically at
  start; take a copy of `/var/lib/tailwatch/tailwatch.db*` first if you care.
* Docker: `docker compose pull && docker compose up -d`.

## Backup

Everything is in `tailwatch.db` (+ `-wal`/`-shm` while running) in the data
directory. Use `sqlite3 tailwatch.db ".backup backup.db"` for a consistent
copy, or stop the service and copy the files. Nothing secret is stored there.

## Sizing

Per device the hub stores one raw sample per poll (15s default, kept 48h) and
one 5-minute rollup row (kept 90 days). A raw sample costs ≈ 140 bytes on
disk including indexes (measured on the demo hub: 13.7 MB for 98k samples),
so 100 devices ≈ 1.15 M samples ≈ 150–200 MB of SQLite at 15s / 48h, plus a
few MB of WAL and rollups; hourly rollup/prune keeps it flat. Halve it with
`--poll-interval 30s` or a shorter `--raw-retention`. CPU is negligible;
memory is dominated by SQLite caches (tens of MB). Fewer devices / longer
`--poll-interval` scale it down for a Raspberry Pi.
