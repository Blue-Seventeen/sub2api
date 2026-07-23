//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// upsertCapturingQuotaRepo 实现 service.UserPlatformQuotaRepository，捕获 UpsertForUser 调用。
type upsertCapturingQuotaRepo struct {
	service.UserPlatformQuotaRepository
	listRecords []service.UserPlatformQuotaRecord
	listErr     error
	upsertCalls []upsertCall
	upsertErr   error
	resetCalls  []resetCall
	resetErr    error
	snapshots   []service.UserPlatformQuotaSnapshot
	snapshotErr error
}

type upsertCall struct {
	userID  int64
	records []service.UserPlatformQuotaRecord
}
type resetCall struct {
	userID   int64
	platform string
	window   string
	newStart time.Time
}

func (r *upsertCapturingQuotaRepo) ListByUser(_ context.Context, _ int64) ([]service.UserPlatformQuotaRecord, error) {
	return r.listRecords, r.listErr
}
func (r *upsertCapturingQuotaRepo) UpsertForUser(_ context.Context, userID int64, records []service.UserPlatformQuotaRecord) error {
	cloned := make([]service.UserPlatformQuotaRecord, len(records))
	copy(cloned, records)
	r.upsertCalls = append(r.upsertCalls, upsertCall{userID: userID, records: cloned})
	return r.upsertErr
}
func (r *upsertCapturingQuotaRepo) ResetExpiredWindow(_ context.Context, userID int64, platform string, window string, newStart time.Time) error {
	r.resetCalls = append(r.resetCalls, resetCall{userID, platform, window, newStart})
	return r.resetErr
}
func (r *upsertCapturingQuotaRepo) BatchSnapshotUsage(_ context.Context, snapshots []service.UserPlatformQuotaSnapshot, _ time.Time) error {
	cloned := make([]service.UserPlatformQuotaSnapshot, len(snapshots))
	copy(cloned, snapshots)
	r.snapshots = append(r.snapshots, cloned...)
	return r.snapshotErr
}

// billingCacheStub 实现 service.BillingCache 中本测试关心的 Delete 方法；其他方法 panic。
type billingCacheStub struct {
	service.BillingCache
	deleteCalls  []deleteCall
	deleteErr    error
	batchGetKeys []service.UserPlatformQuotaKey
	batchEntries []*service.UserPlatformQuotaCacheEntry
	batchSeq     [][]*service.UserPlatformQuotaCacheEntry
	batchCalls   int
	batchGetErr  error
}

type guardBillingCacheStub struct {
	billingCacheStub
	beginCalls []deleteCall
	endCalls   []deleteCall
}

type deleteCall struct {
	userID   int64
	platform string
}

func (b *billingCacheStub) DeleteUserPlatformQuotaCache(_ context.Context, userID int64, platform string) error {
	b.deleteCalls = append(b.deleteCalls, deleteCall{userID, platform})
	return b.deleteErr
}

func (b *billingCacheStub) BatchGetUserPlatformQuotaCache(_ context.Context, keys []service.UserPlatformQuotaKey) ([]*service.UserPlatformQuotaCacheEntry, error) {
	b.batchGetKeys = append(b.batchGetKeys, keys...)
	if b.batchGetErr != nil {
		return nil, b.batchGetErr
	}
	entries := b.batchEntries
	if len(b.batchSeq) > 0 {
		idx := b.batchCalls
		if idx >= len(b.batchSeq) {
			idx = len(b.batchSeq) - 1
		}
		entries = b.batchSeq[idx]
	}
	b.batchCalls++
	out := make([]*service.UserPlatformQuotaCacheEntry, len(keys))
	copy(out, entries)
	return out, nil
}

func (b *guardBillingCacheStub) BeginUserPlatformQuotaCacheMutation(_ context.Context, userID int64, platform string, _ time.Duration) error {
	b.beginCalls = append(b.beginCalls, deleteCall{userID, platform})
	return nil
}

