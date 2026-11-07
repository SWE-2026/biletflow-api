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
| 404 | `not_found`, `user_not_found` |
| 409 | `email_taken`, `role_conflict`, `invalid_event_state`, `capacity_exceeded`, `ticket_type_in_use`, `quantity_below_sold`, `payment_required`, `registration_closed`, `ticket_limit_reached`, `sold_out`, `paid_sales_inactive`, `paid_sales_suspended`, `activation_incomplete`, `invalid_order_state`, `order_expired`, `free_order`, `refund_required`, `not_checked_in`, `promo_not_found`, `promo_disabled`, `promo_not_started`, `promo_expired`, `promo_exhausted`, `promo_not_applicable` |
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
registrationClosesAt, refundPolicy, createdAt, updatedAt }`. `visibility` is `public | unlisted | private` and
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
| GET | `/me/tickets` 🔒 | — | `{tickets}`: the current user's tickets, ordered by event start |
| GET | `/events` | — | `{events}`: published **public** events |
| GET | `/events/:id` | — | `{event, ticketTypes}`: published public or unlisted event and its visible ticket types |
| POST | `/events/:id/register` 🔒 | `{ticketTypeId, quantity}` | 201 `{orderId, tickets}`: free tickets for a verified account |
| GET | `/events/:id/promo?code=` or `?campaign=` | — | `{promo}`: validates a promo code or campaign link token |
| POST | `/events/:id/checkout` 🔒 | Checkout input | 201 `{order}`: holds the tickets (paid) or issues them (free) |
| GET | `/me/orders` 🔒 | — | `{orders}`: the current user's orders, newest first |
| GET | `/me/orders/:id` 🔒 | — | `{order, tickets}` |
| POST | `/me/orders/:id/pay` 🔒 | `{outcome}` | `{order, payment}`: records a **demo** payment |
| POST | `/me/orders/:id/cancel` 🔒 | — | `{order}`: abandons a pending order and releases its hold |
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
| GET | `/organizer/events/:id/paid-sales` 🔒 | — | `{paidSales}` |
| POST | `/organizer/events/:id/paid-sales/activate` 🔒 | `{acceptTerms}` | `{paidSales}`: pays the **demo** activation fee |
| GET | `/organizer/events/:id/sales` 🔒 | — | `{sales}` |
| GET | `/organizer/events/:id/orders` 🔒 | — | `{orders}`: OrganizerOrder list, newest first |
| POST | `/organizer/events/:id/orders/:orderId/refund` 🔒 | `{reason?}` | `{order}`: full **demo** refund of a paid order |
| POST | `/organizer/events/:id/orders/:orderId/cancel` 🔒 | — | `{order}`: cancels a free registration |
| GET | `/organizer/events/:id/campaigns` 🔒 | — | `{campaigns}`, newest first |
| POST | `/organizer/events/:id/campaigns` 🔒 | Campaign input | 201 `{campaign}`: generates the code and link token |
| PUT | `/organizer/events/:id/campaigns/:campaignId` 🔒 | Campaign input | `{campaign}`; full replacement |
| POST | `/organizer/events/:id/campaigns/:campaignId/enable` 🔒 | — | `{campaign}` |
| POST | `/organizer/events/:id/campaigns/:campaignId/disable` 🔒 | — | `{campaign}` |
| GET | `/organizer/events/:id/staff` 🔒 | — | `{staff}`: the event's assigned Event Admins |
| POST | `/organizer/events/:id/staff` 🔒 | `{email, role}` | `{staffMember}`: assigns or changes a role |
| DELETE | `/organizer/events/:id/staff/:userId` 🔒 | — | 204 |
| GET | `/staff/events` 🔒 | — | `{events}`: published events the user may check in |
| GET | `/staff/events/:id` 🔒 | — | `{event, role, summary}` |
| GET | `/staff/events/:id/attendees?q=` 🔒 | — | `{tickets}`: StaffTicket search, up to 50 |
| POST | `/staff/events/:id/check-in` 🔒 | `{qrToken}` | CheckInResult |
| POST | `/staff/events/:id/tickets/:ticketId/check-in` 🔒 | — | CheckInResult: manual check-in |
| POST | `/staff/events/:id/tickets/:ticketId/undo` 🔒 | — | `{ticket}`: reverses a check-in |

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
registrationClosesAt?, refundPolicy?}`. All fields except those marked `?` are required; `description`
and `refundPolicy` may be empty. `refundPolicy` is free text up to 2000 characters shown to buyers.
`imageUrl` is an HTTP(S) URL or `null`. `capacity` is 1–1,000,000. `endsAt` must follow `startsAt`;
registration close, when present, follows registration open and precedes the event start. Both PUT
routes replace the complete editable resource rather than applying a partial patch. `capacity`
cannot drop below the sum of the event's ticket type quantities (409 `capacity_exceeded`).

