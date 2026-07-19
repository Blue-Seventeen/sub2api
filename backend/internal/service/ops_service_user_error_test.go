package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type stubOpsRepoForUserErr struct {
	OpsRepository // 嵌入接口，未实现的方法 panic，仅覆盖 ListErrorLogs
	gotFilter     *OpsErrorLogFilter

	// GetErrorLogByID 控制字段
	detailToReturn    *OpsErrorLogDetail
	detailErrToReturn error
}

type disabledOpsSettingRepo struct {
	SettingRepository
}

func (s *disabledOpsSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyOpsMonitoringEnabled {
		return "false", nil
	}
	return "", ErrSettingNotFound
}

func (s *disabledOpsSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if key == SettingKeyOpsMonitoringEnabled {
			out[key] = "false"
		}
	}
	return out, nil
}

func (s *stubOpsRepoForUserErr) ListErrorLogs(ctx context.Context, f *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	s.gotFilter = f
	return &OpsErrorLogList{
		Errors: []*OpsErrorLog{{
			Phase: "request", Type: "rate_limit_error",
			Model: "m", RequestedModel: "rm", StatusCode: 429,
			Message: "secret", UserEmail: "a@b.c",
		}},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}

func (s *stubOpsRepoForUserErr) GetErrorLogByID(ctx context.Context, id int64) (*OpsErrorLogDetail, error) {
	if s.detailErrToReturn != nil {
		return nil, s.detailErrToReturn
	}
	return s.detailToReturn, nil
}

func TestListUserErrorRequests_ForcesScopeAndRedacts(t *testing.T) {
	stub := &stubOpsRepoForUserErr{}
	svc := &OpsService{opsRepo: stub}
	uid := int64(42)
	kid := int64(7)
	in := &OpsErrorLogFilter{UserID: nil, View: "errors", Phase: "upstream", APIKeyID: &kid}
	out, err := svc.ListUserErrorRequests(context.Background(), uid, in)
	if err != nil {
		t.Fatal(err)
	}
	// 强制按用户
	if stub.gotFilter.UserID == nil || *stub.gotFilter.UserID != uid {
		t.Fatalf("UserID not forced: %+v", stub.gotFilter.UserID)
	}
	// 强制 View=all（含业务限流/余额）
	if stub.gotFilter.View != "all" {
		t.Fatalf("View not forced to all: %q", stub.gotFilter.View)
	}
	// 强制排除 count_tokens
	if !stub.gotFilter.ExcludeCountTokens {
		t.Fatal("ExcludeCountTokens not forced")
	}
	// 强制清空 Phase（防止 "upstream" 绕过 status>=400 子句 + 与 ErrorPhasesAny 双重约束）
	if stub.gotFilter.Phase != "" {
		t.Fatalf("Phase not cleared: %q", stub.gotFilter.Phase)
	}
	// APIKeyID 透传保留（用户可按自己 key 过滤；越权由 user_id AND api_key_id 双重防护）
	if stub.gotFilter.APIKeyID == nil || *stub.gotFilter.APIKeyID != kid {
		t.Fatalf("APIKeyID should be preserved, got %v", stub.gotFilter.APIKeyID)
	}
	// 调用方传入的 filter 不应被原地篡改（验证 shallow copy 隔离生效）
	if in.View != "errors" || in.UserID != nil || in.Phase != "upstream" {
		t.Fatalf("caller filter was mutated: View=%q UserID=%v Phase=%q", in.View, in.UserID, in.Phase)
	}
	// 脱敏：返回条目含 message 字段
	if len(out.Items) != 1 || out.Items[0].Category != "rate_limit" || out.Items[0].Model != "rm" {
		t.Fatalf("bad item: %+v", out.Items)
	}
}

func TestListUserErrorRequests_RequiresOpsMonitoringEnabled(t *testing.T) {
	stub := &stubOpsRepoForUserErr{}
	svc := &OpsService{opsRepo: stub, settingRepo: &disabledOpsSettingRepo{}}
	svc.SetMonitoringEnabled(false)

	got, err := svc.ListUserErrorRequests(context.Background(), 42, &OpsErrorLogFilter{})
	if !errors.Is(err, ErrOpsDisabled) {
		t.Fatalf("expected ErrOpsDisabled, got detail=%+v err=%v", got, err)
	}
	if stub.gotFilter != nil {
		t.Fatalf("repo should not be queried when ops monitoring is disabled: %+v", stub.gotFilter)
	}
}

func TestGetUserErrorRequestDetail_OwnershipEnforced(t *testing.T) {
	ownerUID := int64(999)
	callerUID := int64(1)
	upstreamStatus := 503

	detail := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:              42,
			Phase:           "upstream",
			Type:            "api_error",
			Model:           "gpt-4",
			RequestedModel:  "gpt-4-turbo",
			InboundEndpoint: "/v1/chat/completions",
			StatusCode:      502,
			Platform:        "openai",
			Message:         "upstream failed",
			UserID:          &ownerUID,
		},
		ErrorBody:          `{"error":"upstream"}`,
		UpstreamStatusCode: &upstreamStatus,
	}

	stub := &stubOpsRepoForUserErr{detailToReturn: detail}
	svc := &OpsService{opsRepo: stub}

	// 越权调用（callerUID=1,但记录属于 ownerUID=999）→ 应返回 NotFound,detail 为 nil
	got, err := svc.GetUserErrorRequestDetail(context.Background(), callerUID, 42)
	if err == nil {
		t.Fatal("expected error for unauthorized access, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil detail for unauthorized access, got %+v", got)
	}
	// 验证错误为 NotFound(不暴露存在性)
	if !infraerrors.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got: %v", err)
	}

	// 合法调用（callerUID=999 = ownerUID）→ 应返回 non-nil detail
	got2, err2 := svc.GetUserErrorRequestDetail(context.Background(), ownerUID, 42)
	if err2 != nil {
		t.Fatalf("expected no error for legitimate access, got %v", err2)
	}
	if got2 == nil {
		t.Fatal("expected non-nil detail for legitimate access")
	}
	if got2 != nil {
		if got2.ID != 42 {
			t.Errorf("want ID=42, got %d", got2.ID)
		}
		if got2.ErrorBody != `{"error":"upstream"}` {
			t.Errorf("want ErrorBody=%q, got %q", `{"error":"upstream"}`, got2.ErrorBody)
		}
		if got2.UpstreamStatusCode == nil || *got2.UpstreamStatusCode != 503 {
			t.Errorf("want UpstreamStatusCode=503, got %v", got2.UpstreamStatusCode)
		}
		if got2.Message != "upstream failed" {
			t.Errorf("want Message=%q, got %q", "upstream failed", got2.Message)
		}
	}
}

