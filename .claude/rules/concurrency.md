---
paths: ["**/*.go"]
---
# Concurrency
- context.Context is the FIRST param; never store it in a struct.
- Always defer cancel(); the context owner handles cancellation.
- No goroutine leaks: every goroutine has a known end and a way to wait.
- Run DB-backed tests with -race (`make test` does).
- Inventory, seat holds, promo redemptions and check-ins race by nature. Enforce them in one
  transaction with row locks or a guarded UPDATE (`... WHERE status='valid'`), never with a
  read-then-write in Go.
  Add a concurrent test when you add such a rule.