func (b *guardBillingCacheStub) EndUserPlatformQuotaCacheMutation(_ context.Context, userID int64, platform string) error {
	b.endCalls = append(b.endCalls, deleteCall{userID, platform})
	return nil
}

func buildTestHandler(repo service.UserPlatformQuotaRepository, cache service.BillingCache) *UserHandler {
	return &UserHandler{
		userPlatformQuotaRepo: repo,
		billingCache:          cache,
		adminService:          newStubAdminService(),
	}
}

func putReq(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodPut, "/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = []gin.Param{{Key: "id", Value: "42"}}
	return c, w
}

func allAllowedPlatformQuotasBody(t *testing.T) string {
	t.Helper()
	quotas := make([]PlatformQuotaInput, 0, len(service.AllowedQuotaPlatforms))
	for _, platform := range service.AllowedQuotaPlatforms {
		quota := PlatformQuotaInput{Platform: platform}
		switch platform {
		case service.PlatformAnthropic:
			daily, monthly := 10.0, 100.0
			quota.DailyLimitUSD = &daily
			quota.MonthlyLimitUSD = &monthly
		case service.PlatformOpenAI:
			daily, weekly := 80.0, 300.0
			quota.DailyLimitUSD = &daily
			quota.WeeklyLimitUSD = &weekly
		}
		quotas = append(quotas, quota)
	}
	raw, err := json.Marshal(UpdateUserPlatformQuotasRequest{Quotas: quotas})
	if err != nil {
		t.Fatalf("marshal quotas: %v", err)
	}
	return string(raw)
}

func TestUpdateUserPlatformQuotas_Success(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{}
	cache := &billingCacheStub{}
	h := buildTestHandler(repo, cache)

	body := allAllowedPlatformQuotasBody(t)
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.upsertCalls) != 1 {
		t.Fatalf("UpsertForUser should be called once, got %d", len(repo.upsertCalls))
	}
	if repo.upsertCalls[0].userID != 42 || len(repo.upsertCalls[0].records) != len(service.AllowedQuotaPlatforms) {
		t.Errorf("unexpected upsert call: %+v", repo.upsertCalls[0])
	}
	// 缓存失效：写库前后各清一次全部 platform，防止 flusher 读到旧 Redis 快照后覆盖 admin 写入。
	expectedDeleteCalls := len(service.AllowedQuotaPlatforms) * 2
	if len(cache.deleteCalls) != expectedDeleteCalls {
		t.Errorf("expected %d cache delete calls, got %d: %+v", expectedDeleteCalls, len(cache.deleteCalls), cache.deleteCalls)
	}
}

