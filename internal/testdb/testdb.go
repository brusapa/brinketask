// Package testdb starts a disposable PostgreSQL for integration tests
// (SPEC section 11: tests run against a real PostgreSQL, not a mock).
//
// It needs a Docker-compatible API. With Podman, point DOCKER_HOST at the
// Podman socket; see CLAUDE.md, "Commands".
package testdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the PostgreSQL image used by tests. Keep it in step with
// deploy/compose.dev.yaml.
const Image = "docker.io/library/postgres:18.6-alpine3.24"

// Start runs a fresh, empty PostgreSQL container and returns its connection
// URL. The container is removed when the test finishes.
func Start(t *testing.T) string {
	t.Helper()
	url, terminate, err := Run(context.Background())
	t.Cleanup(terminate)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	return url
}

// Run starts a fresh, empty PostgreSQL container and returns its connection
// URL and a function that removes it. It is for code that outlives a single
// test (see package storagetest); tests call Start. terminate is never nil
// and is safe to call when err is not nil.
func Run(ctx context.Context) (url string, terminate func(), err error) {
	container, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("brinketask"),
		postgres.WithUsername("brinketask"),
		postgres.WithPassword("brinketask"),
		postgres.BasicWaitStrategies(),
	)
	terminate = func() {
		// TerminateContainer accepts a nil container (when Run failed).
		_ = testcontainers.TerminateContainer(container)
	}
	if err != nil {
		return "", terminate, fmt.Errorf("start postgres: %w", err)
	}

	url, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", terminate, fmt.Errorf("connection string: %w", err)
	}
	return url, terminate, nil
}
