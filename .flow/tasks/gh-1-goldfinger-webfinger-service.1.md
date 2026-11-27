---
satisfies: [R1, R2, R3, R4, R5, R6, R7, R8, R9, R10, R11, R12]
---
# gh-1-goldfinger-webfinger-service.1 Implement Goldfinger WebFinger service

## Description
TBD

## Acceptance
Every R-ID in the parent spec's ## Acceptance Criteria is satisfied; judge this task against the spec's criteria directly.

## Done summary
Implemented the Goldfinger WebFinger service as a stdlib-only Go binary (R1-R12): `internal/config` (flags defaulting from env, validation), `internal/server` (WebFinger, `/healthz`, `/robots.txt`, embedded 404 page, `X-Robots-Tag`, slog request logging, server timeouts), `internal/app` (run loop, graceful shutdown, `--healthcheck`), and a thin `cmd/goldfinger` entrypoint.

Tests: table tests, rapid property tests (test-only dependency), direct assertions, and one integration test behind the `integration` build tag; all pass with `-race`. Ran the real binary with curl: two instances, 200/400/404/405, `rel` handling, `--healthcheck` exit codes, port-in-use message.

Review fixes: reject issuer URLs with credentials and never echo a rejected URL; one-line startup errors; exactly one `@` in an acct URI; `OPTIONS *` routed through the handler; dropped the unused server `Addr`. Declined: cgo-off build entry point (belongs to gh-2) and empty-env-as-unset (deliberate).

stage: impl-review - ran [97821aa..09333ec] NEEDS_WORK -> SHIP (model: gpt-6.2-sol)

stage: plan-sync - skipped(config: planSync.enabled != true)
## Evidence
- Commits: 857171e3344e1e0cc460cc47e2f4dc94de0da5a3, 09333ec0165647cd4c4e3b527b38a6bada88295c
- Tests: go test -race ./... && go test -race -tags integration ./...
- PRs: