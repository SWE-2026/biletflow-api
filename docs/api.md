# BiletFlow API contract (v1)

The single contract shared by the web app (`biletflow-frontend`) and the Event Admin app
(`biletflow-mobile`). Change it here first, then in the clients.

## Conventions

- **Base URL**: `http://localhost:8080/api/v1` locally. Clients set it as `VITE_API_URL` /
  `EXPO_PUBLIC_API_URL` **including** `/api/v1`.
- **Auth**: `Authorization: Bearer <token>`. Tokens are opaque, last 7 days and are revoked by logout.
- **JSON** in and out. Unknown request fields are rejected (`invalid_json`).
- **Times** are RFC 3339 strings in UTC. **Money** is an integer number of whole KZT.
- **Lists** are wrapped (`{"events": [...]}`) and are `[]` when empty.

## Errors

```json
{ "code": "validation_failed", "message": "Some fields are invalid.", "fields": { "email": "Enter a valid email address." } }
```

`code` is stable, so clients branch on it; `message` is for people; `fields` appears only for
`validation_failed`.

| Status | code |
|---|---|
| 400 | `invalid_json`, `validation_failed`, `invalid_code` |
| 401 | `unauthorized`, `invalid_credentials` |
| 403 | `forbidden`, `email_unverified` |
| 404 | `not_found` |
| 409 | `email_taken`, `role_conflict`, `invalid_event_state`, `capacity_exceeded`, `ticket_type_in_use` |
| 410 | `code_expired` (expired, already used, or never requested) |
| 429 | `resend_too_soon`, `too_many_attempts`, `rate_limited` |
| 500 | `internal` (details only in server logs) |

## Objects

**User**: `{ id, email, displayName, role, organizerId, emailVerified, hasPassword, createdAt }`.
`role` is `customer | organizer | staff | admin`.

**Session**: `{ token, user }`.

**Challenge**: `{ expiresInSeconds, resendInSeconds, demoCode? }`. `demoCode` is present only when the
API runs with `APP_ENV=local` (no email provider yet).

**Organizer**: `{ id, type, name, contactEmail, contactPhone, payoutAccountReference, createdAt }`.
`type` is `individual | business`. `payoutAccountReference` is an optional **demo label**, not a bank
account number or payment credential. Creating a profile changes the current user's role to
`organizer`; the response includes the updated user so clients can replace their stored session user.

**Event**: `{ id, organizerId, title, description, category, imageUrl, venueName, venueAddress,
timezone, visibility, capacity, status, startsAt, endsAt, registrationOpensAt,
registrationClosesAt, createdAt, updatedAt }`. `visibility` is `public | unlisted | private` and
`status` is `draft | published | cancelled`. Times are RFC 3339 values; the event's IANA `timezone`
preserves its local scheduling context. A new event is always a draft.

**TicketType**: `{ id, eventId, name, description, priceAmount, quantity, salesStartsAt,
salesEndsAt, maxPerOrder, hidden, sortOrder, createdAt, updatedAt }`. `priceAmount` is whole KZT
(0 = free). `salesStartsAt`/`salesEndsAt` are `null` when unrestricted. Hidden types are left out
of the attendee event page. Lists are ordered by `sortOrder`, then creation time.

## Endpoints

