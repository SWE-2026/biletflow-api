# Operations: backups and recovery

SRS 7 requires regular backups and recovery procedures. This is the procedure for the Docker
Compose deployment (`docker compose up`), where Postgres keeps its data in the `postgres_data`
volume.

## Backups

- `make backup` writes a full logical dump to `backups/biletflow-<timestamp>.dump`
  (`pg_dump` custom format, compressed). The `backups/` folder is git-ignored.
- Schedule it at least daily on the demo server, for example with cron:
  `0 3 * * * cd /srv/biletflow-api && make backup`
- Keep the last 14 daily dumps and copy them off the server (another machine or S3-compatible
  storage). A dump on the same disk does not survive a disk failure.
- Dumps contain personal data (emails, names, orders). Store them encrypted and with the same access
  restrictions as the database.

## Recovery

1. Stop the API so nothing writes during the restore: `docker compose stop api`.
2. Restore the chosen dump: `make restore FILE=backups/biletflow-<timestamp>.dump`. This drops and
   recreates every object in the dump (`--clean --if-exists`).
3. Start the API: `docker compose start api`. Migrations run on start and apply anything newer than
   the dump.
4. Check `GET /health` returns `{"status":"ok"}` and sign in as an organizer to confirm the data.

## Recovery test

Restore the newest dump into a scratch database at least once per release (for example on the test
profile, `docker compose --profile test up -d postgres-test`, then `pg_restore` with `-p 55433`) and
check that `GET /health` and an event list work. A backup that has never been restored is not a
backup.

## Encryption

Traffic must use TLS in front of the API (reverse proxy or load balancer). Data at rest is protected
by the host: run Postgres on an encrypted volume (cloud disk encryption or LUKS) and keep dumps
encrypted. The API stores no payment-card data; passwords use bcrypt and session tokens and one-time
codes are stored hashed.
