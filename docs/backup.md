# Backing up and restoring brinketask

All of brinketask's state is in its PostgreSQL database (SPEC section
10). The server keeps nothing on disk, so a backup is a database dump
plus the settings that cannot be made again.

## What to keep

1. **The database**, dumped regularly (below).
2. **`brinketask.env`** (or wherever the settings live), above all
   `VAPID_PRIVATE_KEY` with its public key. A new key pair stops every
   device's notifications until each user turns them on again. The OIDC
   client secret can be made again in Pocket ID, the key pair cannot.
3. **Pocket ID's own data**, as its documentation describes. brinketask
   knows its users by the identity Pocket ID gives them; a user who is
   created again in Pocket ID gets a new identity and, with it, a new and
   empty brinketask account.

## Dump

`pg_dump` in its custom format (`-Fc`) makes a compressed file that
`pg_restore` reads; it is consistent without stopping the server. With
the example compose file:

```sh
podman compose -f compose.example.yaml exec -T db \
  pg_dump -U brinketask -Fc brinketask > brinketask-$(date +%F).dump
```

Run it daily from cron or a systemd timer, copy the files off the host,
and delete old ones on a schedule. The dumps hold every user's tasks:
store them as privately as the database itself.

## Restore

The same steps check a backup: restore it into a scratch database now
and then.

1. Stop the server, so nothing writes during the restore:

   ```sh
   podman compose -f compose.example.yaml stop app
   ```

2. Replace the database with an empty one owned by the same user:

   ```sh
   podman compose -f compose.example.yaml exec -T db \
     psql -U brinketask -d postgres -c 'DROP DATABASE brinketask' -c 'CREATE DATABASE brinketask OWNER brinketask'
   ```

3. Load the dump:

   ```sh
   podman compose -f compose.example.yaml exec -T db \
     pg_restore -U brinketask -d brinketask --no-owner --exit-on-error < brinketask-2026-10-09.dump
   ```

4. Start the server of the same version as the dump, or a newer one; a
   newer one applies its migrations at startup. An older one cannot read
   a newer schema.

   ```sh
   podman compose -f compose.example.yaml start app
   ```

5. Check: `GET /healthz` answers 200, the log shows no errors, and
   signing in shows the tasks of the backup.

6. **Ask users to reload every open tab** of brinketask. The web client
   keeps its copy of the data in the open page and asks only for what
   changed since its last sync; after a restore it would not learn of the
   older state. A reload starts it from scratch. Installed apps and
   phones need the same: close and open them again.

Everything after the dump is lost, including reminders already sent and
devices registered later. Devices registered later keep working once
their users turn notifications on again.