func TestUpdateUserPlatformQuotas_UsesMutationGuardWhenAvailable(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{}
	cache := &guardBillingCacheStub{}
	h := buildTestHandler(repo, cache)

	c, w := putReq(t, `{"quotas":[{"platform":"anthropic","daily_limit_usd":10}]}`)
	h.UpdateUserPlatformQuotas(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(cache.beginCalls) != len(service.AllowedQuotaPlatforms) {
		t.Fatalf("expected guard begin for all platforms, got %+v", cache.beginCalls)
	}
	if len(cache.endCalls) != len(service.AllowedQuotaPlatforms) {
		t.Fatalf("expected guard end for all platforms, got %+v", cache.endCalls)
	}
	if len(cache.deleteCalls) != len(service.AllowedQuotaPlatforms) {
		t.Fatalf("expected guarded cache delete after post-guard flush, got %+v", cache.deleteCalls)
	}
}

func TestUpdateUserPlatformQuotas_FlushesCachedUsageBeforeMutationGuard(t *testing.T) {
	now := time.Now().UTC()
	repo := &upsertCapturingQuotaRepo{}
	cache := &guardBillingCacheStub{
		billingCacheStub: billingCacheStub{
			batchSeq: [][]*service.UserPlatformQuotaCacheEntry{
				{
					{
						SchemaVersion:      service.UserPlatformQuotaCacheSchemaV1,
						DailyUsageUSD:      1.25,
						WeeklyUsageUSD:     2.5,
						MonthlyUsageUSD:    3.75,
						DailyWindowStart:   &now,
						WeeklyWindowStart:  &now,
						MonthlyWindowStart: &now,
					},
				},
				{
					{
						SchemaVersion:      service.UserPlatformQuotaCacheSchemaV1,
						DailyUsageUSD:      1.75,
						WeeklyUsageUSD:     3.0,
						MonthlyUsageUSD:    4.25,
						DailyWindowStart:   &now,
						WeeklyWindowStart:  &now,
						MonthlyWindowStart: &now,
					},
				},
			},
		},
	}
	h := buildTestHandler(repo, cache)

	c, w := putReq(t, `{"quotas":[{"platform":"anthropic","daily_limit_usd":10}]}`)
	h.UpdateUserPlatformQuotas(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(cache.batchGetKeys) != len(service.AllowedQuotaPlatforms)*2 {
		t.Fatalf("expected preflush and post-guard batch get for all platforms, got %+v", cache.batchGetKeys)
	}
	if len(repo.snapshots) != 2 {
		t.Fatalf("expected preflush and post-guard cached usage snapshots, got %+v", repo.snapshots)
	}
	first := repo.snapshots[0]
	if first.UserID != 42 || first.Platform != service.AllowedQuotaPlatforms[0] {
		t.Fatalf("unexpected first snapshot key: %+v", first)
	}
	if first.DailyUsageUSD != 1.25 || first.WeeklyUsageUSD != 2.5 || first.MonthlyUsageUSD != 3.75 {
		t.Fatalf("unexpected first snapshot usage: %+v", first)
	}
	second := repo.snapshots[1]
	if second.UserID != 42 || second.Platform != service.AllowedQuotaPlatforms[0] {
		t.Fatalf("unexpected second snapshot key: %+v", second)
	}
	if second.DailyUsageUSD != 1.75 || second.WeeklyUsageUSD != 3.0 || second.MonthlyUsageUSD != 4.25 {
		t.Fatalf("unexpected second snapshot usage: %+v", second)
	}
	if len(cache.beginCalls) != len(service.AllowedQuotaPlatforms) {
		t.Fatalf("expected guard begin after preflush, got %+v", cache.beginCalls)
	}
	if len(cache.deleteCalls) != len(service.AllowedQuotaPlatforms) {
		t.Fatalf("expected guarded cache delete after post-guard flush, got %+v", cache.deleteCalls)
	}
}

func TestUpdateUserPlatformQuotas_RejectsDuplicatePlatform(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	body := `{"quotas":[
		{"platform":"anthropic","daily_limit_usd":1},
		{"platform":"anthropic","daily_limit_usd":2}
	]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_RejectsInvalidPlatform(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	body := `{"quotas":[{"platform":"unknown","daily_limit_usd":1}]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_RejectsNegativeLimit(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	body := `{"quotas":[{"platform":"anthropic","daily_limit_usd":-1}]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_RejectsTooManyEntries(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	quotas := make([]PlatformQuotaInput, 0, len(service.AllowedQuotaPlatforms)+1)
	for _, platform := range service.AllowedQuotaPlatforms {
		quotas = append(quotas, PlatformQuotaInput{Platform: platform})
	}
	quotas = append(quotas, PlatformQuotaInput{Platform: service.PlatformAnthropic})
	raw, err := json.Marshal(UpdateUserPlatformQuotasRequest{Quotas: quotas})
	if err != nil {
		t.Fatalf("marshal quotas: %v", err)
	}
	body := string(raw)
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_ReturnsLatestState(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{
		listRecords: []service.UserPlatformQuotaRecord{
			{UserID: 42, Platform: "anthropic"},
		},
	}
	cache := &billingCacheStub{}
	h := buildTestHandler(repo, cache)

	body := `{"quotas":[{"platform":"anthropic","daily_limit_usd":10}]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if !strings.Contains(w.Body.String(), `"platform_quotas"`) {
		t.Errorf("response should contain platform_quotas array: %s", w.Body.String())
	}
}

// ───────── T4: Reset 测试 ─────────

func postReq(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = []gin.Param{{Key: "id", Value: "42"}}
	return c, w
}

func TestResetUserPlatformQuotaWindow_Success(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{}
	cache := &billingCacheStub{}
	h := buildTestHandler(repo, cache)
	body := `{"platform":"anthropic","window":"daily"}`
	c, w := postReq(t, body)
	h.ResetUserPlatformQuotaWindow(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.resetCalls) != 1 {
		t.Fatalf("ResetExpiredWindow should be called once, got %d", len(repo.resetCalls))
	}
	if repo.resetCalls[0].userID != 42 ||
		repo.resetCalls[0].platform != "anthropic" ||
		repo.resetCalls[0].window != "daily" {
		t.Errorf("unexpected reset call: %+v", repo.resetCalls[0])
	}
	if len(cache.deleteCalls) != 2 ||
		cache.deleteCalls[0].userID != 42 ||
		cache.deleteCalls[0].platform != "anthropic" {
		t.Errorf("expected 2 cache deletes for anthropic, got %+v", cache.deleteCalls)
	}
	if cache.deleteCalls[1].userID != 42 || cache.deleteCalls[1].platform != "anthropic" {
		t.Errorf("expected second cache delete for anthropic, got %+v", cache.deleteCalls)
	}
}

func TestResetUserPlatformQuotaWindow_RejectsInvalidWindow(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	c, w := postReq(t, `{"platform":"anthropic","window":"yearly"}`)
	h.ResetUserPlatformQuotaWindow(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestResetUserPlatformQuotaWindow_RejectsInvalidPlatform(t *testing.T) {
	h := buildTestHandler(&upsertCapturingQuotaRepo{}, &billingCacheStub{})
	c, w := postReq(t, `{"platform":"unknown","window":"daily"}`)
	h.ResetUserPlatformQuotaWindow(c)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestResetUserPlatformQuotaWindow_NotFound(t *testing.T) {
	// handler 检查 service.ErrUserPlatformQuotaNotFound（由 adapter 包装而来）
	repo := &upsertCapturingQuotaRepo{resetErr: service.ErrUserPlatformQuotaNotFound}
	h := buildTestHandler(repo, &billingCacheStub{})
	c, w := postReq(t, `{"platform":"anthropic","window":"daily"}`)
	h.ResetUserPlatformQuotaWindow(c)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_JSONErrorOnRepoFailure(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{upsertErr: errors.New("db down")}
	cache := &billingCacheStub{}
	h := buildTestHandler(repo, cache)
	body := `{"quotas":[{"platform":"anthropic","daily_limit_usd":10}]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code < 500 {
		t.Errorf("expected 5xx, got %d", w.Code)
	}
	// 返回 JSON 错误响应
	var body2 map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body2); err != nil {
		t.Errorf("expected JSON error body, got: %s", w.Body.String())
	}
}

func TestUpdateUserPlatformQuotas_UserNotFound(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{}
	cache := &billingCacheStub{}
	adminSvc := newStubAdminService()
	adminSvc.getUserErr = service.ErrUserNotFound
	h := &UserHandler{
		userPlatformQuotaRepo: repo,
		billingCache:          cache,
		adminService:          adminSvc,
	}
	body := `{"quotas":[{"platform":"anthropic","daily_limit_usd":10}]}`
	c, w := putReq(t, body)
	h.UpdateUserPlatformQuotas(c)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when user not found, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResetUserPlatformQuotaWindow_UserNotFound(t *testing.T) {
	repo := &upsertCapturingQuotaRepo{}
	cache := &billingCacheStub{}
	adminSvc := newStubAdminService()
	adminSvc.getUserErr = service.ErrUserNotFound
	h := &UserHandler{
		userPlatformQuotaRepo: repo,
		billingCache:          cache,
		adminService:          adminSvc,
	}
	c, w := postReq(t, `{"platform":"anthropic","window":"daily"}`)
	h.ResetUserPlatformQuotaWindow(c)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when user not found, got %d: %s", w.Code, w.Body.String())
	}
}
