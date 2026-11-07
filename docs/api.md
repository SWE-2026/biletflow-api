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
| 403 | `forbidden`, `email_unverified`, `account_suspended` |
| 404 | `not_found`, `user_not_found` |
| 409 | `email_taken`, `role_conflict`, `invalid_event_state`, `capacity_exceeded`, `ticket_type_in_use`, `quantity_below_sold`, `payment_required`, `registration_closed`, `ticket_limit_reached`, `sold_out`, `paid_sales_inactive`, `paid_sales_suspended`, `activation_incomplete`, `invalid_order_state`, `order_expired`, `free_order`, `refund_required`, `not_checked_in`, `promo_not_found`, `promo_disabled`, `promo_not_started`, `promo_expired`, `promo_exhausted`, `promo_not_applicable`, `invalid_state`, `already_reported`, `already_disputed` |
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

**Event**: `{ id, organizerId, title, description, category, imageUrl, imageUrls, venueName, venueAddress,
timezone, visibility, capacity, status, phase, startsAt, endsAt, registrationOpensAt,
registrationClosesAt, refundPolicy, createdAt, updatedAt }`. `phase` is computed on each read:
`cancelled` for a cancelled event, otherwise `upcoming` before `startsAt`, `active` until `endsAt`
and `completed` after. `visibility` is `public | unlisted | private` and
`status` is `draft | published | cancelled | suspended` (`suspended` is set by platform admins only). Times are RFC 3339 values; the event's IANA `timezone`
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
| POST | `/events/:id/report` 🔒 | `{reason, details?}` | 204: reports the event to platform admins |
| POST | `/events/:id/checkout` 🔒 | Checkout input | 201 `{order}`: holds the tickets (paid) or issues them (free) |
| GET | `/me/orders` 🔒 | — | `{orders}`: the current user's orders, newest first |
| GET | `/me/orders/:id` 🔒 | — | `{order, tickets}` |
| POST | `/me/orders/:id/pay` 🔒 | `{outcome}` | `{order, payment}`: records a **demo** payment |
| POST | `/me/orders/:id/cancel` 🔒 | — | `{order}`: abandons a pending order and releases its hold |
| POST | `/me/orders/:id/dispute` 🔒 | `{reason, details?}` | 201 `{dispute}`: disputes a paid order |
| GET | `/me/disputes` 🔒 | — | `{disputes}`: the current user's disputes |
| POST | `/organizer/profile` 🔒 | Organizer input | 201 `{organizer, user}`; verified customer account only |
| GET | `/organizer/profile` 🔒 | — | `{organizer}`; organizer only |
| PUT | `/organizer/profile` 🔒 | Organizer input | `{organizer}`; full replacement, organizer only |
| GET | `/organizer/payouts` 🔒 | — | `{payouts}`: the organizer's demo payouts, newest first |
| POST | `/organizer/events` 🔒 | Event input | 201 `{event}` in `draft` state |
| GET | `/organizer/events?phase=` 🔒 | — | `{events}`: current organizer's events, all states; optional `phase` filter |
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
| POST | `/organizer/events/:id/paid-sales/activate` 🔒 | `{acceptTerms}` | `{paidSales}`: pays the **demo** activation fee and requests platform review |
| GET | `/organizer/events/:id/sales` 🔒 | — | `{sales}` |
| GET | `/organizer/events/:id/orders` 🔒 | — | `{orders}`: OrganizerOrder list, newest first |
| POST | `/organizer/events/:id/orders/:orderId/refund` 🔒 | `{reason?}` | `{order}`: full **demo** refund of a paid order |
| POST | `/organizer/events/:id/orders/:orderId/cancel` 🔒 | — | `{order}`: cancels a free registration |
| POST | `/support/cases` 🔒 | SupportCase input | 201 `{case}` |
| GET | `/support/cases?status=&eventId=` 🔒 | — | `{cases}`: cases the user may see, latest activity first, up to 100 |
| GET | `/support/cases/unread` 🔒 | — | `{unread}`: number of cases with activity the user has not seen |
| GET | `/support/cases/:id` 🔒 | — | `{case, messages}`; marks the case read |
| POST | `/support/cases/:id/messages` 🔒 | `{body}` | 201 `{case}` |
| POST | `/support/cases/:id/status` 🔒 | `{status}` | `{case}`; handlers only |
| POST | `/support/cases/:id/assign` 🔒 | `{assigneeId}` | `{case}`; handlers only, `null` unassigns |
| GET | `/organizer/events/:id/campaigns` 🔒 | — | `{campaigns}`, newest first |
| POST | `/organizer/events/:id/campaigns` 🔒 | Campaign input | 201 `{campaign}`: generates the code and link token |
| PUT | `/organizer/events/:id/campaigns/:campaignId` 🔒 | Campaign input | `{campaign}`; full replacement |
| POST | `/organizer/events/:id/campaigns/:campaignId/enable` 🔒 | — | `{campaign}` |
| POST | `/organizer/events/:id/campaigns/:campaignId/disable` 🔒 | — | `{campaign}` |
| POST | `/organizer/events/:id/duplicate` 🔒 | — | 201 `{event}`: a new draft copied from the event |
| GET | `/organizer/events/:id/analytics` 🔒 | — | `{analytics}`; filters `from`, `to`, `ticketTypeId` |
| GET | `/organizer/events/:id/activity` 🔒 | — | `{activity}`: timeline; filters `from`, `to`, `type` |
| GET | `/organizer/events/:id/attendees` 🔒 | — | `{tickets}`: StaffTicket list; filters `ticketTypeId`, `status`, `q` |
| GET | `/organizer/events/:id/disputes` 🔒 | — | `{disputes}`: disputes on the event's orders |
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

