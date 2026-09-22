# Affiliate Inertness Gate Report

- Date: 2026-09-23
- Scope: v0.2.4 closeout Affiliate preservation gate
- Contract: `README-CUSTOM.md` requires upstream Affiliate to remain inert; Promotion is the only referral and rebate system.
- Audit input: `.superpowers/sdd/2026-09-22-deployment-closure/agent-static-audit.md`

## Result

The Affiliate preservation gate is implemented in the backend paths reviewed by the static audit. Compatibility DTOs, interfaces, constructor arguments, OAuth `aff_code` parameters, and existing shims remain available, but Affiliate cannot bind users, accrue or settle rebates, transfer or expose Affiliate ledger balances, or become enabled through settings.

Promotion wiring was not changed. No model-policy, Docker, frontend, migration, release, or deployment files were changed for this task, and Task1 tests were not processed.

## Changes

### User binding

- `backend/internal/service/auth_service.go:974` keeps `bindOAuthAffiliate` as a no-op compatibility shim. Existing email and OAuth callers can continue passing legacy `aff_code` values, but the shim performs no repository access.
- `backend/internal/service/auth_oauth_email_flow.go:301` and `backend/internal/service/auth_email_oauth_auto.go:206` retain call compatibility only; both reach the inert shim.
- The previously skipped OAuth completion test now runs and asserts no Affiliate repository bind or profile initialization.

### Accrual and settlement

- `backend/internal/service/affiliate_compat.go:97-108` makes `IsEnabled` permanently false and makes `AccrueInviteRebateForOrder` return `(0, nil)` without touching the repository.
- `backend/internal/service/redeem_service.go:556-560` no longer invokes Affiliate accrual after balance redemption. The variadic Affiliate constructor argument remains for source compatibility.
- `backend/internal/service/payment_fulfillment.go` no longer invokes Affiliate settlement from balance or subscription fulfillment. The Affiliate audit-claim and settlement helper implementation was removed.
- `backend/internal/service/admin_user.go:737-771` no longer includes Affiliate entries in balance history. The Affiliate balance-history SQL query and count path were removed; the legacy merge argument remains accepted but is ignored.
- `backend/internal/service/admin_user.go:606-609` preserves the existing admin recharge compatibility hook as an explicit no-op.

### Settings and compatibility views

- `backend/internal/service/setting_features.go:107-116` always reports Affiliate and Affiliate admin-recharge as disabled.
- `backend/internal/service/setting_parse.go:426,838` forces both compatibility settings fields false when reading persisted settings.
- `backend/internal/service/setting_public.go:380` forces the public compatibility field false.
- `backend/internal/service/setting_update.go:40-44,120-124,415,474` normalizes Affiliate fields before every system-settings write path and persists `false`, including combined auth-source settings updates.
- DTOs and setting keys remain present so older clients and upstream-compatible code continue to compile and receive stable disabled fields.

## Test updates

Tests were changed only where they asserted behavior prohibited by the fork contract:

- `backend/internal/service/payment_fulfillment_test.go`: subscription fulfillment completes without Affiliate accrual or Affiliate rebate audit creation.
- `backend/internal/service/redeem_admin_fulfillment_test.go`: admin fulfillment bypasses redemption rate limits without Affiliate accrual.
- `backend/internal/service/admin_balance_history_test.go`: Affiliate transfer records are ignored by the compatibility merge helper.
- `backend/internal/service/setting_service_update_test.go`: persisted or stored `true` Affiliate settings remain disabled and are written as `false`.
- `backend/internal/handler/auth_email_oauth_test.go`: the previously skipped Affiliate-code completion case now verifies no bind calls.

## Validation evidence

Passed:

1. `go test -tags unit ./internal/service -run 'Test(ExecuteSubscriptionFulfillmentDoesNotApplyAffiliateRebate|AdminFulfillmentBypassesLimitAndKeepsAffiliateInert|MergeBalanceHistoryCodesIgnoresAffiliateTransfers|MergeBalanceHistoryCodesPaginatesRedeemHistoryOnly|SettingService_AffiliateCannotBeEnabled|SettingService_AffiliateAdminRechargeSetting)$' -count=1`
   Result: `ok github.com/Wei-Shaw/sub2api/internal/service`.
2. `go test ./internal/handler -run 'Test(EmailOAuthCallbackCreatesPasswordRegistrationSessionForNewEmail|CompleteEmailOAuthRegistrationUsesAffiliateCodeFromPendingSession)$' -count=1`
   Result: `ok github.com/Wei-Shaw/sub2api/internal/handler`.
3. `go test -tags unit ./internal/service -run 'TestAuthService|TestSettingService_Affiliate' -count=1`
   Result: `ok github.com/Wei-Shaw/sub2api/internal/service`.
4. `go test -tags unit ./internal/service -run 'Affiliate|affiliate' -count=1`
   Result: `ok github.com/Wei-Shaw/sub2api/internal/service`.
5. `go test ./internal/handler -run 'Affiliate|affiliate' -count=1`
   Result: `ok github.com/Wei-Shaw/sub2api/internal/handler`.
6. `git diff --check`
   Result: passed.

A concurrent pre-existing `go test ./...` process temporarily delayed one focused run. It was left untouched; only the focused processes started for this task were stopped before the final sequential runs.

## Residual concerns

- The full Go suite was not used as the Affiliate gate because the request called for focused tests and the worktree contains unrelated dirty changes.
- Affiliate repository interfaces and test stubs still expose upstream method names for compatibility. They are not wired to production mutation paths by this gate.
- The report does not claim broader release closure. The static audit's model-policy, Docker, deployment, and other half-implementation gates remain outside this task.
