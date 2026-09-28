SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MODULE  := github.com/evilgenius79/fable-tailscale
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT) -X $(MODULE)/internal/version.Date=$(DATE)
export GOTOOLCHAIN ?= auto
export CGO_ENABLED := 0

.PHONY: all build web hub agent test test-go test-web lint vet fmt clean run-demo e2e docker

all: build

## build: build the web UI, hub and agent into ./bin
build: web hub agent

## web: build the React UI into web/dist (embedded into the hub)
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## hub: build the hub binary (embeds whatever is in web/dist)
hub:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/tailwatch ./cmd/tailwatch

## agent: build the agent binary
agent:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/tailwatch-agent ./cmd/tailwatch-agent

## test: run Go and web tests
test: test-go test-web

test-go:
	CGO_ENABLED=1 go test -race -count=1 ./...

test-web:
	cd web && npm run typecheck && npm test

vet:
	go vet ./...

fmt:
	gofmt -l -w cmd internal web/embed.go

## run-demo: run the hub with simulated data on http://127.0.0.1:8484
run-demo: hub
	./bin/tailwatch --demo --listen 127.0.0.1:8484 --data-dir ./data-demo

## e2e: build everything, start demo hub and run Playwright tests
e2e: build
	cd web && npm run e2e

## docker: build the container image
docker:
	docker build -f deploy/docker/Dockerfile -t tailwatch:$(VERSION) .

## cross: build release binaries for common platforms into ./dist
cross: web
	@mkdir -p dist
	@for target in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 freebsd/amd64; do \
	  os=$${target%/*}; arch=$${target#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "building $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tailwatch_$${os}_$${arch}$$ext ./cmd/tailwatch || exit 1; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tailwatch-agent_$${os}_$${arch}$$ext ./cmd/tailwatch-agent || exit 1; \
	done

clean:
	rm -rf bin dist web/dist/* && touch web/dist/.gitkeep

help:
	@grep -E '^## ' Makefile | sed 's/^## //'