**Event input** (for POST and PUT): `{title, description, category, imageUrl?, imageUrls?, venueName,
venueAddress, timezone, visibility, capacity, startsAt, endsAt, registrationOpensAt?,
registrationClosesAt?, refundPolicy?}`. All fields except those marked `?` are required; `description`
and `refundPolicy` may be empty. `refundPolicy` is free text up to 2000 characters shown to buyers.
`imageUrl` is the cover image, an HTTP(S) URL or `null`; `imageUrls` lists up to 10 more HTTP(S)
image URLs for the event gallery (SRS 4.2) and defaults to `[]`. `capacity` is 1–1,000,000. `endsAt` must follow `startsAt`;
registration close, when present, follows registration open and precedes the event start. Both PUT
routes replace the complete editable resource rather than applying a partial patch. `capacity`
cannot drop below the sum of the event's ticket type quantities (409 `capacity_exceeded`).

**TicketType input** (for POST and PUT): `{name, description, priceAmount, quantity,
salesStartsAt?, salesEndsAt?, maxPerOrder, hidden, sortOrder}`. `name` is 1–100 characters,
`description` up to 2000 and may be empty. `priceAmount` is 0–10,000,000, `quantity` is 1–1,000,000,
`maxPerOrder` is 1–100, `sortOrder` is 0–10,000. Sales end, when both are set, follows sales start.
The sum of all of an event's ticket type quantities may not exceed the event `capacity` (409
`capacity_exceeded`). A price above 0 can be sold only while the event's paid sales are `active` (requested and approved).
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
3. `ticket_limit_reached`: `quantity` is above the type's `maxPerOrder` (SRS 4.3: the limit applies
   to one order; an account may place several orders).
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

**PaidSales**: `{ status, feeAmount, demo, requestedAt, activatedAt, reviewNote, checklist: {
paidTicketType, payoutAccount, contactPhone } }`. `status` is `inactive | pending | active | rejected |
suspended`. `feeAmount` is the one-time activation
fee in KZT, taken from the platform settings (default 10000) until activation and fixed after it. The checklist needs at least one ticket type with a price above 0, a
`payoutAccountReference` on the organizer profile and a `contactPhone` (the demo stand-in for identity
and payout verification).

**Activation** (`POST .../paid-sales/activate`, SRS 3.2, 4.5, 2.3): `acceptTerms` must be `true` (400
`validation_failed`). The system checks the checklist (409 `activation_incomplete` when incomplete),
records the demo activation fee payment and accepted terms, and sets `status` to `pending`. A platform
admin then approves (`active`) or rejects it with a `reviewNote` (`rejected`); paid checkout opens
only when `active`. After a rejection the organizer fixes the issue and requests again, without
paying the fee a second time. A platform suspension is 409 `paid_sales_suspended`; a cancelled event
is 409 `invalid_event_state`. Requesting while `pending` or `active` returns the current state.
Activation applies to that event only; requests and decisions are recorded in the audit log.

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

**Processing fee**: the platform's `processingFeePercent` (default 3%) of the order total, fixed when the order is created, rounded to whole KZT, deducted from the organizer's payout
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

## Support cases

SRS 4.13. Asynchronous threads; clients poll (no WebSocket). Notifications are in-app through
`unread`; email notifications come with the email provider.

**Kinds and access**

- `attendee`: opened by any signed-in user about an event, order or ticket. Handled by the event's
  organizer.
- `organizer`: opened by an organizer about their account, activation, payments or a technical
  problem, optionally about one of their events. Handled by platform admins.

