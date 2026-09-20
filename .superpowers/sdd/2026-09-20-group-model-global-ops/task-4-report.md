# Task 4 Report: Frontend Global Model Operation Workflow

## Status

Implemented the frontend workflow for group model-list global operations. Existing unrelated backend and frontend work was preserved; only the Task 4 frontend files, focused tests, and this report were changed or committed.

## Implemented

- Mounted `ModelListScopeDialog.vue` in `GroupsView.vue` and wired create/edit toolbar and row deletion actions.
- Preserved one-argument helper compatibility by defaulting model-list additions and deletions to `group` scope.
- Added `globalModelOperations` state to both create and edit model-list flows.
- Added global add intents through an editable draft and global remove intents for selected rows.
- Kept local model-list edits immediate while global operations remain pending until save.
- Applied case-insensitive last-operation-wins deduplication for pending global operations.
- Kept wildcard deletion exact: a wildcard is removed globally only when that wildcard row is selected.
- Added `global_model_operations` to create/update payloads through a shared payload builder.
- Preserved disabled-list behavior and never changed `models_list_config.enabled` as a side effect of global operations.
- Preserved pending operations on create/update failure; cleared them only after a successful save or platform change.
- Added platform-change warning and reset behavior for create/edit forms.
- Displayed successful global-operation summaries with target platform, operation/model names, and affected group count when the backend returns a summary.
- Updated English and Chinese model-list wording and added scope, warning, and summary locale keys.
- Re-exported the new helpers through the existing `groupModelAllowlist.ts` compatibility module.

## Tests Added/Extended

- `groupsModelsList.spec.ts` covers default group scope, global add, duplicate global add, selected exact/wildcard deletion, local deletion isolation, pending-operation retention, hidden-list payload behavior, and last-operation-wins deduplication.
- `ModelListScopeDialog.spec.ts` covers add and delete scope choices and delete-count wording.

## Verification

- `pnpm exec vitest run src/views/admin/__tests__/groupsModelsList.spec.ts src/components/admin/group/__tests__/ModelListScopeDialog.spec.ts`: 25 tests passed.
- `pnpm run check:i18n`: 3 tests passed.
- `pnpm run typecheck`: passed with exit code 0.
- `pnpm run test:run`: 285 test files passed, 2,037 tests passed.
- `git diff --check -- frontend`: passed.

The full suite emitted pre-existing test-path stderr such as simulated API failures, missing test fixture settings, and the Browserslist age notice. These did not cause failures.

## Concerns / Follow-up

- No live browser interaction or backend API E2E was run. The frontend relies on the Task 3 backend contract and optional `global_model_operation_summary` response shape already represented in the frontend types.
- The repository contains unrelated staged/unstaged/untracked changes, including backend work and `frontend/vitest-sync-*.json`; those were intentionally excluded from the Task 4 commit.
