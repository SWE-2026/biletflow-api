---
paths: ["**/*.go"]
---
# Idiomatic Go
- gofmt clean; must pass `go vet`.
- Naming: MixedCaps, no underscores (except _test.go); short receivers; no stutter;
  no util/common/helper/model package names.
- Errors: wrap with %w and context; match with errors.Is/errors.As; handle once; panic only for
  irrecoverable states.
- Interfaces: accept interfaces, return structs; define them in the consumer package; don't add an
  interface before there is a real need (a second implementation or a test fake).
- Modern idioms within the go.mod floor: any, min/max, slices.*/maps.*, `for i := range n`,
  log/slog. Don't hand-roll what the stdlib provides.