The requester always sees their own cases. Organizer users see the `attendee` cases of their events;
platform admins (role `admin`) see every case. Anyone else gets 404. `viewerRole` is `requester` or
`handler` for the current user. Only handlers change status or assignment (403 `forbidden`).

**SupportCase input**: `{kind, category, subject, message, eventId?, orderId?, ticketId?}`.
`subject` is 1–200 characters and `message` (the first message) 1–5000.

| kind | categories | links |
|---|---|---|
| `attendee` | `ticket_delivery`, `payment`, `refund`, `seating`, `event_information`, `check_in`, `account`, `technical`, `other` | exactly one of `eventId`, `orderId`, `ticketId` |
| `organizer` | `account`, `activation`, `payment`, `technical`, `other` | optional `eventId` only |

The server fills in the context itself: a ticket brings its order and event, an order brings its
event. The ticket or order must belong to the requester, and an event linked directly must be
published or cancelled and public or unlisted; otherwise 404. An `organizer` case needs an organizer
account (403 `forbidden`), and its `eventId` must be one of the organizer's events (404).

**SupportCase**: `{ id, kind, category, subject, status, requesterId, requesterEmail, requesterName,
eventId, eventTitle, orderId, ticketId, assigneeId, assigneeEmail, viewerRole, unread, createdAt,
updatedAt, lastActivityAt }`. `status` is `open | in_progress | waiting_for_customer | resolved`.
`unread` is true when someone else acted on the case after the current user last opened it.

**SupportMessage**: `{ id, kind, authorId, authorName, authorRole, body, createdAt }`. `kind` is
`message`, `status_change` (`body` is the new status) or `assignment` (`body` is the assignee's
email, empty when unassigned). `authorRole` is `requester` or `handler`. The full history is kept;
messages cannot be edited or deleted.

**Rules**

- A reply from the requester reopens a `waiting_for_customer` or `resolved` case as `open`.
- `assigneeId` must be a platform admin, or for an `attendee` case a user of the event's organizer
  (400 `validation_failed` on `assigneeId` otherwise).
- `status` filters the list (`?status=open`); any other value is 400 `validation_failed`.
- `eventId` limits the list to cases linked to one event, so organizers can review the support
  history of past and cancelled events (SRS 4.16).

## Organizer analytics, attendees and history

SRS 4.15 and 4.16. Owner organizer only; any other event is 404. Everything is computed from
BiletFlow's own order, ticket, campaign, refund and check-in records, and money is demo money
(`demo: true`). Completed and cancelled events keep all their data and stay readable through every
organizer route.

**Filters** (query parameters): `from` and `to` are RFC 3339 times (`to` exclusive and after `from`),
`ticketTypeId` is one of the event's ticket types. An invalid value is 400 `validation_failed`.

**Analytics** (`GET .../analytics`):

```
{ capacity, ticketsSold, ticketsReserved, ticketsRemaining, percentSold,
  grossAmount, discountAmount, refundedAmount, netAmount, orders,
  checkedIn, absent, checkInPercent, demo,
  ticketTypes: [{ ticketTypeId, name, priceAmount, quantity, available, reserved, sold,
                  refunded, cancelled, checkedIn, grossAmount }],
  salesOverTime: [{ date, orders, tickets, grossAmount }],
  campaigns: [Campaign] }
```

- Ticket counts are current totals and ignore `from`/`to`. `sold` counts valid and used tickets,
  `reserved` the quantities held by pending orders, `available` is `quantity − sold − reserved`,
  `checkedIn` counts used tickets and `absent` is `sold − checkedIn`. Percentages have one decimal.
- `capacity` is the event capacity, or the type's `quantity` when `ticketTypeId` is set.
  `ticketsRemaining` is `capacity − ticketsSold`.
- Money covers paid and refunded orders whose payment time falls in `from`/`to`: `grossAmount` is
  before discounts, `netAmount` is gross minus discounts minus refunds, and `orders` counts them.
  Order-level money ignores `ticketTypeId`; per-type `grossAmount` and `salesOverTime` respect it.
- `salesOverTime` groups by calendar day in the event's time zone (`date` is `YYYY-MM-DD`).
- `campaigns` lists the event's campaigns with their all-time `stats`.

**Attendees** (`GET .../attendees`): every ticket of the event (up to 5000) as StaffTicket, ordered
by attendee name. `status` is `valid | used | cancelled | refunded`; `q` (up to 100 characters)
matches attendee or account name or email.

