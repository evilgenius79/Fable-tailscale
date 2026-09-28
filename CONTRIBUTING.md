# Contributing to Tailwatch

Thanks for helping. This is a small project with strict contracts; the fastest
way to a merged PR is to read `docs/ARCHITECTURE.md` and `docs/API.md` first.

## Toolchain

* Go **1.26** (`go.mod` pins the toolchain; `GOTOOLCHAIN=auto` fetches it)
* Node **22** + npm (for `web/`)
* `make`, `git`; Docker only for the container image

## Make targets

| Target | What it does |
|---|---|
| `make build` | Builds the UI, then `bin/tailwatch` and `bin/tailwatch-agent` |
| `make web` | `npm ci && npm run build` in `web/` (output embedded into the hub) |
| `make hub` / `make agent` | One binary, embedding whatever is in `web/dist` |
| `make test` | `go test -race ./...` + web typecheck and unit tests |
| `make vet` / `make fmt` | `go vet` / `gofmt -w` on `cmd`, `internal`, `web/embed.go` |
| `make run-demo` | Runs the hub with simulated data on <http://127.0.0.1:8484> |
| `make e2e` | Builds everything and runs the Playwright suite against the demo hub |
| `make cross` | Release binaries for linux/darwin/windows/freebsd into `dist/` |
| `make docker` | Builds the container image from `deploy/docker/Dockerfile` |
| `make clean` | Removes `bin/`, `dist/` and the UI build |

## Dev loop

Terminal 1 — hub with fake data, no tailnet needed:

```sh
make run-demo            # http://127.0.0.1:8484, auth disabled, 24h of backfilled history
```

Terminal 2 — UI with hot reload; Vite proxies `/api` and `/healthz` to the demo hub:

```sh
cd web && npm install && npm run dev      # http://127.0.0.1:5173
```

Go-only changes: `go build ./... && go vet ./... && go test ./internal/<pkg>/...`.
The `web` package embeds `web/dist`; if you have never built the UI, run
`make web` once (or `mkdir -p web/dist && touch web/dist/.gitkeep` to compile
without it — the hub then serves a "UI not built" page).

## Coding standards

* Go: `gofmt`, `go vet` clean; `log/slog` for logging with a `*slog.Logger`
  passed in; `context.Context` first argument on anything that blocks; every
  exported identifier documented; table-driven tests with the standard
  `testing` package only (no testify). Do not run `go mod tidy` in feature PRs
  unless you are adding a dependency — say so in the PR.
* **Never log or return secrets** (API keys, OAuth secrets, tokens, webhook
  URLs). `Settings` is the only config projection the API exposes.
* JSON field names in `internal/model` are a public contract mirrored in
  `web/src/api/types.ts` and `docs/API.md`; change all three together.
* Web: TypeScript strict mode, no `any`; Tailwind utility classes; keep
  components accessible (keyboard, contrast, `prefers-reduced-motion`).
* Shell: POSIX `sh` only, `shellcheck -s sh` clean. YAML must parse.
* Security-relevant changes (auth, headers, agent listener, systemd/Docker
  hardening) need a note in `docs/SECURITY.md`.

## Pull requests

1. One topic per PR; include tests for behaviour changes.
2. CI must be green: Go (fmt/vet/race/govulncheck), web (typecheck/test/build),
   scripts/assets lint, Docker build and the demo smoke test.
3. Describe user-visible changes in the PR body; they become the release notes.

Releases are cut by pushing a `vX.Y.Z` tag; `.github/workflows/release.yml`
builds binaries, `SHA256SUMS`, provenance attestations and the multi-arch
container image.