**TicketType input** (for POST and PUT): `{name, description, priceAmount, quantity,
salesStartsAt?, salesEndsAt?, maxPerOrder, hidden, sortOrder}`. `name` is 1–100 characters,
`description` up to 2000 and may be empty. `priceAmount` is 0–10,000,000, `quantity` is 1–1,000,000,
`maxPerOrder` is 1–100, `sortOrder` is 0–10,000. Sales end, when both are set, follows sales start.
The sum of all of an event's ticket type quantities may not exceed the event `capacity` (409
`capacity_exceeded`). A price above 0 can be sold only after paid sales are activated for the event.
`quantity` cannot drop below the tickets issued or held for the type (409 `quantity_below_sold`), and
a type with orders cannot be deleted (409 `ticket_type_in_use`).

All organizer event routes require a bearer session with role `organizer`. An event owned by another
organizer returns 404, including for state changes and its ticket types. Invalid state transitions
return 409 `invalid_event_state`. A cancelled event cannot be edited or reopened, and its ticket types
cannot be created, changed or deleted. Private events, drafts and cancelled events are excluded from
both `/events` and `/events/:id`. Unlisted events are excluded from `/events` but can be opened by
link through `/events/:id`.

**Registration** (`POST /events/:id/register`): `ticketTypeId` is a visible ticket type of that
event, and `quantity` is 1–100. The account must have a verified email (403 `email_unverified`). The
event must be published and public or unlisted; otherwise, or for a hidden type or a type of another
event, the response is 404. Checks run in this order, each a 409:

1. `payment_required`: the type has a price above 0. Paid types go through checkout.
2. `registration_closed`: outside the type's sales window or the event's registration window, or the
   event has started.
3. `ticket_limit_reached`: the account's tickets of this type would exceed `maxPerOrder`. The limit
   counts all of the account's valid or used tickets and held quantities for the type, not only this
   request.
4. `sold_out`: fewer than `quantity` tickets of the type are left. Tickets held by pending orders
   count as taken until the hold ends.

Success creates one completed order (`totalAmount` 0) and `quantity` tickets with status `valid`.
Concurrent registrations never oversell a ticket type.

**Ticket**: `{ id, status, qrToken, createdAt, orderId, ticketTypeId, ticketTypeName, event: { id,
title, status, venueName, venueAddress, timezone, startsAt, endsAt } }`. `status` is
`valid | used | cancelled | refunded`. `qrToken` is the secret encoded in the ticket's QR code; clients show it
only to the ticket owner and never log it.

## Paid sales, checkout and refunds

All payments, activation fees and refunds are **simulations** (SRS 3.2, 4.6). No money moves. Every
payment-related object carries `demo: true`, and clients must label these records as demo
transactions.

**PaidSales**: `{ status, feeAmount, demo, activatedAt, checklist: { paidTicketType, payoutAccount,
contactPhone } }`. `status` is `inactive | active | suspended`. `feeAmount` is the one-time activation
fee in KZT (currently 10000). The checklist needs at least one ticket type with a price above 0, a
`payoutAccountReference` on the organizer profile and a `contactPhone` (the demo stand-in for identity
and payout verification).

**Activation** (`POST .../paid-sales/activate`): `acceptTerms` must be `true` (400
`validation_failed`). An incomplete checklist is 409 `activation_incomplete`; a platform suspension is
409 `paid_sales_suspended`; a cancelled event is 409 `invalid_event_state`. Activating an already active
event returns it unchanged. Activation applies to that event only and is recorded in the audit log.

