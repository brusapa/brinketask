# Deploying brinketask

brinketask is one container image and a PostgreSQL database. The server
serves the API, the web client and the login, sends the reminders and
purges old data; nothing else runs beside it. Users sign in through an
OpenID Connect provider (Pocket ID is the one SPEC section 7 targets),
and the server must be reachable over https, which a reverse proxy in
front of it provides.

`deploy/compose.example.yaml`, `deploy/brinketask.env.example` and
`deploy/db.env.example` are a working starting point for one host.

## 1. Build the image

There is no published image. Build one from a release of the repository
with Podman (or Docker):

```sh
git checkout <version>
podman build -f deploy/Dockerfile -t localhost/brinketask:<version> .
```

The image holds a single static binary with the migrations and the web
client inside. It runs as a non-root user and writes nothing to disk, so
run it with a read-only root file system and no capabilities, as the
example does.

## 2. PostgreSQL

PostgreSQL 18 (the version tests run against). Give brinketask its own
database, owned by its user: at startup the server applies the pending
migrations, and the first one creates the `unaccent` extension, which the
owner of a database may do.

```sql
CREATE ROLE brinketask LOGIN PASSWORD '…';
CREATE DATABASE brinketask OWNER brinketask;
```

The example compose file runs PostgreSQL in a container with a named
volume. Back it up as `docs/backup.md` describes.

## 3. The OIDC client in Pocket ID

In Pocket ID's administration, add an OIDC client for brinketask:

- callback URL: `PUBLIC_URL` followed by `/auth/callback`, for example
  `https://tasks.example.com/auth/callback`;
- a confidential client (not public), with PKCE enabled.

Pocket ID then shows the client ID and lets you create a client secret;
they go into `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET`. `OIDC_ISSUER` is
Pocket ID's own URL (`https://id.example.com`). The server and the users'
browsers must both reach the issuer at that same URL.

Every user who can sign in to Pocket ID and use this client gets an
account on first login. Restrict the client to a group in Pocket ID if
not everyone should.

## 4. Web Push keys

Reminders reach browsers through Web Push, which identifies the server
with a VAPID key pair. Make it once:

```sh
podman run --rm localhost/brinketask:<version> vapid-keys
```

It prints `VAPID_PUBLIC_KEY=…` and `VAPID_PRIVATE_KEY=…` for the env
file. Keep the pair for the life of the installation: browsers subscribe
with the public key, so a new pair stops every device's notifications
until each user turns them on again. The private key is a secret; back it
up with the env file (`docs/backup.md`). `VAPID_SUBJECT` is a `mailto:`
or `https:` URL where push services can reach the administrator.

## 5. Settings

All settings are environment variables (SPEC section 10). Required:

| Variable | Meaning |
|---|---|
| `PUBLIC_URL` | The origin users open, `https://…` without a path. Cookies, the OIDC callback and the same-origin checks derive from it |
| `DATABASE_URL` | PostgreSQL connection URL |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` | The client of section 3 |
| `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT` | Section 4 |

Optional:

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Application port, for the reverse proxy |
| `METRICS_LISTEN_ADDR` | `:9090` | Port of `GET /metrics`; empty turns metrics off |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `SESSION_IDLE_TIMEOUT` | `168h` | A session ends after this long unused |
| `SESSION_MAX_AGE` | `720h` | and after this long in any case |
| `SCHEDULER_INTERVAL` | `15s` | How often the reminder scheduler looks for due reminders |
| `REMINDER_MAX_LATENESS` | `12h` | A reminder later than this, after an outage, is dropped instead of sent |

`DATABASE_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` and
`VAPID_PRIVATE_KEY` can instead be read from a file: set
`DATABASE_URL_FILE=/run/secrets/db_url` and so on (not both forms).

`PUSH_TEST_ENDPOINT_PREFIX` exists for the end-to-end test only. Never
set it: it lets the server post to addresses users choose.

## 6. Reverse proxy

The proxy terminates TLS for `PUBLIC_URL` and forwards everything to the
application port. https is not optional: the session cookie is `Secure`
and browsers allow service workers, and so notifications, only on secure
origins. The server limits request bodies to 1 MiB and rate-limits each
session itself.

Caddy, which also obtains the certificate:

```
tasks.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

nginx:

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name tasks.example.com;
    ssl_certificate     /etc/ssl/tasks.example.com/fullchain.pem;
    ssl_certificate_key /etc/ssl/tasks.example.com/privkey.pem;

    # The server's own limit is 1 MiB; let it answer 413 itself.
    client_max_body_size 2m;

    # The server builds every URL from PUBLIC_URL, so it needs no
    # forwarded headers.
    location / {
        proxy_pass http://127.0.0.1:8080;
    }
}
```

Do not route `/metrics` or the metrics port through the proxy.

## 7. Start

With the example compose file, `brinketask.env` and `db.env` filled in
next to it, and the tag of the image built in section 1 in a file named
`.env` in the same directory (compose reads it by itself, for every
command):

```sh
echo BRINKETASK_VERSION=<version> > .env
podman compose -f compose.example.yaml up -d
podman compose -f compose.example.yaml logs -f app
```

The server applies the migrations, then logs `http server listening`.
Open `PUBLIC_URL`; it sends you to Pocket ID and back.

## 8. Monitoring

- `GET /healthz` on the application port answers 200 when the server can
  reach the database and 503 otherwise. Point the uptime check or the
  orchestrator's probe at it (the image has no shell, so a container
  health check cannot run a command inside it).
- Prometheus metrics are on `METRICS_LISTEN_ADDR` (D-72), on their own
  port so they are never exposed with the application. A scrape job for
  the example compose file, from a Prometheus on the same network:

  ```yaml
  scrape_configs:
    - job_name: brinketask
      static_configs:
        - targets: ["app:9090"]
  ```

  Worth watching: `brinketask_reminder_fire_delay_seconds` (reminders
  should fire within a minute), `brinketask_deliveries_total` by outcome,
  `brinketask_http_requests_total` by code, and
  `brinketask_last_purge_timestamp_seconds` (the purge runs every hour).
- Logs are JSON lines on standard output. They hold ids and counts, never
  task titles or descriptions.

## 9. Upgrades

1. Back up the database (`docs/backup.md`).
2. Build the image of the new version.
3. Set its tag in `.env` (`BRINKETASK_VERSION=<new version>`) and run
   `podman compose -f compose.example.yaml up -d`.

The new server applies its migrations at startup. Migrations only go
forward: to go back to an older version, restore the backup taken in
step 1 and set the older tag again.

Several replicas of the server can share one database: reminders are
claimed so that each is sent once, and only one replica purges at a
time. The rate limit is kept per replica.