func TestGetUserErrorRequestDetail_RequiresOpsMonitoringEnabled(t *testing.T) {
	ownerUID := int64(999)
	stub := &stubOpsRepoForUserErr{detailToReturn: &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:     42,
			UserID: &ownerUID,
		},
	}}
	svc := &OpsService{opsRepo: stub, settingRepo: &disabledOpsSettingRepo{}}
	svc.SetMonitoringEnabled(false)

	got, err := svc.GetUserErrorRequestDetail(context.Background(), ownerUID, 42)
	if !errors.Is(err, ErrOpsDisabled) {
		t.Fatalf("expected ErrOpsDisabled, got detail=%+v err=%v", got, err)
	}
}

func TestGetUserErrorRequestDetail_NotFound(t *testing.T) {
	stub := &stubOpsRepoForUserErr{detailErrToReturn: sql.ErrNoRows}
	svc := &OpsService{opsRepo: stub}

	got, err := svc.GetUserErrorRequestDetail(context.Background(), 1, 999)
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil detail, got %+v", got)
	}
}

func TestUserErrorRequestRedactsNetworkAndSecrets(t *testing.T) {
	apiKeySecret := "sk-user-visible-secret"
	authSecret := "sk-auth-secret"
	detailSecret := "sk-detail-secret"
	detailAuthSecret := "detail-token-secret"
	bareSecret := "sk-proj-user-visible-secret-1234567890"
	bareBearer := "bare-token-secret-1234567890"
	jwtSecret := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLWlkIn0.abcdefghijklmnopqrstuvwxyz"

	item := ToUserErrorRequest(&OpsErrorLog{
		ID:              10,
		Phase:           "network",
		Type:            "upstream_error",
		Model:           "gpt-5.4",
		InboundEndpoint: "/v1/responses",
		StatusCode:      502,
		Platform:        "openai",
		Message:         `Post "https://japan.zelly.cn/v1/responses?api_key=` + apiKeySecret + `": dial tcp 43.165.178.21:443: connect: account_name=OpenAI_Free authorization: Bearer ` + authSecret,
	})
	if item == nil {
		t.Fatal("expected user error item")
	}
	for _, leaked := range []string{"japan.zelly.cn", "43.165.178.21", apiKeySecret, authSecret, "OpenAI_Free"} {
		if strings.Contains(item.Message, leaked) {
			t.Fatalf("message leaked %q: %s", leaked, item.Message)
		}
	}
	for _, expected := range []string{"https://*.*.*.*/v1/responses?api_key=sk-user-...cret", "account_name=***", "authorization: Bearer sk-auth-...cret"} {
		if !strings.Contains(item.Message, expected) {
			t.Fatalf("message missing %q: %s", expected, item.Message)
		}
	}
	queryOnly := ToUserErrorRequest(&OpsErrorLog{
		Message: `Get "https://query.secret.example?api_key=` + apiKeySecret + `&target=/v1/responses": failed`,
	})
	if queryOnly == nil {
		t.Fatal("expected query-only URL item")
	}
	for _, leaked := range []string{"query.secret.example", apiKeySecret} {
		if strings.Contains(queryOnly.Message, leaked) {
			t.Fatalf("query-only URL message leaked %q: %s", leaked, queryOnly.Message)
		}
	}
	if !strings.Contains(queryOnly.Message, "https://*.*.*.*?api_key=sk-user-...cret&target=/v1/responses") {
		t.Fatalf("query-only URL should keep path/query but mask host and key: %s", queryOnly.Message)
	}
	pathOnly := ToUserErrorRequest(&OpsErrorLog{
		Message: `POST /v1/responses?target=/backend-api/codex/responses&api_key=` + apiKeySecret + ` failed: invalid API key ` + bareSecret + ` accountName=prod-openai accountID=2553 Bearer ` + bareBearer + ` jwt=` + jwtSecret,
	})
	if pathOnly == nil {
		t.Fatal("expected path-only item")
	}
	for _, leaked := range []string{apiKeySecret, bareSecret, "prod-openai", "2553", bareBearer, jwtSecret} {
		if strings.Contains(pathOnly.Message, leaked) {
			t.Fatalf("path-only message leaked %q: %s", leaked, pathOnly.Message)
		}
	}
	for _, expected := range []string{"/v1/responses?target=/backend-api/codex/responses&api_key=sk-user-...cret", "sk-proj-...7890", "accountName=***", "accountID=***", "Bearer bare-t...7890"} {
		if !strings.Contains(pathOnly.Message, expected) {
			t.Fatalf("path-only message missing %q: %s", expected, pathOnly.Message)
		}
	}

	detail := ToUserErrorRequestDetail(&OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:         11,
			Phase:      "upstream",
			Type:       "upstream_error",
			Model:      "gpt-5.4",
			StatusCode: 502,
			Message:    "failed",
		},
		ErrorBody: `{"error":{"message":"request to https://api.secret.example/v1 failed","api_key":"` + detailSecret + `","authorization":"Bearer ` + detailAuthSecret + `"}}`,
	})
	if detail == nil {
		t.Fatal("expected user error detail")
	}
	for _, leaked := range []string{"api.secret.example", detailSecret, detailAuthSecret} {
		if strings.Contains(detail.ErrorBody, leaked) {
			t.Fatalf("error_body leaked %q: %s", leaked, detail.ErrorBody)
		}
	}
	if !strings.Contains(detail.ErrorBody, "https://*.*.*.*/v1") {
		t.Fatalf("error_body should mask host but keep path: %s", detail.ErrorBody)
	}
}

