# Development entry points. Every target is also what CI runs, so a green
# `make lint test build` locally predicts a green pipeline.

GOLANGCI_LINT_VERSION := v2.14.0

# Container engine for `make image` and sqlc. CI and development use Podman.
CONTAINER_ENGINE ?= podman
IMAGE ?= localhost/brinketask:dev

# sqlc needs cgo, so it runs from its official image (pinned by digest)
# instead of as a `go tool`.
SQLC_IMAGE := docker.io/sqlc/sqlc:1.31.1@sha256:70f53171d27b2424e9358869975455a6e955a5aa8e58a998a270a6e34e525537

# The race detector needs cgo and a C compiler; set GO_TEST_FLAGS= to skip it.
GO_TEST_FLAGS ?= -race

.PHONY: all generate check-generated lint test build image dev dev-down clean

all: lint test build

generate:
	go generate ./...
	$(CONTAINER_ENGINE) run --rm -v "$(CURDIR):/src:z" -w /src/internal/storage $(SQLC_IMAGE) generate

# Fails when generated files differ from what the generators produce, e.g.
# after editing api/openapi.yaml without running `make generate`.
check-generated: generate
	git diff --exit-code -- '*.gen.go'
	@test -z "$$(git status --porcelain -- '*.gen.go')" || \
		{ git status --short -- '*.gen.go'; echo "generated files not committed"; exit 1; }

lint:
	@golangci-lint version | grep -q "version $(GOLANGCI_LINT_VERSION:v%=%) " || \
		{ echo "golangci-lint $(GOLANGCI_LINT_VERSION) required"; exit 1; }
	golangci-lint run ./...

test:
	go test $(GO_TEST_FLAGS) ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/brinketask ./cmd/brinketask

image:
	$(CONTAINER_ENGINE) build -f deploy/Dockerfile -t $(IMAGE) .

DEV_COMPOSE := $(CONTAINER_ENGINE) compose -f deploy/compose.dev.yaml

# Pocket ID must be up and configured before the app starts, because the
# app reads the OIDC client secret from deploy/dev.env.
dev:
	$(DEV_COMPOSE) up -d db pocket-id
	deploy/dev-oidc-setup.sh
	$(DEV_COMPOSE) up --build app

dev-down:
	$(DEV_COMPOSE) down

clean:
	rm -rf bin
