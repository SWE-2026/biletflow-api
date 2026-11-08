# Milestone 2 implementation report

**Date:** 8 November 2026

**Milestone:** Ticketing, payments, check-in, campaigns, support, administration and notifications API

**Repository:** [biletflow-api](https://github.com/SWE-2026/biletflow-api), `main` at `041a804`
(follows Milestone 1 at `ff8c92b`)
**Status:** Implemented. `make check` and the full database-backed test suite pass.

## Delivered

Migrations `004` to `014` are append-only and extend the team ER diagram. The full contract is in
[`api.md`](api.md).

| Area | What the API does | Migrations |
|---|---|---|
| Ticket types | Organizer CRUD for ticket types with price, sale window and per-type capacity. Capacity is enforced atomically. Adds the public event detail endpoint. | 004 |
| Password reset | Reset by email code. It also sets a first password on accounts that only used codes. | 005 |
| Free registration and My Tickets | `POST /events/:id/register` with quantity, no overselling, and `GET /me/tickets`. | 006 |
| Paid sales and checkout | Paid sales activation with a checklist and a demo fee. Demo checkout with a 15-minute hold, server-side totals, demo pay (succeed or fail), order cancel and organizer refunds. Changes are recorded in `audit_log`. | 007 |
| Event Admins and check-in | Event staff with owner and supervisor roles. Online ticket check-in with six outcomes, and undo for the owner or a supervisor. | 008 |
| Promo codes and campaign QR codes | Campaigns limited to selected ticket types. `GET /events/:id/promo` checks a code and explains why one can't be used. | 009 |
| Support cases | Attendee→organizer and organizer→platform cases with message threads, assignment, status changes and unread counts. | 010 |
| Analytics and history | Event analytics, attendee lists, an activity timeline from `audit_log`, and event duplication. | — |
| Platform administration | Users, events, orders, payments, reports and settings. Suspending users and events, handling event reports, and changes to platform settings and organizers, all recorded in the audit log. | 011 |
| Disputes | Payment disputes opened by attendees and resolved by admins, ending in a demo refund. | 012 |
| Paid-sales review and payouts | Admins approve or suspend activations. Demo payouts can be scheduled, put on hold or marked paid. Adds event images. | 013 |
| Notifications and ticket files | Notifications are stored for the in-app list and emailed from a retrying background queue (only logged when SMTP isn't set up). Tickets download as A4 PDFs, plus a QR code PNG. | 014 |

`docs/operations.md` adds the backup and restore procedure (SRS 7).

All payments, activation fees, refunds and payouts are simulations. Every such record carries
`demo: true`.

## Verification

| Check | Result |
|---|---|
| `make check` (gofmt, `go vet`, `go build`) | Passed |
| `go test -race -count=1 ./...` with `TEST_DB_DSN` on a fresh PostgreSQL 17 | Passed (`auth`, `httpapi`; migrations 001–014 applied from scratch) |

`make test` could not use its usual port 55433 on the test machine, because another local project's
database already used it. The same suite ran on a temporary PostgreSQL 17 container on another port.

## Scope boundary and follow-up

- **Seating:** the schema has no seat maps. The web app still has `Seat Selection` and
  `Seating Layout` routes. Either build seating or drop both routes.
- **Seating is a bonus feature** (SRS 12, 13.4) and is deferred until the required web screens are done.
- **Notification links (fixed):** links now point to the web app's real routes, chosen per
  recipient (`internal/store/weblinks.go`), and a test checks them.
- **Clients:** most of the matching web screens are still placeholders: My Orders, support,
  notifications, organizer event tabs and the admin portal. The mobile scanner already uses the
  check-in API.
- No shared database or deployment was changed.
