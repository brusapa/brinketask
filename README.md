# brinketask

Self-hosted tasks and reminders for a household or a small group. One
container and a PostgreSQL database; users sign in with their OpenID
Connect provider (Pocket ID), and reminders arrive as notifications on
their devices through Web Push.

## Features

- Lists, tasks with priority, manual ordering, a checklist and tags.
- Due dates, with or without a time, and repeating tasks (a subset of
  iCalendar RRULE).
- Several reminders per task, before the due time or at a set moment,
  with snooze from the notification itself.
- A web client that installs as an app (PWA) on desktop and phone.
- Trash: deleted lists and tasks can be restored for 30 days.
- An HTTP API described in OpenAPI, with an incremental sync endpoint.

V1 is complete: every phase of `SPEC.md` section 12 is done. The
interface is in English only. Sharing lists, offline work, email
reminders and native apps are planned for later versions and kept
possible by the design (`SPEC.md` section 1).

## Running it

You need a domain with https in front of the server and a Pocket ID
instance. On NixOS, the flake's module runs it as a service; see
[`docs/deployment.md`](docs/deployment.md), section "NixOS".
Elsewhere, with a container engine (Podman or Docker), in short:

```sh
podman build -f deploy/Dockerfile -t localhost/brinketask:<version> .
podman run --rm localhost/brinketask:<version> vapid-keys   # once, for the env file
```

Then fill in `deploy/brinketask.env.example` and `deploy/db.env.example`,
and start `deploy/compose.example.yaml` behind your reverse proxy.
[`docs/deployment.md`](docs/deployment.md) walks through every step: the
OIDC client, the settings, Caddy or nginx, metrics and upgrades.
[`docs/backup.md`](docs/backup.md) covers backups and restores.

## Development

Go 1.27.1, Node 24.21.0, GNU Make and Podman with a compose provider.

```sh
make dev     # PostgreSQL, Pocket ID and the app on http://localhost:8080
make all     # lint, tests and build: run before calling a change done
make image   # the container image
make e2e     # the end-to-end test (Playwright) against that image
```

`make dev` prints a one-time link that signs you in to the development
Pocket ID without a passkey. [`CLAUDE.md`](CLAUDE.md) has the rules for
changing the code, every command and how to run the server on the host.

| Path | Contents |
|---|---|
| `api/` | The OpenAPI contract; the Go server and the TypeScript types are generated from it |
| `cmd/brinketask/` | The server's entry point |
| `cmd/fakepush/` | The push service the end-to-end test uses |
| `internal/` | Domain, storage, HTTP, reminders and notifications |
| `migrations/` | SQL migrations, applied at startup |
| `web/` | The React web client, embedded in the server binary |
| `deploy/` | Dockerfile, compose files and the test scripts |
| `flake.nix`, `nix/` | The Nix package, the NixOS module and its test |
| `docs/` | Deployment and backup guides |

## Documents

- [`SPEC.md`](SPEC.md): behaviour, data model and every design decision.
- [`api/openapi.yaml`](api/openapi.yaml): the API contract.
- [`DESIGN.md`](DESIGN.md): layout and colours of the web client.

## License

MIT; see [`LICENSE`](LICENSE).
