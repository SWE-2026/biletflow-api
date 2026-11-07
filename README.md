# BiletFlow API

Go REST API for BiletFlow, a self-service event ticketing platform for Kazakhstan. Serves the web app
and the Event Admin mobile app. The contract is in [docs/api.md](docs/api.md).

## Run it

```bash
docker compose up --build          # Postgres + API on http://localhost:8080
```

or, for development with `go run`:

```bash
make db-up                         # Postgres only, on :5432
make run                           # API in local mode on :8080
```

Migrations in `migrations/` are embedded in the binary and applied on start.

Organizer profiles and event creation are available through `/api/v1/organizer`. Sign in with a
verified email first, then create a profile. The API contract documents the required event fields
and lifecycle actions. The local database also contains one sample published event.

Local mode (`APP_ENV=local`) returns one-time email codes in responses as `demoCode`. Without
`SMTP_HOST` no email leaves the server; notifications are still stored and shown in the app.

PDF tickets embed Noto Sans (`internal/tickets/fonts`, SIL Open Font License, see `OFL.txt`) so
Kazakh, Russian and English text and the ₸ sign print correctly.

## Stack and rationale

SRS 9 asks for the backend choice to be documented.

- **Go** with Echo and pgx. Go's standard library covers HTTP, crypto and testing, so the API has
  few dependencies. It compiles to one static binary that runs in a small distroless container, and
  goroutines handle concurrent check-ins and checkouts without extra infrastructure. Echo adds
  routing and middleware only, so the code stays close to plain Go.
- **PostgreSQL 17**. Ticketing is relational: events, ticket types, orders, tickets, payments and
  refunds reference each other, and inventory, ticket holds, promo limits and check-ins must be
  enforced in transactions with row locks. Postgres gives that atomicity, `CHECK` constraints that
  keep money and status values valid, and aggregation for the analytics.
- **One service**. A single API with embedded migrations keeps development and deployment simple
  (SRS 9 rules out unnecessary microservices); Docker Compose runs it with the database.

Backups and recovery are described in [docs/operations.md](docs/operations.md).

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | — (required) | Postgres connection string |
| `PORT` | `8080` | HTTP port |
| `APP_ENV` | `production` | `local` exposes one-time codes as `demoCode`; never on a public server |
| `CORS_ORIGINS` | none | comma-separated browser origins, e.g. `http://localhost:5173,http://localhost:8081` |
| `PUBLIC_WEB_URL` | `http://localhost:5173` | web app origin used in email links and Campaign QR codes; use the HTTPS origin in production |
| `TRUST_PROXY` | `false` | `true` only when the API runs behind a reverse proxy on a private network; client IPs (used by the `/auth` rate limit) are then read from `X-Forwarded-For` hops sent by private-network proxies. Leave `false` when clients reach the API directly, so a spoofed header cannot bypass the rate limit |
| `SMTP_HOST` | none | transactional email server; when unset, emails are logged (subject only) instead of sent |
| `SMTP_PORT` | `587` | `587` uses STARTTLS (required), `465` uses implicit TLS |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | none | SMTP credentials; keep them in the environment or a secret store, never in the repo |
| `MAIL_FROM` | — (required with `SMTP_HOST`) | sender address, e.g. `tickets@biletflow.kz` |
| `ADMIN_EMAILS` | none | comma-separated emails promoted to platform admin on start; each account must exist and be a plain customer account |

## Checks

```bash
make check       # gofmt, go vet, go build
make test-unit   # no database needed
make test        # unit + DB-backed tests on a throwaway Postgres (:55433)
```
