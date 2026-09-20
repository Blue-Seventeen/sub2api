# Task 3 Report: Atomic Global Group Model Operations

## Status

Implemented the backend Task 3 flow. Existing canonical-policy, auth-cache,
subscription, deployment, and frontend worktree changes were preserved.

## Implementation

- Added `GroupModelOperation` and `GlobalModelOperationSummary` service types.
- Added global operation fields to create/update service inputs and admin
  request DTOs.
- Added an optional `GlobalModelOperationsRepository` capability so mandatory
  `GroupRepository` and existing test doubles remain unchanged.
- Added concrete repository create/update methods backed by one Ent
  transaction. The transaction locks all non-soft-deleted groups on the final
  platform in ascending ID order, including inactive and subscription groups.
- Global operations are applied in request order with case-insensitive exact
  matching. Additions preserve requested spelling, do not match wildcard
  entries, and removals delete a wildcard only when that wildcard is the exact
  requested model.
- The canonical `models_list_config` JSON and compatibility `model_allowlist`
  JSON are written together. The existing `enabled` value is preserved.
- One `group_changed` scheduler outbox row is written per changed target, in
  the same transaction. Any operation, persistence, or outbox failure rolls
  back the transaction.
- Create/update service flows invoke the optional capability only when global
  operations are present. Update rejects global operations combined with a
  platform change. Normal paths and account-copy handling remain unchanged.
- Auth caches are invalidated for every affected group after commit.
- Admin responses expose `global_model_operation_summary` only when a global
  operation path was used.

## Tests

Passed:

```text
go test ./internal/service -run 'TestApplyGlobalModelOperations' -count=1
go test ./internal/repository ./internal/handler/admin ./internal/handler/dto -run '^$' -count=1
```

The full service package command was also run:

```text
go test ./internal/service -count=1
```

It failed in two pre-existing canonical-policy/auth-cache expectation tests:

- `TestAPIKeyAuthSnapshotProfitControlRoundtrip` expects auth snapshot v21 but
  the current worktree produces v22.
- `TestAPIKeyAuthSnapshotPreservesPricingAndAccessPolicy` expects the legacy
  model allowlist mirror while the current canonical-policy worktree produces
  an empty legacy field.

Those failures are outside Task 3 and were not changed.

Database integration tests requiring the repository integration environment
were not run in this checkout.

## Files Changed For Task 3

- `backend/internal/service/group_model_allowlist.go`
- `backend/internal/service/group_model_global_ops_test.go`
- `backend/internal/service/group_service.go`
- `backend/internal/service/admin_group.go`
- `backend/internal/service/group.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/repository/group_repo.go`
- `backend/internal/handler/admin/group_handler.go`
- `backend/internal/handler/dto/types.go`
- `backend/internal/handler/dto/mappers.go`

## Fix Round 1

### Status

Completed. The reviewer findings and the follow-up compile blocker were fixed
without changing frontend, subscription, deployment, or migrations 235/236.

### Changes

- Routed runtime model policy checks through `EffectiveModelPolicy()` in
  `openai_codex_models_service.go`, `openai_models_list.go`, and
  `model_plaza_service.go`. The model-plaza read is a runtime model-admission
  filter, so it is part of the canonical policy surface rather than a legacy
  compatibility projection.
- Declared the canonical `policy` in `MergeGroupConfiguredCodexModels` and used
  it for merge and pinned-order operations.
- Updated global create/update repository operations to copy the final
  canonical policy back to both `groupIn.ModelsListConfig` and
  `groupIn.ModelAllowlist` after commit. The enabled value is preserved.
- Kept one scheduler outbox event for every changed global target and emitted
  the current-group event only when the current group was not already in the
  affected target set. This avoids duplicate current-group events while
  preserving normal create/update event semantics.
- Added request presence plumbing with `ModelsListConfigSet`. An explicitly
  supplied empty or disabled `models_list_config` now wins over
  `model_allowlist`; the legacy alias is used only when the canonical field is
  absent.
- Added canonical-vs-mirror regression coverage for Codex listing, OpenAI
  models listing, and model plaza filtering, plus presence, in-memory
  synchronization, scope, repeated-operation, wildcard, outbox-count, and
  outbox-rollback tests.

### Changed Files

- `.superpowers/sdd/2026-09-20-group-model-global-ops/task-3-report.md`
- `backend/internal/handler/admin/group_handler.go`
- `backend/internal/handler/admin/group_handler_sync_v024_test.go`
- `backend/internal/repository/group_repo.go`
- `backend/internal/repository/group_repo_integration_test.go`
- `backend/internal/service/admin_group.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/service/admin_service_group_model_allowlist_test.go`
- `backend/internal/service/model_plaza_service.go`
- `backend/internal/service/model_plaza_canonical_policy_test.go`
- `backend/internal/service/openai_codex_models_service.go`
- `backend/internal/service/openai_codex_models_service_test.go`
- `backend/internal/service/openai_models_list.go`
- `backend/internal/service/openai_models_list_test.go`
- `backend/internal/service/group_model_policy_test.go`

### Verification

Commands and observed outputs:

```text
go test ./internal/service ./internal/repository ./internal/handler/admin ./internal/handler/dto -run '^$' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service       0.666s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/repository    0.424s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/handler/admin 0.415s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/handler/dto   0.518s [no tests to run]

go test ./internal/service -run 'Test(EffectiveModelPolicy|FilterCodexModelIDsForGroup|OpenAI.*Canonical|ModelPlaza.*Canonical|ApplyGlobalModelOperations)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service 0.645s

go test ./internal/handler/admin -run 'Test.*(ModelsList|Global|Create)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/handler/admin 0.508s

go test -tags=integration ./internal/repository -run 'TestGroupRepoSuite/TestGlobalModelOperationsScopesTargetsAndWritesOneEventPerAffectedGroup|TestGlobalModelOperationsRollBackWhenOutboxFails' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/repository 9.223s

git diff --check
exit code 0
```

