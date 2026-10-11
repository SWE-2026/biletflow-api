---
paths: ["**/*_test.go"]
---
# Testing
- Table-driven tests with t.Run subtests; t.Parallel() where safe; `tests`/`tt` naming with
  `give`/`want` fields.
- Unit tests (`make test-unit`) must not need a database or network.
- DB-backed tests call `newDBServer(t)`, which skips unless TEST_DB_DSN is set; `make test` sets it
  and starts a throwaway Postgres on :55433. Use `uniqueEmail` so reruns don't collide.
- Prefer testing through HTTP (`client.do`) so routing, auth, validation and SQL are covered together.
