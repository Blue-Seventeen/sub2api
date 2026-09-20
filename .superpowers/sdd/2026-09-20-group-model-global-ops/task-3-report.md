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
