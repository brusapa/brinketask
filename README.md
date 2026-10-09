# brinketask

Self-hosted tasks and reminders. Lists, tasks with due dates, repeats,
checklists and tags, and reminders that arrive as notifications on your
devices through Web Push. Users sign in through an OpenID Connect
provider (Pocket ID). One container and a PostgreSQL database.

## Documents

- [`SPEC.md`](SPEC.md): behaviour, data model and the design decisions.
- [`api/openapi.yaml`](api/openapi.yaml): the API contract.
- [`DESIGN.md`](DESIGN.md): layout and colours of the web client.
- [`docs/deployment.md`](docs/deployment.md): building the image, Pocket
  ID, settings, reverse proxy, metrics and upgrades.
- [`docs/backup.md`](docs/backup.md): what to back up and how to restore.
- [`CLAUDE.md`](CLAUDE.md): working on the code: rules, commands and the
  development environment.

## Development

Go 1.27.1, Node 24.21.0, GNU Make and Podman.

```sh
make all     # lint, tests and build
make dev     # PostgreSQL, Pocket ID and the app on http://localhost:8080
make image   # the container image
make e2e     # the end-to-end test against that image
```

`CLAUDE.md` has the details.
