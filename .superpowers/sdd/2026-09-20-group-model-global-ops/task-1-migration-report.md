# Task 1 Migration 238 Integration Evidence

## Follow-up Test

Added repository-level integration coverage for migration 238. The test uses
the existing `testTx` helper and embedded `migrations.FS`, covers differing
enabled, empty, disabled, equal, and soft-deleted group policies, executes the
migration twice in one transaction, and verifies canonical values after both
runs. The second run is compared with the complete first-run state to prove it
is harmless.

## Changed Files

- `backend/internal/repository/group_model_allowlist_sync_migration_integration_test.go`
- `.superpowers/sdd/2026-09-20-group-model-global-ops/task-1-migration-report.md`

Migrations 235/236, migration 238 SQL, and unrelated work were not changed.

## Verification

Command:

```powershell
go test -tags=integration ./internal/repository -run 'TestMigration238' -count=1
```

Result (exit code 0):

```text
ok  	github.com/Wei-Shaw/sub2api/internal/repository	5.553s
```

Command:

```powershell
go test ./internal/repository -run '^$' -count=1
```

Result (exit code 0):

```text
ok  	github.com/Wei-Shaw/sub2api/internal/repository	0.356s [no tests to run]
```

Command:

```powershell
git diff --check
```

Result (exit code 0): no output.