Both sign-in styles reach the same account, matched by email. `/auth` is rate-limited per IP.

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/auth/otp/request` | `{email}` | 200 Challenge: emails a 6-digit sign-in code |
| POST | `/auth/otp/verify` | `{email, code}` | 200 Session: creates the account on first sign-in, marks the email verified |
| POST | `/auth/signup` | `{email, password, displayName}` | 201 Session |
| POST | `/auth/login` | `{email, password}` | 200 Session |
| POST | `/auth/password/reset/request` | `{email}` | 200 Challenge: emails a 6-digit reset code |
| POST | `/auth/password/reset/confirm` | `{email, code, password}` | 200 Session: sets the password, signs out every other session |
| POST | `/auth/logout` 🔒 | — | 204 |
| GET | `/me` 🔒 | — | `{user}` |
| GET | `/events` | — | `{events}`: published **public** events |
| POST | `/organizer/profile` 🔒 | Organizer input | 201 `{organizer, user}`; verified customer account only |
| GET | `/organizer/profile` 🔒 | — | `{organizer}`; organizer only |
| PUT | `/organizer/profile` 🔒 | Organizer input | `{organizer}`; full replacement, organizer only |
| POST | `/organizer/events` 🔒 | Event input | 201 `{event}` in `draft` state |
| GET | `/organizer/events` 🔒 | — | `{events}`: current organizer's events, all states |
| GET | `/organizer/events/:id` 🔒 | — | `{event}` owned by current organizer |
| PUT | `/organizer/events/:id` 🔒 | Event input | `{event}`; full replacement, except cancelled events |
| POST | `/organizer/events/:id/publish` 🔒 | — | `{event}`; draft → published |
| POST | `/organizer/events/:id/unpublish` 🔒 | — | `{event}`; published → draft |
| POST | `/organizer/events/:id/cancel` 🔒 | — | `{event}`; draft/published → cancelled |
| GET | `/organizer/events/:id/ticket-types` 🔒 | — | `{ticketTypes}`: all of the event's types, hidden included |
| POST | `/organizer/events/:id/ticket-types` 🔒 | TicketType input | 201 `{ticketType}` |
| PUT | `/organizer/events/:id/ticket-types/:typeId` 🔒 | TicketType input | `{ticketType}`; full replacement |
| DELETE | `/organizer/events/:id/ticket-types/:typeId` 🔒 | — | 204; only while the type has no orders |

Codes: valid 10 minutes, resend after 30 s, 5 attempts. Passwords: 8–72 characters.

**Password reset**: an unknown email still gets a 200 Challenge (without `demoCode`) and no email,
so the endpoint does not reveal which accounts exist. Reset codes and sign-in codes are separate:
one cannot be used for the other. Confirming a reset also sets a first password on a code-only
account, marks the email verified and revokes all existing sessions. The password is validated
before the code, so a `validation_failed` response does not spend an attempt.

**Organizer input**: `{type, name, contactEmail, contactPhone?, payoutAccountReference?}`. The
name and contact email are required. Email verification is required before creating a profile.
An account that is already an organizer or has a staff/admin role receives `role_conflict`.

**Event input** (for POST and PUT): `{title, description, category, imageUrl?, venueName,
venueAddress, timezone, visibility, capacity, startsAt, endsAt, registrationOpensAt?,
registrationClosesAt?}`. All fields except those marked `?` are required; `description` may be empty.
`imageUrl` is an HTTP(S) URL or `null`. `capacity` is 1–1,000,000. `endsAt` must follow `startsAt`;
registration close, when present, follows registration open and precedes the event start. Both PUT
routes replace the complete editable resource rather than applying a partial patch. `capacity`
cannot drop below the sum of the event's ticket type quantities (409 `capacity_exceeded`).

**TicketType input** (for POST and PUT): `{name, description, priceAmount, quantity,
salesStartsAt?, salesEndsAt?, maxPerOrder, hidden, sortOrder}`. `name` is 1–100 characters,
`description` up to 2000 and may be empty. `priceAmount` is 0–10,000,000, `quantity` is 1–1,000,000,
`maxPerOrder` is 1–100, `sortOrder` is 0–10,000. Sales end, when both are set, follows sales start.
The sum of all of an event's ticket type quantities may not exceed the event `capacity` (409
`capacity_exceeded`). Storing a price does not enable paid sales; checkout is a later feature.

All organizer event routes require a bearer session with role `organizer`. An event owned by another
organizer returns 404, including for state changes and its ticket types. Invalid state transitions
return 409 `invalid_event_state`. A cancelled event cannot be edited or reopened, and its ticket types
cannot be created, changed or deleted. Private and unlisted events are excluded from the public
`/events` list; attendee detail access is a later API feature.
