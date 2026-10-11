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
| 404 | `not_found` |
| 409 | `email_taken` |
| 410 | `code_expired` (expired, already used, or never requested) |
| 429 | `resend_too_soon`, `too_many_attempts`, `rate_limited` |
| 500 | `internal` (details only in server logs) |

## Objects

**User**: `{ id, email, displayName, role, organizerId, emailVerified, hasPassword, createdAt }`.
`role` is `customer | organizer | staff | admin`.

**Session**: `{ token, user }`.

**Challenge**: `{ expiresInSeconds, resendInSeconds, demoCode? }`. `demoCode` is present only when the
API runs with `APP_ENV=local` (no email provider yet).

## Endpoints

Both sign-in styles reach the same account, matched by email. `/auth` is rate-limited per IP.

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/auth/otp/request` | `{email}` | 200 Challenge: emails a 6-digit sign-in code |
| POST | `/auth/otp/verify` | `{email, code}` | 200 Session: creates the account on first sign-in, marks the email verified |
| POST | `/auth/signup` | `{email, password, displayName}` | 201 Session |
| POST | `/auth/login` | `{email, password}` | 200 Session |
| POST | `/auth/logout` 🔒 | — | 204 |
| GET | `/me` 🔒 | — | `{user}` |
| GET | `/events` | — | `{events}`: published events |

Codes: valid 10 minutes, resend after 30 s, 5 attempts. Passwords: 8–72 characters.
