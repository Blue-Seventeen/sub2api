# Managed proxy refresh binding fix implementation plan

> For agentic workers: Use subagent-driven-development for scoped implementation and requesting-code-review for independent review. Track checks below.

**Goal:** Keep account proxy bindings usable when a subscription rotates the address of an unambiguously identified node; report genuine missing/disabled nodes accurately and fail closed before SOCKS authentication.

**Architecture:** Within a per-subscription PostgreSQL row lock, reuse the existing node/proxy ID and local authentication for one-to-one matches by exact endpoint identity or unique trimmed name plus normalized protocol. Prefer the unique current node over obsolete source_missing copies when endpoints rotate back. Store the initial node_key as the stable identity of a reused row, update provider configuration, and preserve operator-disable state. Do not merge ambiguous names/protocols or automatically redirect accounts to another node/direct connection.

**Tech Stack:** Go, PostgreSQL, Redis, Vue 3, Vitest, Mihomo, Docker Compose.

## Constraints and evidence

- Continue codex/sync-v0.2.15 from c311f44f; local only, no production or remote push.
- User explicitly authorized planning and implementing a confirmed harmful bug; no additional design/deployment confirmation is needed.
- Account 48 direct.o10.top: direct and proxy 557 succeeded, subscription revision 3 -> 4 rotated the node to proxy 603 while account stayed on 557, old binding failed in 63 ms with local SOCKS authentication error, rebind to 603 succeeded.
- Preserve credentials, operator status, proxy expiry/fallback config, account IDs, balances, volumes and fixed application keys. Historical missing rows stay available for review, never auto-delete them.
- Incoming duplicate names/types, historical ambiguity, different subscriptions and protocol changes must not be guessed into one identity.
- Use temporary restricted PostgreSQL/Redis integration resources, never the business DB for regression tests.
- New admin field node_status is optional and one of active/inactive/source_missing. The repository optional batch method GetNodeStatesByProxyIDs(ctx context.Context, ids []int64) returns map[int64]service.ProxySubscriptionNode (ID, ProxyID, Status and UpdatedAt suffice). Existing interfaces and clients remain compatible.

## Task 1: Repository identity and concurrency

Files: backend/internal/repository/proxy_subscription_repo.go; new focused matcher/test files; proxy_subscription_repo_integration_test.go.

- [x] Add a failing PostgreSQL regression that binds an account, rotates server/port with the same unique name/type, and asserts stable proxy/node ID, username/password, updated raw config, unchanged binding and no extra proxy rows.
- [x] Add pure matching regressions for exact keys, endpoint reversion with historical source_missing rows, duplicate incoming names/types, ambiguous existing nodes, changed type, disabled canonical node, and disappeared/returned node.
- [x] Implement four-phase reserved matching: exact nonmissing matches first, unique current logical matches second, unused exact historical matches third, unique historical logical match last. Each existing row can be reused once. Keep the persisted node_key on logical reuse.
- [x] Lock the subscription row before reading current nodes, and use the same subscription-before-node lock order for operator node status changes. UpdateWithNodes stays within the same transaction.
- [x] Add GetNodeStatesByProxyIDs as one batched SQL query. Keep proxy rows and operator status intact; invalidate affected account upstream billing snapshots and enqueue scheduler changes when the network config or availability changes.
- [x] Verify independent connections serialize refresh, cancellation rolls back, refresh failure preserves old state, and disabled rows are not recreated/reactivated.

## Task 2: Runtime and admin API node state

Files: backend/internal/service/proxy.go, managed_proxy.go, managed_proxy_runtime.go, admin_proxy.go, backend/internal/handler/dto conversion; focused service tests.

- [x] Write failing tests for a running subscription containing a missing/disabled bound node, current identity acceptance and obsolete local authentication rejection.
- [x] Add optional per-proxy runtime resolver, check installed active node identities without an extra DB request per gateway request, and never turn unavailable proxy routing into direct routing. Publish the installed identity set atomically with runtime generation/port.
- [x] Use the optional batch repository method in admin list/get/snapshots, exposing node_status separately from subscription runtime status. Remove missing/disabled nodes from active account picker results.
- [x] Clear unavailable or stale latency/quality display evidence; do not show old A/100 as a current proof. Legacy subscription proxies without node rows preserve existing behavior.
- [x] Run focused service and DTO tests, retain manual proxy behavior and runtime lifecycle guarantees.

