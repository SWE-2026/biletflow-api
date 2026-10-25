# Milestone 1 implementation report

**Date:** 25 October 2026

**Milestone:** Organizer profiles, roles and event management API (Arsen and Gleb; due 26 October 2026)

**Repository:** [biletflow-api](https://github.com/SWE-2026/biletflow-api), based on `main` at `ff8c92b`
**Status:** Implemented and verified against a local PostgreSQL 17 database.

## Delivered

- Added append-only migration `003_organizer_events.sql` for organizer contact/demo payout-reference fields and event category, venue, timezone, visibility, registration window, capacity, and update timestamp. The original sample event keeps its 100-ticket capacity.
- Added organizer profile creation, read, and full update endpoints. A verified customer can create one profile; creation atomically promotes the account to `organizer`. Staff/admin and existing organizer accounts cannot create another profile.
- Added organizer-only create, list, read, and full update event endpoints. Draft events can be published; published events can be unpublished; draft or published events can be cancelled. Cancelled events cannot be edited or reopened.
- Added role middleware and owner-scoped event queries. Anonymous calls receive 401, non-organizer calls receive 403, and another organizer's event returns 404.
- Added bounded input validation for profile and event fields, IANA timezone checks, registration timing checks, UUID path validation, and state-conflict errors.
- Restricted public `GET /events` to published **public** events. Private and unlisted events remain visible only through the owner's organizer endpoints at this stage.
- Updated `docs/api.md` and `README.md` with the new contract and local workflow.

## Verification

| Check | Result |
|---|---|
| `go test -count=1 ./...` with `TEST_DB_DSN` on a temporary PostgreSQL 17 database | Passed, including fresh migration application and HTTP/database tests |
| Organizer lifecycle, cross-organizer access, visibility, validation, and concurrent profile creation tests | Passed |
| `go vet ./...` | Passed |
| `go build ./...` | Passed |
| `gofmt -l .` and `git diff --check` | Clean |
| PostgreSQL parser on migration 003 | Parsed all 15 statements |

The repository's `make test` target specifies Go's race detector. The Windows Go environment has `CGO_ENABLED=0` and no C compiler, so `go test -race` could not run here. The equivalent database-backed test suite ran without `-race`; the concurrent organizer-creation test also passed. Docker Desktop could not start its Linux engine because WSL virtualization is disabled on this machine, so a temporary native PostgreSQL server was used instead.

## Scope boundary and follow-up

This milestone provides organizer and event API behavior. The web organizer screens are a separate team milestone and are still placeholders. Ticket types, registration, checkout, payouts, and event audit history are later work. The payout reference is a demo label, not bank data; paid-sale activation still needs its own verification and payment flow. Private and unlisted attendee-facing detail access is also not part of this milestone.

No shared database or deployment was changed. The implementation is ready for review in `biletflow-api`.