**Activity timeline** (`GET .../activity`): **ActivityEntry** `{ id, createdAt, actorId, actorName,
action, type, entityType, entityId, description }`, oldest first, up to 1000 entries. `action` is
`<type>.<verb>`, e.g. `event.published`; `description` is a short English summary. The `type`
filter takes one of `event`, `ticket_type`, `paid_sales`, `payment`, `refund`, `registration`,
`staff`, `campaign`, `check_in`, `support`, `report`, `dispute`, `payout`. Recorded actions:

- `event.created`, `event.duplicated`, `event.updated` (with capacity changes), `event.published`,
  `event.unpublished`, `event.cancelled`
- `ticket_type.created`, `ticket_type.updated` (with price and quantity changes), `ticket_type.deleted`
- `paid_sales.activated`, `payment.succeeded`, `payment.failed`, `refund.succeeded`,
  `registration.cancelled`
- `staff.assigned`, `staff.removed`, `campaign.created`, `campaign.updated`, `campaign.enabled`,
  `campaign.disabled`
- `check_in.recorded`, `check_in.reversed`, `support.status_changed` and `support.assigned` (cases
  linked to the event)
- `paid_sales.requested`, `paid_sales.approved`, `paid_sales.rejected`, `payout.scheduled`,
  `payout.on_hold`, `payout.paid`
- `event.suspended`, `event.restored`, `paid_sales.suspended`, `paid_sales.restored`,
  `report.dismissed`, `report.actioned`, `dispute.opened`, `dispute.refunded`, `dispute.rejected`

`entityId` may be `null` for entries without a single affected record. Entries are append-only; no
route edits or deletes them. Organizer profile changes (`organizer.created`, `organizer.updated`), user
suspensions (`user.suspended`, `user.restored`) and platform settings changes (`settings.updated`)
are also audited; they belong to no event, so they do not appear in event timelines.

**Duplicate** (`POST .../duplicate`): creates a new `draft` owned by the same organizer with the
event's details, capacity, refund policy and dates, and copies of its ticket types (sales windows
cleared). Registration windows are cleared too. Orders, tickets, payments, check-ins, campaigns,
staff and support cases are not copied. Works for events in any state.

## Platform administration

SRS 4.12 and 4.5. Every `/admin` route needs a session with role `admin` (else 403 `forbidden`).

**Admin accounts**: list emails in `ADMIN_EMAILS`. On start the API promotes each listed account that
exists and has role `customer`; create the account first (sign in once), then restart the API.
Organizer accounts are not promoted.

**Suspension**

- `POST /admin/users/:userId/suspend` and `/restore` → `{user}` (AdminUser). A suspended account is
  signed out everywhere, every token is rejected (401) and signing in returns 403
  `account_suspended`. Admin accounts cannot be suspended (403 `forbidden`); suspending a suspended
  user, or restoring an active one, is 409 `invalid_state`.
- `POST /admin/events/:id/suspend` and `/restore` → `{event}`. Only a `published` event can be
  suspended; restoring returns it to `published` (409 `invalid_event_state` otherwise). A suspended
  event disappears from public routes, cannot be sold, paid for or checked in, and stays readable and
  editable by its organizer, who can still cancel it.
- `POST /admin/events/:id/paid-sales/approve` (`{note?}`) and `/reject` (`{note}`, required) → 204.
  Reviews a `pending` activation request (409 `invalid_state` otherwise).
- `POST /admin/events/:id/paid-sales/suspend` and `/restore` → 204. Suspends or restores the event's
  paid sales; buyers then get `paid_sales_inactive` and the organizer `paid_sales_suspended`
  (409 `invalid_state` when the activation is not in the required state).

**Search and monitoring** (each up to 200 rows, newest first):

