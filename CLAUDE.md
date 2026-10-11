# BiletFlow API

Go REST API for BiletFlow, a self-service event ticketing platform for Kazakhstan (attendees,
organizers, Event Admins, platform admins). Clients: `biletflow-frontend` (React web) and
`biletflow-mobile` (Expo Event Admin scanner). Requirements: the SRS (v0.3). This file is technical.

## Stack

- Go (module `github.com/SWE-2026/biletflow-api`, `go 1.24` floor in go.mod — check before using newer features)
- Echo v4, pgx v5 (pgxpool), PostgreSQL 17, bcrypt
- Docker: multi-stage build to distroless; `docker-compose.yml` runs Postgres + API

## Commands (verify every change with these)

- `make check`: gofmt, `go vet`, `go build`
- `make test-unit`: unit tests, no database
- `make test`: unit + DB-backed tests on a throwaway Postgres (:55433)
- `make db-up` + `make run`: API in local mode on :8080
- `docker compose up --build`: full stack

Work counts as done only when `make check` and `make test` pass.

## Architecture

```
cmd/api/            entrypoint: config, wiring, graceful shutdown
internal/config/    env vars (each one documented in README "Configuration")
internal/httpapi/   Echo routes, middleware, handlers, request validation, error mapping
internal/auth/      one-time email codes, password hashing, session tokens (no HTTP)
internal/store/     Postgres queries and transactions, domain types, domain errors
internal/mail/      Mailer; LogMailer until an email provider is chosen
migrations/         numbered SQL files, embedded and applied on start
docs/api.md         the API contract shared with both clients
```

- Layering: handler → (auth) → store. Handlers validate input and map errors; business rules that
  need atomicity live in store transactions.
- The contract in `docs/api.md` is the source of truth for clients. Any change to a route, field,
  status or error code updates `docs/api.md` in the same change, and both client repos follow.
- Errors: return domain errors (`store.Err*`, `auth.Err*`) or `*httpapi.Error`; `knownErrors` in
  `errors.go` maps them to `{code, message}`. Unknown errors become a logged generic 500. Add new
  domain errors to `knownErrors`, never leak `err.Error()` to clients.
- Authorization: scope data by owner in the SQL query itself; someone else's resource answers
  404, not 403.
- Migrations are append-only: never edit a migration that may have run; add the next number.
- Schema follows the team ER diagram; extend it with new migrations, don't rename existing tables.

## Rules of engagement

- Before extending code, check the current structure fits the feature. If it doesn't, stop and
  propose the refactor first. Never force new logic into an ill-fitting function.
- One responsibility per function, about 60 lines at most. Propose a split before exceeding it.
- Minimal changes: no unrelated refactors in the same task, and never a refactor and a feature in
  one commit.
- Approval discipline: ask for design approval once; after approval, implement without new
  confirmation gates unless scope or risk changes.
- Verify unfamiliar library APIs against current docs before copying patterns from memory.
- New env vars: read them in `internal/config` and document them in the README table in the same change.
- Commit messages: `type(scope): summary`, short, English. Only commit when asked.

## Detailed standards

See `.claude/rules/` (go-style, concurrency, security, testing).

## Reporting style

After each task, reply concisely: what changed, what's left, one-line rationale only when a
decision is non-obvious.
