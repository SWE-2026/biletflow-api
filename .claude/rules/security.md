# Security
- Secrets: never read, print, log or embed values from .env, .env.* or any credentials file.
  Reference them via config; never echo actual values in chat or logs.
- Never log request/response bodies, tokens, one-time codes, QR tokens, passwords, card/payment
  data or auth headers.
- Money is always an integer (whole KZT), never float64. Prices and discounts are computed on the
  server only; never trust amounts sent by a client (SRS 4.14).
- Passwords: bcrypt only. Session tokens and one-time codes are stored hashed.
- SQL: parameterized queries only, never string-built SQL with interpolated values.
- Validate input at every trust boundary (request bodies, path/query params, webhook payloads):
  shape and bounds before use.
- Errors returned to clients never include internal error strings or raw DB errors.
- Idempotency: payment/webhook handlers dedupe on the provider's own ID before side effects.
  Never blind-retry a charge.
- Outbound HTTP to third parties sets an explicit timeout; never use the zero-value http.Client.
- Webhooks (payment sandbox, email provider) verify the provider signature before processing.
- Demo payments are simulations and must be labelled as such in every response and UI.
- Dependencies: run `govulncheck ./...` after adding or updating deps.
- Deploy gate: never push to main, deploy, or run migrations against a shared database without
  explicit confirmation in the session.
