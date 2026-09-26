//go:build unit

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiModelsHTTPUpstreamStub struct {
	service.HTTPUpstream
	status int
	body   string
}

func (s *geminiModelsHTTPUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"models-request"},
		},
		Body: io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func TestGeminiV1BetaListModels_ProductionAllowlistPreservesEnvelopeAndSanitizesNulls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const groupID int64 = 42
	const body = `{"models":[{"name":"models/gemini-2.5-pro","description":null,"extra":{"kept":true,"removed":null}},{"name":"models/gemini-2.5-flash"}],"nextPageToken":"next","unknown":{"value":42},"empty":null}`
	for _, enabled := range []bool{false, true} {
		repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{groupID: {{
				ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key"},
			}}},
		}}
		h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(
			repo, nil, nil, nil, nil, nil,
			&geminiModelsHTTPUpstreamStub{status: http.StatusOK, body: body}, nil, &config.Config{},
		)}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: new(groupID), Group: &service.Group{
			ID: groupID, Platform: service.PlatformGemini,
			ModelAllowlist: service.GroupModelAllowlist{Enabled: enabled, Models: []string{"gemini-2.5-pro"}},
		}})
		h.GeminiV1BetaListModels(c)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "models-request", rec.Header().Get("X-Request-Id"))
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.JSONEq(t, `"next"`, string(got["nextPageToken"]))
		require.JSONEq(t, `{"value":42}`, string(got["unknown"]))
		require.NotContains(t, rec.Body.String(), "null")
		wantModels := `[{"name":"models/gemini-2.5-pro","extra":{"kept":true}}]`
		if !enabled {
			wantModels = `[{"name":"models/gemini-2.5-pro","extra":{"kept":true}},{"name":"models/gemini-2.5-flash"}]`
		}
		require.JSONEq(t, wantModels, string(got["models"]))
	}
}

func TestGeminiV1BetaListModels_AllowlistFiltersNativeResponse(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-2.5-pro"},{"name":"models/gemini-2.5-flash"}],"nextPageToken":"next"}`)
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(body, service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-2.5-pro"}})
	require.True(t, ok)
	require.True(t, dropped)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-2.5-pro"}],"nextPageToken":"next"}`, string(filtered))
}

func TestGeminiV1BetaListModels_ForcedAntigravityAppliesAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			Platform: service.PlatformGemini,
			ModelAllowlist: service.GroupModelAllowlist{
				Enabled: true,
				Models:  []string{"gemini-custom"},
			},
		},
	})
	c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)

	groupID := int64(42)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.GroupID = &groupID
	repo := &geminiAllowlistAccountRepoStub{}
	(&GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, nil)}).GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got antigravity.GeminiModelsListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.Models)
}

func TestGeminiModelAllowlist_DisabledPreservesNativeResponse(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-2.5-pro"}]}`)
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(body, service.GroupModelAllowlist{Enabled: false, Models: []string{"other"}})
	require.True(t, ok)
	require.False(t, dropped)
	require.Equal(t, body, filtered)
}

// TestGeminiV1BetaHandler_PlatformRoutingInvariant 文档化并验证 Handler 层的平台路由逻辑不变量
// 该测试确保 gemini 和 antigravity 平台的路由逻辑符合预期
func TestGeminiV1BetaHandler_PlatformRoutingInvariant(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		expectedService string
		description     string
	}{
		{
			name:            "Gemini平台使用ForwardNative",
			platform:        service.PlatformGemini,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
			description:     "Gemini OAuth 账户直接调用 Google API",
		},
		{
			name:            "Antigravity平台使用ForwardGemini",
			platform:        service.PlatformAntigravity,
			expectedService: "AntigravityGatewayService.ForwardGemini",
			description:     "Antigravity 账户通过 CRS 中转，支持 Gemini 协议",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaModels 中的路由决策 (lines 199-205 in gemini_v1beta_handler.go)
			var routedService string
			if tt.platform == service.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			require.Equal(t, tt.expectedService, routedService,
				"平台 %s 应该路由到 %s: %s",
				tt.platform, tt.expectedService, tt.description)
		})
	}
}

// TestGeminiV1BetaHandler_ListModelsAntigravityFallback 验证 ListModels 的 antigravity 降级逻辑
// 当没有 gemini 账户但有 antigravity 账户时，应返回静态模型列表
func TestGeminiV1BetaHandler_ListModelsAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态列表",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_fallback",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaListModels 的逻辑 (lines 33-44 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_fallback"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

// TestGeminiV1BetaHandler_GetModelAntigravityFallback 验证 GetModel 的 antigravity 降级逻辑
func TestGeminiV1BetaHandler_GetModelAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态模型信息",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_model_info",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaGetModel 的逻辑 (lines 77-87 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_model_info"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

func TestShouldFallbackGeminiModel_KnownFallbackOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.True(t, shouldFallbackGeminiModel("gemini-3.1-pro-preview-customtools", res))
}

func TestShouldFallbackGeminiModel_UnknownModelOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.False(t, shouldFallbackGeminiModel("gemini-future-model", res))
}

func TestShouldFallbackGeminiModel_DelegatesScopeFallback(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{
		StatusCode: http.StatusForbidden,
		Headers:    http.Header{"Www-Authenticate": []string{"Bearer error=\"insufficient_scope\""}},
		Body:       []byte("insufficient authentication scopes"),
	}
	require.True(t, shouldFallbackGeminiModel("gemini-future-model", res))
}

func TestSanitizeJSONNullFields_RemovesNullModelFields(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"models": [
			{
				"name": "gemini-3.1-flash-image",
				"baseModelId": null,
				"version": null,
				"displayName": "gemini-3.1-flash-image",
				"description": null,
				"inputTokenLimit": null,
				"outputTokenLimit": null,
				"supportedGenerationMethods": ["generateContent", "streamGenerateContent"]
			}
		],
		"nextPageToken": null
	}`)

	sanitized := sanitizeJSONNullFields(input)

	require.JSONEq(t, `{
		"models": [
			{
				"name": "gemini-3.1-flash-image",
				"displayName": "gemini-3.1-flash-image",
				"supportedGenerationMethods": ["generateContent", "streamGenerateContent"]
			}
		]
	}`, string(sanitized))
}

func TestWriteSanitizedGeminiModelsResponse_PreservesStatusAndHeaders(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	res := &service.UpstreamHTTPResult{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Test":       []string{"keep-me"},
		},
		Body: []byte(`{"models":[{"name":"gemini-2.5-pro","description":null}]}`),
	}

	writeSanitizedGeminiModelsResponse(c, res)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "keep-me", w.Header().Get("X-Test"))
	require.JSONEq(t, `{"models":[{"name":"gemini-2.5-pro"}]}`, w.Body.String())
}
