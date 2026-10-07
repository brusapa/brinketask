// Package testdb starts a disposable PostgreSQL for integration tests
// (SPEC section 11: tests run against a real PostgreSQL, not a mock).
//
// It needs a Docker-compatible API. With Podman, point DOCKER_HOST at the
// Podman socket; see CLAUDE.md, "Commands".
package testdb

import (
	"context"
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
	ctx := context.Background()

	container, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("brinketask"),
		postgres.WithUsername("brinketask"),
		postgres.WithPassword("brinketask"),
		postgres.BasicWaitStrategies(),
	)
	// CleanupContainer registers the removal with t.Cleanup; it is safe to
	// call even when Run failed and container is nil.
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("testdb: start postgres: %v", err)
	}

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testdb: connection string: %v", err)
	}
	return url
}