func TestUserErrorRequestDoesNotMutateAdminSource(t *testing.T) {
	secret := "sk-admin-source-secret"
	source := &OpsErrorLog{
		Message: "api_key=" + secret + " https://admin-source.example/v1",
	}

	item := ToUserErrorRequest(source)
	if item == nil {
		t.Fatal("expected user error item")
	}
	if !strings.Contains(source.Message, secret) || !strings.Contains(source.Message, "admin-source.example") {
		t.Fatalf("source should remain unmodified for admin path: %s", source.Message)
	}
	if item != nil && (strings.Contains(item.Message, secret) || strings.Contains(item.Message, "admin-source.example")) {
		t.Fatalf("user message should be redacted: %s", item.Message)
	}
}

func TestGetUserErrorRequestDetail_InvalidID(t *testing.T) {
	stub := &stubOpsRepoForUserErr{}
	svc := &OpsService{opsRepo: stub}

	_, err := svc.GetUserErrorRequestDetail(context.Background(), 1, 0)
	if err == nil {
		t.Fatal("expected error for id=0")
	}
	_, err = svc.GetUserErrorRequestDetail(context.Background(), 1, -5)
	if err == nil {
		t.Fatal("expected error for id=-5")
	}
}
func TestListUserErrorRequests_EnablesMatchDeletedKeyOwner(t *testing.T) {
	stub := &stubOpsRepoForUserErr{}
	svc := &OpsService{opsRepo: stub}
	uid := int64(42)

	if _, err := svc.ListUserErrorRequests(context.Background(), uid, &OpsErrorLogFilter{}); err != nil {
		t.Fatal(err)
	}
	if stub.gotFilter == nil || !stub.gotFilter.MatchDeletedKeyOwner {
		t.Fatal("ListUserErrorRequests should enable MatchDeletedKeyOwner for the user scope")
	}
}

