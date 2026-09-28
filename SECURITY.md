# Security policy

Tailwatch sits on your tailnet with read access to `tailscaled` and, optionally,
a credential for the Tailscale control API. We take reports seriously.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

* Preferred: open a private report via GitHub Security Advisories
  (**Security → Report a vulnerability** on
  <https://github.com/evilgenius79/fable-tailscale>).
* Include: affected version/commit, how to reproduce, impact, and whether the
  issue needs a tailnet identity, an admin role or a control-API credential.

You will get an acknowledgement within 7 days. Fixes for confirmed issues are
released as a patch version with a GitHub advisory; reporters are credited
unless they ask not to be.

Supported: the latest tagged release and `main`.

## Threat model in one page

The full model and hardening guide is in [docs/SECURITY.md](docs/SECURITY.md).

**Trust boundaries**

| Boundary | How it is enforced |
|---|---|
| Internet → hub | The hub binds a Tailscale IP only (`--listen auto`); it is never reachable from outside the tailnet. No public ports, no reverse proxy needed. |
| Tailnet peer → hub | Every request is attributed to a tailnet identity through `tailscaled` **WhoIs** on the source IP. There are no passwords, sessions, cookies or tokens to steal. Tailscale ACLs decide who can even connect. |
| Viewer → admin | Roles come from `--admins` / `--admin-tags` (and `--viewers` / `--viewer-tags`). Admin-only endpoints: rule edits, acks, on-demand ping, test notifications, audit log, refresh and device actions. |
| Browser page → hub | Same-origin only: `Sec-Fetch-Site: cross-site` and mismatched `Origin` are rejected, every write needs `X-Requested-With: tailwatch`, no CORS headers are ever sent, strict CSP, `frame-ancestors 'none'`. |
| Hub → agents | Agents listen on their Tailscale IP only, authenticate the caller with WhoIs and/or a shared token, allow-list by user, tag or node, rate limit, and never expose more than read-only system metrics. |
| Hub → Tailscale API | Optional. Read scope by default; write scope only when you opt into `--enable-admin-actions`. Every admin action is audited. |
| Process → host | Static binaries, no shell in the container image, hardened systemd units (no capabilities, read-only filesystem, syscall filter), unprivileged users. |

**Out of scope / assumptions**

* A compromised tailnet admin account or a compromised hub host is game over
  by design: the hub trusts `tailscaled` for identity.
* Tailwatch does not protect against a malicious `tailscaled` on the hub node.
* Metrics from agents are treated as untrusted data (size-limited, schema
  checked, never executed), but an attacker who controls a device can obviously
  report false metrics for that device.
* Demo mode (`--demo`) and `--insecure-no-auth` disable authentication and are
  for local evaluation only; the latter is refused unless the listener is on
  loopback.

**Secrets**

Control-API keys, OAuth client secrets, the agent token, webhook secrets and
ntfy tokens are taken from the environment (or an `EnvironmentFile` with mode
0600). They are never written to the database, never returned by the API
(`/api/v1/settings` is a non-secret projection) and never logged.
