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

Local mode (`APP_ENV=local`) returns one-time email codes in responses as `demoCode`, because no
email provider is configured yet.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | — (required) | Postgres connection string |
| `PORT` | `8080` | HTTP port |
| `APP_ENV` | `production` | `local` exposes one-time codes as `demoCode`; never on a public server |
| `CORS_ORIGINS` | none | comma-separated browser origins, e.g. `http://localhost:5173,http://localhost:8081` |

## Checks

```bash
make check       # gofmt, go vet, go build
make test-unit   # no database needed
make test        # unit + DB-backed tests on a throwaway Postgres (:55433)
```