func TestGetUserErrorRequestDetail_DeletedKeyOwnerAccess(t *testing.T) {
	ownerUID := int64(777)
	otherUID := int64(2)

	// 情况2:user_id=NULL,靠 deleted_key_owner_user_id 归因到 ownerUID
	mk := func() *OpsErrorLogDetail {
		return &OpsErrorLogDetail{
			OpsErrorLog: OpsErrorLog{
				ID:                    55,
				Phase:                 "auth",
				Type:                  "api_error",
				StatusCode:            401,
				Message:               "Invalid API key",
				UserID:                nil,
				APIKeyName:            "my-old-key",
				APIKeyDeleted:         true,
				DeletedKeyOwnerUserID: &ownerUID,
			},
		}
	}

	// 原所有者(经 deleted_key 归因)→ 放行
	svcOwner := &OpsService{opsRepo: &stubOpsRepoForUserErr{detailToReturn: mk()}}
	got, err := svcOwner.GetUserErrorRequestDetail(context.Background(), ownerUID, 55)
	if err != nil {
		t.Fatalf("owner via deleted_key should be allowed, got err: %v", err)
	}
	if got == nil || got.ID != 55 {
		t.Fatalf("expected detail ID=55, got %+v", got)
	}
	if !got.KeyDeleted || got.KeyName != "my-old-key" {
		t.Fatalf("expected KeyDeleted=true KeyName=my-old-key, got %+v", got)
	}

	// 他人 → NotFound,不泄露存在性
	svcOther := &OpsService{opsRepo: &stubOpsRepoForUserErr{detailToReturn: mk()}}
	got2, err2 := svcOther.GetUserErrorRequestDetail(context.Background(), otherUID, 55)
	if err2 == nil || got2 != nil {
		t.Fatalf("non-owner should get (nil, NotFound), got detail=%+v err=%v", got2, err2)
	}
	if !infraerrors.IsNotFound(err2) {
		t.Fatalf("expected NotFound, got %v", err2)
	}
}

func TestGetUserErrorRequestDetail_RejectsListExcludedRows(t *testing.T) {
	ownerUID := int64(777)

	cases := []struct {
		name   string
		detail *OpsErrorLogDetail
	}{
		{
			name: "recovered upstream",
			detail: &OpsErrorLogDetail{
				OpsErrorLog: OpsErrorLog{
					ID:         71,
					Phase:      "upstream",
					Type:       "upstream_error",
					StatusCode: 503,
					Message:    "upstream recovered",
					UserID:     &ownerUID,
				},
				ClientStatusCode: intPtr(200),
			},
		},
		{
			name: "count tokens probe",
			detail: &OpsErrorLogDetail{
				OpsErrorLog: OpsErrorLog{
					ID:         72,
					Phase:      "upstream",
					Type:       "upstream_error",
					StatusCode: 400,
					Message:    "count tokens failed",
					UserID:     &ownerUID,
				},
				ClientStatusCode: intPtr(400),
				IsCountTokens:    true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpsService{opsRepo: &stubOpsRepoForUserErr{detailToReturn: tc.detail}}
			got, err := svc.GetUserErrorRequestDetail(context.Background(), ownerUID, tc.detail.ID)
			if err == nil || got != nil {
				t.Fatalf("expected list-excluded detail to be hidden, got detail=%+v err=%v", got, err)
			}
			if !infraerrors.IsNotFound(err) {
				t.Fatalf("expected NotFound, got %v", err)
			}
		})
	}
}