**Checkout input**: `{items: [{ticketTypeId, quantity}], attendeeName, attendeeEmail?, promoCode?}`. 1–10 items,
each ticket type once, quantity 1–100. `attendeeName` is 1–200 characters; `attendeeEmail` defaults to
the account email. The account must have a verified email (403 `email_unverified`). Each item goes
through the same 404 rules and the `registration_closed`, `ticket_limit_reached` and `sold_out` checks
as registration. If any item has a price above 0 and paid sales are not active for the event, the
response is 409 `paid_sales_inactive`. Starting a checkout cancels the account's other pending orders
for the same event. Prices and totals are always computed on the server.

- Total above 0: the order is `pending` and holds its tickets for 15 minutes (`expiresAt`). No tickets
  exist until a successful payment.
- Total 0: the order is `paid` at once and its tickets are issued, without a payment.

Concurrent checkouts never oversell a ticket type. With `promoCode`, the promo checks below run after
the item checks; a failing code rejects the whole checkout.

**Order**: `{ id, eventId, eventTitle, status, subtotalAmount, discountAmount, promoCode, totalAmount, attendeeName, attendeeEmail, expiresAt,
paidAt, createdAt, items: [{ticketTypeId, ticketTypeName, quantity, unitPriceAmount}], payments:
[Payment] }`. `status` is `pending | paid | cancelled | refunded | expired`. `expired` means a pending
order whose hold has ended; its tickets are released. `totalAmount` is `subtotalAmount` minus
`discountAmount`; `promoCode` is `null` without a promo.

**Payment**: `{ id, amount, status, demo, createdAt }`. `status` is `failed | succeeded | refunded`.

**Pay** (`POST /me/orders/:id/pay`): `outcome` is `succeeded` or `failed` and picks the result of the
simulated payment. `succeeded` marks the order `paid` and issues its tickets; `failed` records a
failed payment, issues nothing and keeps the order `pending`, so the buyer can retry until the hold
ends. An order can be paid once: a second attempt, or paying a cancelled order, is 409
`invalid_order_state`; an ended hold is 409 `order_expired`. If the event is no longer published the
response is 409 `invalid_event_state`, and if paid sales were suspended it is 409
`paid_sales_inactive`. Someone else's order is 404.

**OrganizerOrder**: Order plus `{ buyerEmail, processingFeeAmount }`.

**Processing fee**: 3% of the order total, rounded to whole KZT, deducted from the organizer's payout
(the buyer pays `totalAmount`).

**Sales** (`GET .../sales`): `{ grossAmount, processingFeeAmount, refundedAmount,
estimatedPayoutAmount, paidOrders, ticketsSold, demo }`. `grossAmount` sums paid and refunded orders;
`processingFeeAmount` covers orders that are still paid; `estimatedPayoutAmount` is gross minus
refunds minus fees. `ticketsSold` counts valid and used tickets, free ones included.

**Refunds and cancellations** (organizer, own events only, else 404):

- `refund`: full refund of a `paid` order with a total above 0. The payment becomes `refunded`, the
  order `refunded` and its tickets `refunded`. A free order is 409 `free_order`; any other status is
  409 `invalid_order_state`. `reason` is optional, up to 500 characters.
- `cancel`: cancels a free (`totalAmount` 0) `paid` order; its tickets become `cancelled`. A paid order
  is 409 `refund_required`.

Refunded and cancelled tickets are invalid and free their inventory. Payments, activations, refunds
and cancellations are written to an append-only audit log.

## Event Admins and check-in

Used by the mobile ticket-verification app (SRS 4.8). Verification is online only.

**Staff assignment** (organizer, own events only, else 404): `{email, role}` where `role` is
`scanner | supervisor`. The email must belong to an existing account (404 `user_not_found`); the
person signs in to BiletFlow once to create it. Assigning an already assigned user changes their
role. A cancelled event cannot get new staff (409 `invalid_event_state`). Staff changes are written
to the audit log.

**StaffMember**: `{ userId, email, displayName, role, createdAt }`.

**Access**: a user may use the `/staff/events/:id` routes for events they are assigned to and for
events owned by their own organizer profile (role `owner`). Any other event is 404. `/staff/events`
lists only published events. Check-in and undo require a published event (409
`invalid_event_state`); the event view and attendee search also work for drafts and cancelled events.

**Summary**: `{ registered, checkedIn, remaining }`. `registered` counts valid and used tickets;
`checkedIn` counts used ones.

**StaffTicket**: `{ id, status, ticketTypeName, attendeeName, attendeeEmail, orderId, checkedInAt }`.
`attendeeName`/`attendeeEmail` come from the order's attendee details, falling back to the ticket
owner's account. It never contains the `qrToken`.