| Method | Path | Filters | Response |
|---|---|---|---|
| GET | `/admin/users` | `q` (email, name or ID) | `{users}`: AdminUser |
| GET | `/admin/events` | `q` (title, venue or ID), `status` | `{events}` |
| GET | `/admin/events/:id/activity` | `from`, `to`, `type` | `{activity}`: the event timeline |
| GET | `/admin/orders` | `q` (order ID, buyer or attendee email, event title), `status` | `{orders}`: OrganizerOrder |
| GET | `/admin/payments` | `q`, `status` (`failed` lists payment failures) | `{payments}`: AdminPayment |
| GET | `/admin/refunds` | — | `{refunds}`: AdminRefund |
| GET | `/admin/disputes` | `status` (`open`, `refunded`, `rejected`) | `{disputes}`: Dispute |
| POST | `/admin/disputes/:disputeId/resolve` | `{decision, note}` | `{dispute}` |
| GET | `/admin/paid-sales` | `status` (`pending`, `active`, `suspended`, `rejected`) | `{paidSales}`: AdminActivation |
| GET | `/admin/payouts` | `status` (`scheduled`, `on_hold`, `paid`) | `{payouts}`: Payout |
| POST | `/admin/payouts/schedule` | — | `{payouts}`: payouts created by this run |
| POST | `/admin/payouts/:payoutId/mark-paid` | — | `{payout}`: scheduled → paid |
| POST | `/admin/payouts/:payoutId/hold` | — | `{payout}`: scheduled → on_hold |
| POST | `/admin/payouts/:payoutId/release` | — | `{payout}`: on_hold → scheduled |
| GET | `/admin/campaigns` | — | `{campaigns}`: every campaign with stats |
| GET | `/admin/reports` | `status` (`open`, `dismissed`, `actioned`) | `{reports}`: EventReport |
| POST | `/admin/reports/:reportId/resolve` | `{status}`: `dismissed` or `actioned` | `{report}` |
| GET | `/admin/settings` | — | `{settings}` |
| PUT | `/admin/settings` | `{activationFeeAmount, processingFeePercent}` | `{settings}` |
| GET | `/admin/export/:kind` | `kind`: `orders`, `payments`, `refunds` | CSV download, up to 10000 rows |

Escalated support cases are handled through `/support/cases`, where admins see every case.

**AdminUser**: `{ id, email, displayName, role, organizerId, emailVerified, suspendedAt, createdAt }`.

**AdminPayment**: `{ id, orderId, eventId, eventTitle, buyerEmail, amount, status, demo, createdAt }`.

**AdminRefund**: `{ id, orderId, eventId, eventTitle, amount, status, reason, createdBy, demo,
createdAt }` (`createdBy` is the refunding user's email).

**Payouts** (SRS 4.10, 6): simulated organizer payouts; no money moves. `POST /admin/payouts/schedule`
creates one payout per ended event (after `endsAt`) whose paid sales were activated and that has no
payout yet. `amount` is gross minus refunds minus processing fees (never below 0) and
`payoutAccountReference` is copied from the organizer profile. A payout starts `on_hold` when the
event or its paid sales are suspended or a dispute is open, otherwise `scheduled`. Admins then mark it
`paid` or move it between `scheduled` and `on_hold` (409 `invalid_state` for any other change). Every
status change is in the event timeline (type `payout`). **Payout**: `{ id, eventId, eventTitle,
organizerName, grossAmount, refundedAmount, feeAmount, amount, payoutAccountReference, status, demo,
createdAt, paidAt }`.

**AdminActivation**: `{ eventId, eventTitle, organizerName, status, feeAmount, demo, activatedAt,
suspendedAt }`.

**Settings**: `{ activationFeeAmount, processingFeePercent, updatedAt }`. The fee is 0–10,000,000 KZT
and the percentage 0–20. New values apply to activations and orders created afterwards.

**Disputes** (SRS 2.3, 4.12): a basic dispute record; advanced dispute workflows are out of scope
(SRS 8). The buyer disputes a `paid` order with a total above 0 (409 `invalid_order_state` or
`free_order` otherwise; someone else's order is 404). `reason` is `not_received | event_cancelled |
not_as_described | duplicate_charge | other`, `details` up to 2000 characters. One open dispute per
order (409 `already_disputed`). An admin resolves it with `decision` `refund` (runs the full demo
refund; an order the organizer already refunded is just closed) or `reject`, and a `note` of 1–500
characters. Resolving a closed dispute is 409 `invalid_state`. **Dispute**: `{ id, orderId, eventId,
eventTitle, buyerEmail, amount, reason, details, status, resolutionNote, demo, createdAt, resolvedAt }`;
`status` is `open | refunded | rejected`. Opening and resolving appear in the event timeline (type
`dispute`).

**Event reports**: `POST /events/:id/report` takes `reason` (`fraud | inappropriate | misleading |
other`) and optional `details` (up to 2000 characters) for a published public or unlisted event
(404 otherwise). One open report per user and event (409 `already_reported`). **EventReport**:
`{ id, eventId, eventTitle, eventStatus, reporterEmail, reason, details, status, createdAt,
reviewedAt }`. Resolving an already resolved report is 409 `invalid_state`.

Admin suspensions, restorations and report decisions are written to the audit log; event-related ones
appear in that event's timeline (types `event`, `paid_sales`, `report`, `dispute`).

**CSV export**: comma-separated with a header row; money is whole KZT and times are RFC 3339 UTC.
Cells that start with `=`, `+`, `-`, `@`, tab or carriage return are prefixed with `'` so spreadsheet
apps do not run them as formulas.
