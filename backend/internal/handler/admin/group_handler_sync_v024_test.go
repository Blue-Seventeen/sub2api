package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Verify the HTTP-to-service boundary, including explicit zero/false and clears.
func TestGroupRequestsSyncV024Mapping(t *testing.T) {
	cases := []struct {
		key, field string
		value      json.RawMessage
		want       any
	}{
		{"long_context_pricing_enabled", "LongContextPricingEnabled", json.RawMessage(`true`), true},
		{"model_pricing", "ModelPricing", json.RawMessage(`[{"models":["gpt-test"],"billing_mode":"token","input_price":0.5}]`), []service.ChannelModelPricing{{Models: []string{"gpt-test"}, BillingMode: "token", InputPrice: float64PtrForSimpleModeTest(0.5)}}},
		{"model_allowlist", "ModelAllowlist", json.RawMessage(`{"enabled":true,"models":["gpt-test"]}`), service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-test"}}},
		{"codex_models_manifest_config", "CodexModelsManifestConfig", json.RawMessage(`{"enabled":true,"account_ids":[12,34],"fallback_to_scheduler":true}`), service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{12, 34}, FallbackToScheduler: true}},
		{"force_openai_fast", "ForceOpenAIFast", json.RawMessage(`true`), true},
		{"free_openai_fast", "FreeOpenAIFast", json.RawMessage(`true`), true},
		{"video_model_prices", "VideoModelPrices", json.RawMessage(`{"grok-imagine-video":{"720p":0.12}}`), map[string]map[string]float64{"grok-imagine-video": {"720p": 0.12}}},
		{"web_search_price_per_call", "WebSearchPricePerCall", json.RawMessage(`0.02`), 0.02},
		{"search_price_per_1k", "SearchPricePer1k", json.RawMessage(`5`), 5.0},
		{"audio_realtime_price_per_min", "AudioRealtimePricePerMin", json.RawMessage(`0.05`), 0.05},
		{"audio_tts_price_per_million_chars", "AudioTTSPricePerMillionChars", json.RawMessage(`4.2`), 4.2},
		{"audio_stt_price_per_hour", "AudioSTTPricePerHour", json.RawMessage(`0.3`), 0.3},
		{"max_reasoning_effort", "MaxReasoningEffort", json.RawMessage(`"high"`), "high"},
		{"max_reasoning_effort_over_limit", "MaxReasoningEffortOverLimit", json.RawMessage(`"deny"`), "deny"},
		{"reasoning_effort_mappings", "ReasoningEffortMappings", json.RawMessage(`[{"from":"xhigh","to":"high","match_type":"prefix","model":"gpt"}]`), []service.ReasoningEffortMapping{{From: "xhigh", To: "high", MatchType: "prefix", Model: "gpt"}}},
		{"models_list_config", "ModelsListConfig", json.RawMessage(`{"enabled":true,"models":["custom-public"]}`), service.GroupModelsListConfig{Enabled: true, Models: []string{"custom-public"}}},
		{"newapi_style_interface_enabled", "NewAPIStyleInterfaceEnabled", json.RawMessage(`true`), true},
		{"rate_multiplier", "RateMultiplier", json.RawMessage(`0`), 0.0},
		{"image_rate_multiplier", "ImageRateMultiplier", json.RawMessage(`1.5`), 1.5},
		{"video_rate_multiplier", "VideoRateMultiplier", json.RawMessage(`2.5`), 2.5},
		{"peak_rate_windows", "PeakRateWindows", json.RawMessage(`[{"start":"08:00","end":"10:00","multiplier":1.5},{"start":"18:00","end":"20:00","multiplier":2}]`), []service.PeakRateWindow{{Start: "08:00", End: "10:00", Multiplier: 1.5}, {Start: "18:00", End: "20:00", Multiplier: 2}}},
		{"custom_limit_hours", "CustomLimitHours", json.RawMessage(`12`), 12},
		{"custom_limit_usd", "CustomLimitUSD", json.RawMessage(`8.5`), 8.5},
		{"long_context_pricing_enabled", "LongContextPricingEnabled", json.RawMessage(`false`), false},
		{"force_openai_fast", "ForceOpenAIFast", json.RawMessage(`false`), false},
		{"free_openai_fast", "FreeOpenAIFast", json.RawMessage(`false`), false},
		{"model_pricing", "ModelPricing", json.RawMessage(`[]`), []service.ChannelModelPricing{}},
		{"video_model_prices", "VideoModelPrices", json.RawMessage(`{}`), map[string]map[string]float64{}},
		{"model_allowlist", "ModelAllowlist", json.RawMessage(`{"enabled":false,"models":[]}`), service.GroupModelAllowlist{Models: []string{}}},
		{"codex_models_manifest_config", "CodexModelsManifestConfig", json.RawMessage(`{"enabled":false,"account_ids":[]}`), service.GroupCodexModelsManifestConfig{AccountIDs: []int64{}}},
		{"max_reasoning_effort_over_limit", "MaxReasoningEffortOverLimit", json.RawMessage(`""`), ""},
		{"reasoning_effort_mappings", "ReasoningEffortMappings", json.RawMessage(`[]`), []service.ReasoningEffortMapping{}},
		{"search_price_per_1k", "SearchPricePer1k", json.RawMessage(`0`), 0.0},
		{"audio_realtime_price_per_min", "AudioRealtimePricePerMin", json.RawMessage(`-1`), -1.0},
	}
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, tc := range cases {
			t.Run(method+"/"+tc.key+"/"+string(tc.value), func(t *testing.T) {
				svc := newStubAdminService()
				payload := map[string]json.RawMessage{"name": json.RawMessage(`"sync"`), "platform": json.RawMessage(`"openai"`), tc.key: tc.value}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				syncGroupRequest(t, svc, method, body)
				var input any
				if method == http.MethodPost {
					require.Len(t, svc.createdGroups, 1)
					input = svc.createdGroups[0]
				} else {
					require.Len(t, svc.updatedGroups, 1)
					input = svc.updatedGroups[0]
				}
				field := reflect.ValueOf(input).Elem().FieldByName(tc.field)
				require.True(t, field.IsValid(), tc.field)
				if field.Kind() == reflect.Pointer {
					require.False(t, field.IsNil(), tc.field+" must not be omitted")
					field = field.Elem()
				}
				require.Equal(t, tc.want, field.Interface())
			})
		}
	}
}

