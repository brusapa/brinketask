# Instructions for the coding agent

Project: `brinketask`. Self-hosted tasks and reminders application.

## Sources of truth

1. `SPEC.md`: behaviour, data model and design decisions (D-xx, R-x).
2. `api/openapi.yaml`: API contract.
3. `DESIGN.md`: layout, metrics and colour tokens of the web client. On behaviour, `SPEC.md` wins.

Read them before any change. If a task requires departing from them, **stop and ask**; do not improvise. If the change is approved, update the document in the same commit as the code.

## Rules

- **OpenAPI first.** To change the API, edit `api/openapi.yaml` and regenerate the code. Generated files are never edited by hand and no routes are added outside the contract, except the operational routes listed in its description (`/auth/*`, `/healthz`, `/metrics` and the web client's static files).
- **Scope.** Implement only what the current phase asks for (`SPEC.md`, section 12). Nothing from the "Out of V1" list. Do not add features, configuration options or abstractions nobody asked for.
- **Tests are mandatory.** Every behaviour change arrives with its tests in the same commit. Section 11 of `SPEC.md` lists the minimum cases. A test is never disabled, skipped or loosened to make it pass; if a test looks wrong, explain why and ask.
- **Injected clock.** Domain code (recurrence, reminders, sessions) never calls `time.Now()`; it receives a clock. Tests use a fixed clock.
- **Authorization.** Every data access is filtered by membership in `list_members`. Someone else's resource = 404.
- **Sync.** Every write to a syncable resource increments `version` and assigns a new `seq` within the same transaction (D-07). Deletion is always soft.
- **Idempotency.** Creations and actions must be repeatable with no additional effect (D-04, D-10).
- **Migrations.** Forward-only. A merged migration is never modified; add another one.
- **Dependencies.** Before adding one, justify why it is needed and check that it is maintained. Versions pinned.
- **Secrets.** None in the repository, in logs or in tests. Logs contain no task titles or descriptions.
- **i18n.** Every user-facing string, including notification texts, goes through the i18n layer. No hard-coded literals in components. V1 ships English only; do not add a language selector or other locales.

## Way of working

- Small changes: one commit per logical unit, with a message that explains the why and cites the decision (`D-10`, `R-1`) where it applies.
- Before calling anything done: lint, tests and build locally, all green.
- Everything in English: code, identifiers, comments, commit messages, documentation and UI strings.
- The code reviewer has extensive C/C++ experience and little with Go and React. Prefer explicit, direct code over clever constructs, and comment the why of any non-obvious language or framework idiom.
- If you find an ambiguity or contradiction in the specification, do not resolve it on your own: flag it and propose options.

## Planned layout

```
/api            openapi.yaml and generation config
/cmd/brinketask entry point
/internal       domain, storage, http, scheduler, notifications
/migrations     SQL
/web            React client
/deploy         Dockerfile and compose
SPEC.md
DESIGN.md
CLAUDE.md
```

## Commands

Tool versions: Go 1.27.1, golangci-lint v2.14.0, GNU Make.

| Command | What it does |
|---|---|
| `make generate` | Regenerates code from `api/openapi.yaml` (oapi-codegen v2.8.0, pinned as a `go tool`) into `internal/httpapi/api.gen.go`. Generated files are committed |
| `make check-generated` | Regenerates and fails if any `*.gen.go` file changed: run after editing the contract |
| `make lint` | golangci-lint (config in `.golangci.yml`); fails if the linter version differs |
| `make test` | `go test -race ./...`; `GO_TEST_FLAGS=` drops `-race` when no C compiler is available |
| `make build` | Static binary in `bin/brinketask` |
| `make all` | lint, test and build: run before calling anything done |
| `make image` | Builds the application image `localhost/brinketask:dev` from `deploy/Dockerfile` with Podman |
| `make dev` / `make dev-down` | Starts / stops PostgreSQL and the app (`deploy/compose.dev.yaml`) on `http://127.0.0.1:8080`. Needs a compose provider for `podman compose` (podman-compose or docker-compose) |
| `deploy/smoke-test.sh` | Runs the built image read-only next to PostgreSQL and waits for `GET /healthz` = 200 (CI runs it after `make image`) |

Integration tests start a throwaway PostgreSQL (`internal/testdb`) through testcontainers, which needs a Docker-compatible API. With Podman:

```sh
systemctl --user enable --now podman.socket
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
export TESTCONTAINERS_RYUK_CONTAINER_PRIVILEGED=true   # the cleanup container needs it under Podman
```

To run the server on the host instead of in a container, start only the database and point the binary at it:

```sh
podman compose -f deploy/compose.dev.yaml up -d db
DATABASE_URL='postgres://brinketask:brinketask@127.0.0.1:5432/brinketask?sslmode=disable' go run ./cmd/brinketask
```

CI (`.github/workflows/ci.yml`, GitHub Actions) runs `make check-generated`, golangci-lint, `make test` and `make build`, then `make image` and `deploy/smoke-test.sh`. Actions are pinned by commit SHA.