## Task 3: Frontend state and regression

Files: frontend/src/types/index.ts; frontend/src/views/admin/ProxiesView.vue; relevant locales; new focused component regression tests.

- [x] Write failing rendered tests where node_status=source_missing and runtime.status=running with old success/A100; assert explicit missing state and no stale successful quality display.
- [x] Add optional node_status type, show missing/disabled node state ahead of process running state, disable copying an unusable node URL, preserve editable operator state and connectivity retest controls.
- [x] Ensure status polling/snapshot merge preserves and refreshes node_status; normal active/manual proxies still render correctly.
- [x] Run focused component tests, typecheck, lint and build.

## Task 4: Review, verification and local application update

- [x] Request scoped independent reviews of each deliverable and the full final diff; resolve important findings.
- [x] Run Go default tests, unit-tag tests and vet with bounded parallelism; dedicated real PostgreSQL integration regressions; frontend test suite/typecheck/lint/build. Report exact coverage and failures.
- [ ] Commit the reviewed repair, build an image tagged sub2api-custom:v0.2.15-proxy-fix-<SHA8> with actual SHA in binary/OCI metadata.
- [ ] Back up the local Compose image configuration, update only the application image/container, preserve PostgreSQL/Redis/volumes/secrets and 127.0.0.1:8080. Roll back on failed health/runtime verification.
- [ ] On the updated container keep account 48 bound to proxy 603, prove actual InUser connection and successful model response without a binding write, refresh twice without rebinding, prove stable ID/auth and repeated success. Verify historical missing nodes have honest state and cannot appear as active picker entries.
- [ ] Keep one verified proxy binding on account 48 for the user; leave other account bindings unchanged. Revoke test admin sessions; deliver SHA, evidence, scope and rollback instructions.

## Review follow-up requirements

Independent review found four related boundary defects. The same local repair includes regression tests and fixes for:

- DeleteWithProxy locking the subscription before proxy writes, consistent with refresh and operator updates.
- Real admin operator status updates persisting proxy/node state in one transaction and reloading the runtime. Metadata edits preserve disabled nodes; an active write cannot invent a source_missing provider node.
- Separate latency/quality configuration epochs captured at probe start, validated against the installed runtime and current database state before caching or returning results. Late results and coarse historical quality timestamps must not be presented as evidence for a new configuration.
- DNS-only changes updating affected node epochs and invalidating account probe snapshots within the same subscription transaction. Genuine no-op refreshes preserve measurement epochs.

The first Windows all-check attempt encountered test executable file contention and Node filesystem lookup errors. Repeat those checks with bounded sequential Go execution and one Vitest fork; do not count interrupted runs as passing.

## Managed proxy account cache closure

Scoped final inspection confirmed that probe invalidation alone omits accounts without a stored probe. Redis account objects include Proxy and have no expiration; generic hydration can keep an inactive proxy after operator re-enable. OpenAI normally reads the selected account from PostgreSQL, but a post-slot cache replacement also compares account UpdatedAt.

- [x] Add real isolated PostgreSQL/Redis regressions for disable/re-enable without a probe, all live bound account types, unrelated/deleted exclusions, no-op suppression and transactional rollback.
- [x] Managed network or availability changes independently advance every live bound account configuration timestamp and publish the existing batched scheduler notification; keep manual proxy/probe behavior intact.
- [x] Review the bounded change and rerun affected integration/default/unit/vet checks before committing.

## Validation before delivery commit

- Real restricted PostgreSQL/Redis subscription, matcher and managed-cache regressions: PASS, repository-cache-acceptance-final.log (14.248s). Corrected RED recorded in cache-regression-red-corrected.log.
- Go default and unit-tag full suites and vet: PASS, sequential bounded run, logs suffix cache-acceptance-final.
- Frontend full suite: 383 files / 2947 tests PASS; latest node-state fixture 14/14 PASS; typecheck, lint and production build PASS.
- Independent final review: four original Important findings and managed cache P2 all addressed; no unresolved Important within this repair.
- Commit/image/local runtime/real upstream refresh acceptance remains the delivery phase. Results and rollback instructions are recorded in artifacts/proxy-refresh-fix-20261012/report.md after deployment.