**CheckInResult**: `{ result, ticket }`. Every scan returns 200 so the scanner can show the outcome:

| result | meaning |
|---|---|
| `valid` | admitted now; the ticket is `used` and `checkedInAt` is set |
| `already_used` | the ticket was checked in before (`checkedInAt` shows when) |
| `cancelled` | the registration was cancelled |
| `refunded` | the order was refunded |
| `invalid` | no ticket with this QR token or ID belongs to this event; `ticket` is `null` |
| `campaign_qr` | a Campaign QR link or token was scanned; it never admits anyone; `ticket` is `null` |

A ticket is admitted at most once, even when two devices scan it at the same time.

`qrToken` is up to 512 characters, so a scanned campaign link is recognised rather than rejected as
too long.

**Attendee search**: `q` (up to 100 characters) matches the attendee name or email, the owner's
display name or email (case-insensitive substring), or an exact ticket ID. An empty `q` lists the
first 50 tickets by name. Results include tickets of every status.

**Undo** (`.../undo`): only the owner or a `supervisor` (403 `forbidden` for a `scanner`). Returns the
ticket to `valid` and marks the check-in reversed; the reversal is written to the audit log. A ticket
that is not checked in is 409 `not_checked_in`.

## Promo codes and Campaign QR codes

SRS 4.14. Every discount is computed and enforced on the server; links never carry an amount.

**Campaign**: `{ id, eventId, name, code, token, linkPath, discountType, discountValue, startsAt,
endsAt, maxRedemptions, status, ticketTypeIds, createdAt, updatedAt, stats: { redemptions,
ticketsSold, grossAmount, discountAmount, netAmount } }`. `discountType` is `percent | fixed`;
`discountValue` is 1–100 for `percent` and whole KZT for `fixed`. `status` is `active | disabled`.
An empty `ticketTypeIds` means the campaign applies to every ticket type of the event.

- `code` is a generated 10-character promo code (uppercase letters and digits) that buyers type.
- `token` is an opaque link token (`cmp_…`). `linkPath` is `/events/:id?campaign=<token>`; the web app
  prefixes its own HTTPS origin and renders the Campaign QR from that URL. Draw it so it looks
  clearly different from an admission ticket QR.
- `stats` covers paid orders only: `redemptions` (orders), `ticketsSold` (valid and used tickets),
  `grossAmount` (before discount), `discountAmount` and `netAmount` (gross minus discount).

**Campaign input** (POST and PUT): `{name, discountType, discountValue, startsAt?, endsAt?,
maxRedemptions?, ticketTypeIds}`. `name` is 1–100 characters; `endsAt` follows `startsAt`;
`maxRedemptions` is 1–1,000,000 or `null` for unlimited; `ticketTypeIds` lists up to 50 distinct
ticket types of this event (400 `validation_failed` on `ticketTypeIds` otherwise). The code and token
never change. A cancelled event cannot get new or changed campaigns (409 `invalid_event_state`);
enabling and disabling still work. Campaign changes are written to the audit log.

**Promo lookup** (`GET /events/:id/promo`): send exactly one of `code` (case-insensitive) or
`campaign` (the link token). The event must be published and public or unlisted (else 404). It
returns **Promo** `{ code, name, discountType, discountValue, ticketTypeIds, endsAt }`, so a scanned
campaign link can show and pre-fill its code. The lookup does not reserve anything; checkout checks
again. Failures, each a 409:

| code | meaning |
|---|---|
| `promo_not_found` | no campaign of this event has the code or token |
| `promo_disabled` | the organizer disabled the campaign |
| `promo_not_started` | before `startsAt` |
| `promo_expired` | at or after `endsAt` |
| `promo_exhausted` | `maxRedemptions` reached |
| `promo_not_applicable` | checkout only: none of the selected tickets is discounted |

**Discount at checkout**: computed on the subtotal of the applicable items. `percent` rounds to whole
KZT (half up); `fixed` is taken once per order and never exceeds the applicable subtotal. A discount
that brings the total to 0 completes the order without payment. Redemptions count paid orders and
pending orders with an active hold, and the limit is enforced while the campaign row is locked, so
concurrent checkouts never exceed `maxRedemptions`. Refunded, cancelled and expired orders free their
redemption. The processing fee applies to the discounted total.
