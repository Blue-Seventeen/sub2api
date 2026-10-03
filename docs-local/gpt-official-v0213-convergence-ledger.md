# GPT v0.2.13 Convergence Ledger

Baseline: `78fbfc1821b1a0943f27a103731e9e149f6942db`
Official reference: `v0.2.13` (`3040209f205472038c1ba745a1bedd2edd9053b1`)
Rollback image: `sub2api-custom:v0.2.13-78fbfc1`

## Gates

- [x] Create isolated implementation branch from the rollback commit.
- [x] Restore official GPT Chat/Responses route matrix without changing non-GPT routes.
- [x] Restore official protocol conversion, tool mapping, terminal/error handling, and WS recovery.
- [x] Preserve RecordUsage/UsageBillingCommand and exactly-once usage boundaries.
- [x] Add fake-upstream fault injection and local Responses/Chat/Messages/WS coverage.
- [x] Build and health-check the local container.
- [ ] Run two independent final audits.
- [ ] Export a candidate package with `cloud_test: pending` only after every gate passes.

## Current blocker

The local container health check is HTTP 200 and protocol/WS unit coverage passes,
but the supplied `cross_retest.mjs` keys are rejected by the recovered local
database with application `401 INVALID_API_KEY` for all 28 POSTs. No upstream
relay result was observed, so local real-Key E2E acceptance remains incomplete.

## Cleanup log

Temporary logs, Go build cache, and Docker build cache are removed after each
completed gate. Rollback images, audit reports, test results, and reproduction
scripts are retained.
