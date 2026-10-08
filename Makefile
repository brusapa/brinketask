# Development entry points. Every target is also what CI runs, so a green
# `make lint test build` locally predicts a green pipeline.

GOLANGCI_LINT_VERSION := v2.14.0

# Container engine for `make image`. CI and development use Podman.
CONTAINER_ENGINE ?= podman
IMAGE ?= localhost/brinketask:dev

# The race detector needs cgo and a C compiler; set GO_TEST_FLAGS= to skip it.
GO_TEST_FLAGS ?= -race

.PHONY: all generate check-generated lint test build image dev dev-down clean

all: lint test build

generate:
	go generate ./...

# Fails when generated files differ from what the generators produce, e.g.
# after editing api/openapi.yaml without running `make generate`.
check-generated: generate
	git diff --exit-code -- '*.gen.go'

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

dev:
	$(DEV_COMPOSE) up --build

dev-down:
	$(DEV_COMPOSE) down

clean:
	rm -rf bin