The integration command ran against the configured repository integration
database and covered same-platform scope, inactive and subscription groups,
soft-delete and other-platform exclusion, exact removal with wildcard
preservation, repeated operations with last-operation-wins behavior, one
outbox row per affected group, and rollback when outbox persistence fails.

### Concerns

- The `go test -tags=unit` service command remains unavailable because the
  broader worktree has unrelated duplicate helpers, undefined platform
  constants, and stale pricing-test signatures. The normal service suite and
  all Task 3 focused tests pass without that tag.

## Fix Round 2

### Status

Completed. All Important findings were addressed, and the low-risk Minor
findings were also fixed without changing migrations 235/236 or unrelated
subscription, deployment, promotion, proxy, scheduler, or frontend work.

### Changes

- `UpdateWithGlobalModelOperations` locks every live target group in ascending
  ID order before updating the current row or applying global mutations.
- Create-path global-operation auth caches are invalidated immediately after
  the committed repository operation, before account filtering or binding can
  fail.
- `EffectiveModelPolicy` treats hydrated groups, including explicit empty
  `models_list_config`, as canonical. Legacy-only auth-cache snapshots are
  recognized through `models_list_config_present` and materialized only in
  memory, so stale mirrors cannot revive persisted empty policy.
- Batch image, Gemini v1beta, model-allowlist middleware/handler, and related
  runtime paths use `EffectiveModelPolicy` for admission/listing decisions.
- `AffectedGroupIDs` is service-internal and is excluded from JSON responses.
- Rollback coverage mutates both the current group and a second global target,
  then verifies both JSON columns and all outbox rows roll back on failure.
- Empty or unsupported global operations return structured HTTP 400 errors.

### Changed Files

- `backend/internal/handler/batch_image_handler.go`
- `backend/internal/handler/gemini_v1beta_handler.go`
- `backend/internal/handler/model_allowlist.go`
- `backend/internal/repository/api_key_repo.go`
- `backend/internal/repository/group_repo.go`
- `backend/internal/repository/group_repo_integration_test.go`
- `backend/internal/server/middleware/group_model_allowlist.go`
- `backend/internal/service/admin_group.go`
- `backend/internal/service/admin_service_group_model_allowlist_test.go`
- `backend/internal/service/api_key_auth_cache_impl.go`
- `backend/internal/service/group_model_allowlist.go`
- `backend/internal/service/group_model_global_ops_create_test.go`
- `backend/internal/service/group_model_global_ops_test.go`
- `backend/internal/service/group_model_policy_test.go`
- `.superpowers/sdd/2026-09-20-group-model-global-ops/task-3-report.md`

### Verification

The new old-snapshot regression was verified red before implementation:

```text
go test ./internal/service -run 'TestAPIKeyAuthSnapshot(LegacyMirrorRemainsEffectiveWhenCanonicalIsAbsent|HydratedEmptyCanonicalDoesNotRestoreLegacyMirror)' -count=1
FAIL: TestAPIKeyAuthSnapshotLegacyMirrorRemainsEffectiveWhenCanonicalIsAbsent
expected legacy-only policy, got empty policy
```

It passed after the cache reconstruction fix:

```text
go test ./internal/service -run 'TestAPIKeyAuthSnapshot(LegacyMirrorRemainsEffectiveWhenCanonicalIsAbsent|HydratedEmptyCanonicalDoesNotRestoreLegacyMirror)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service 0.461s
```

Additional covering commands and observed outputs:

```text
go test ./internal/service -run 'Test(ApplyGlobalModelOperations|GroupEffectiveModelPolicy|GroupModelPolicy|APIKeyAuthSnapshot|ModelPlaza.*Canonical|OpenAI.*Canonical|FilterCodexModelIDsForGroup)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service 0.441s

go test ./internal/service -run 'TestCreateGroupGlobalOperationsInvalidateCachesBeforeAccountCopyFailure' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service 0.452s

go test ./internal/handler -run 'Test.*(BatchImage|Gemini|ModelAllowlist|Model)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/handler 1.362s

go test ./internal/handler/admin -run 'Test.*(ModelsList|Global|Create)' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/handler/admin 1.804s

go test ./internal/server/middleware -run 'TestGroupModelAllowlist' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/server/middleware 0.414s

go test ./internal/repository -run '^$' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/repository 0.326s [no tests to run]

go test -tags=integration ./internal/repository -run 'TestGroupRepoSuite/TestGlobalModelOperationsScopesTargetsAndWritesOneEventPerAffectedGroup|TestGlobalModelOperationsRollBackWhenOutboxFails' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/repository 7.902s

go test ./internal/service -count=1
ok  github.com/Wei-Shaw/sub2api/internal/service 132.486s

go test ./internal/handler ./internal/handler/admin ./internal/server/middleware -run '^$' -count=1
ok  github.com/Wei-Shaw/sub2api/internal/handler 0.390s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/handler/admin 0.263s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/server/middleware 0.292s [no tests to run]

git diff --check
exit code 0
```

The integration tests verified same-platform scope, inactive and subscription
groups, soft-delete and other-platform exclusion, exact wildcard behavior,
last-operation-wins ordering, one outbox row per affected group, and rollback
of current and target policy JSON plus outbox rows. The full normal service
suite also passed.