func syncGroupRequest(t *testing.T, svc *stubAdminService, method string, body []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewGroupHandler(svc, nil, nil)
	r := gin.New()
	r.POST("/groups/:id", h.Create)
	r.PUT("/groups/:id", h.Update)
	req := httptest.NewRequest(method, "/groups/7", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
}

func TestGroupRequestsSyncV024OmittedFieldsStayUnchanged(t *testing.T) {
	svc := newStubAdminService()
	syncGroupRequest(t, svc, http.MethodPut, []byte(`{"name":"renamed"}`))
	require.Equal(t, &service.UpdateGroupInput{Name: "renamed"}, svc.updatedGroups[0])
	syncGroupRequest(t, svc, http.MethodPost, []byte(`{"name":"default"}`))
	require.Equal(t, 1.0, svc.createdGroups[0].RateMultiplier)
	require.False(t, svc.createdGroups[0].PeakRateEnabledSet)
}

func TestGroupCreateRequestModelsListConfigPresenceIsPreserved(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		set  bool
	}{
		{name: "omitted", body: `{"name":"default"}`, set: false},
		{name: "explicitly disabled", body: `{"name":"disabled","models_list_config":{"enabled":false,"models":[]}}`, set: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newStubAdminService()
			syncGroupRequest(t, svc, http.MethodPost, []byte(tc.body))
			require.Equal(t, tc.set, svc.createdGroups[0].ModelsListConfigSet)
			if tc.set {
				require.False(t, svc.createdGroups[0].ModelsListConfig.Enabled)
				require.Empty(t, svc.createdGroups[0].ModelsListConfig.Models)
			}
		})
	}
}

func TestGroupRequestsSyncV024Platforms(t *testing.T) {
	for _, platform := range []string{"kimi", "minimax", "moonshot", "volcengine", "ali", "perplexity", "mistral", "siliconflow", "openrouter", "suno", "kling", "midjourney"} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(method+"/"+platform, func(t *testing.T) {
				svc := newStubAdminService()
				body, err := json.Marshal(map[string]string{"name": "platform", "platform": platform})
				require.NoError(t, err)
				syncGroupRequest(t, svc, method, body)
			})
		}
	}
}

func TestGroupRequestsSyncV024SimpleModeDropsAdvancedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			svc := newStubAdminService()
			h := NewGroupHandlerWithConfig(svc, nil, nil, &config.Config{RunMode: config.RunModeSimple})
			r := gin.New()
			r.POST("/groups/:id", h.Create)
			r.PUT("/groups/:id", h.Update)
			body := `{"name":"basic","force_openai_fast":true,"free_openai_fast":true,"model_allowlist":{"enabled":true,"models":["private"]},"codex_models_manifest_config":{"enabled":true,"account_ids":[7]},"models_list_config":{"enabled":true,"models":["custom"]},"newapi_style_interface_enabled":true,"custom_limit_hours":12,"custom_limit_usd":8.5,"peak_rate_windows":[{"start":"08:00","end":"10:00","multiplier":2}],"video_model_prices":{"grok-imagine-video":{"720p":0.12}},"max_reasoning_effort_over_limit":"deny"}`
			req := httptest.NewRequest(method, "/groups/7", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			if method == http.MethodPost {
				require.Len(t, svc.createdGroups, 1)
				require.Equal(t, &service.CreateGroupInput{Name: "basic", RateMultiplier: 1, SubscriptionType: service.SubscriptionTypeStandard}, svc.createdGroups[0])
			} else {
				require.Len(t, svc.updatedGroups, 1)
				require.Equal(t, &service.UpdateGroupInput{Name: "basic"}, svc.updatedGroups[0])
			}
		})
	}
}
