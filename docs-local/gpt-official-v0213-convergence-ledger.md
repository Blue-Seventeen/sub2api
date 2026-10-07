# GPT v0.2.13 Convergence Ledger

Implementation baseline: `9e006bf34`
Official reference: `v0.2.13` (`3040209f205472038c1ba745a1bedd2edd9053b1`)
Rollback commit/image: `78fbfc1821b1a0943f27a103731e9e149f6942db` / `sub2api-custom:v0.2.13-78fbfc1`

## Gates

- [x] Create isolated implementation branch from the rollback commit.
- [x] Restore official GPT Chat/Responses route matrix without changing non-GPT routes.
- [x] Restore official protocol conversion, tool mapping, terminal/error handling, and WS recovery.
- [x] Preserve RecordUsage/UsageBillingCommand and exactly-once usage boundaries.
- [x] Add fake-upstream fault injection and local Responses/Chat/Messages/WS unit coverage.
- [x] Build and health-check the local container.
- [x] Complete local fake-upstream container E2E for Responses/Chat/Messages/WS and billing.
- [x] Run two independent final audits against the final worktree.
- [ ] Export a candidate package with `cloud_test: pending` only after every gate passes.

## Local E2E status (2026-10-07)

The rebuilt image `sub2api-custom:v0.2.13-gpt-converged-final-20261007` was
started against the existing named volumes (no `down -v`). A local fake
upstream on `host.docker.internal:18321` received all requests; the test
account was restored to its original production URL and route switches after
the run. The original script was not modified and retains SHA-256
`dd405b24ddbe8a29cef8f0428ada6a359ef0826e16c01e1a7565730be1389c89`.

The strict cross runner passed 28/28 POSTs and the four-mode smoke passed 4/4.
Each of the 32 HTTP requests has exactly one usage row, matching fake usage
(7 input, 2 output), positive cost, and `usage_estimated=false`. The Messages
and WebSocket runner passed both routes; each has one usage row with the same
tokens and positive observed cost. The final aggregate contains 34 usage rows
for account 17 with no duplicate request IDs. Group 35 rows used multiplier
1.0; group 64 rows used subscription multiplier 0.5. Local `/health` returned
HTTP 200 and the container became healthy after restoration.

After the independent protocol audit identified three P2 official-alignment
gaps, the final worktree restored the session-preemption health guard and
changed GPT scheduler reports to use the resolved upstream model and observed
failure error. The RED preemption test failed before the guard and passes after
it; the focused handler/routes/service suite and full backend suite were
rerun successfully. The final container E2E was rerun after rebuilding that
image: Responses/Chat 28/28 plus 4/4, Messages/WS 2/2, and all 34 usage rows
reconciled exactly once with positive observed cost.

The historical pre-fake run that reached the configured `api.hack3rx.top` host is
retained in `v0213-20261007-cross-results.json` for provenance only and is not
used as local acceptance evidence. It included three client timeouts and a WS
close with status 1013; no further real-upstream requests were sent after the
host was identified. The completed container E2E above uses only the local fake
upstream and does not claim cloud readiness.

The two final read-only audits reported no P0/P1 or blocking findings. The
protocol audit's former P2 findings were fixed and rechecked. The accepted
official semantic risk remains that a provider-billed failed attempt can be
dropped during failover and therefore may not enter local billing.

Local app, PostgreSQL, and Redis are healthy after restoring the temporary
settings; `/health` returned HTTP 200. The named-volume backup is at
`artifacts/local-volume-backups/20261007-082033`.

## Cleanup log

Temporary logs, Go build cache, and Docker build cache are removed after each
completed gate. Rollback images, audit reports, test results, and reproduction
scripts are retained.
