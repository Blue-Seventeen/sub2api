package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNewAPIStyleAliImagesGenerationsConvertsOpenAIRequestToDashScope(t *testing.T) {
	body := []byte(`{"model":"qwen-image","prompt":"draw a cat","size":"1024x1024","n":2,"negative_prompt":"blur","watermark":false}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"request_id":"req-image",
		"output":{"choices":[{"message":{"content":[{"image":"https://example.test/a.png"},{"image":"https://example.test/b.png"}]}}]}
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	result, endpoint, err := svc.Forward(context.Background(), c, newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteImages,
		RequestBody: body,
		InboundPath: "/v1/images/generations",
		ContentType: "application/json",
		HeaderSource: http.Header{
			"Accept": []string{"application/json"},
		},
	})

	require.NoError(t, err)
	require.Equal(t, aliQwenMultimodalGenerationPath, endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, aliQwenMultimodalGenerationPath, upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer account-token", upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "qwen-image", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(upstream.lastBody, "input.messages.0.content.0.text").String())
	require.Equal(t, "1024*1024", gjson.GetBytes(upstream.lastBody, "parameters.size").String())
	require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "parameters.n").Int())
	require.Equal(t, "blur", gjson.GetBytes(upstream.lastBody, "parameters.negative_prompt").String())

	require.NotNil(t, result)
	require.Equal(t, 2, result.ImageCount)
	require.Equal(t, "1K", result.ImageSize)
	require.Equal(t, BillableUnitTypeImage, result.BillableUnitType)
	require.Equal(t, "qwen-image", result.Model)

	responseBody := rec.Body.String()
	require.Equal(t, "https://example.test/a.png", gjson.Get(responseBody, "data.0.url").String())
	require.Equal(t, "https://example.test/b.png", gjson.Get(responseBody, "data.1.url").String())
	require.Equal(t, "req-image", gjson.Get(responseBody, "id").String())
}

func TestNewAPIStyleAliImagesRequirePromptBeforeUpstream(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"ok":true}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteImages,
		RequestBody: []byte(`{"model":"qwen-image"}`),
		InboundPath: "/v1/images/generations",
		ContentType: "application/json",
	})

	var clientErr *CompatibleClientError
	require.ErrorAs(t, err, &clientErr)
	require.Equal(t, http.StatusBadRequest, clientErr.StatusCode)
	require.Contains(t, clientErr.Message, "prompt is required")
	require.Nil(t, upstream.lastReq)
}

func TestNewAPIStyleAliImagesBillingUsesOriginalUsageBeforeResponseNormalization(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"usage":{"image_count":1},
		"output":{"choices":[{"message":{"content":[{"image":"https://example.test/a.png"},{"image":"https://example.test/b.png"}]}}]}
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteImages,
		RequestBody: []byte(`{"model":"qwen-image","prompt":"draw","n":4}`),
		InboundPath: "/v1/images/generations",
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, BillableUnitTypeImage, result.BillableUnitType)
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 2)
}

func TestNewAPIStyleAliQwenImageOfficialRouteForwardsDashScopeBodyAndBillsImage(t *testing.T) {
	body := []byte(`{"model":"qwen-image","input":{"messages":[{"role":"user","content":[{"text":"draw a cat"}]}]},"parameters":{"size":"2048*2048","n":1,"watermark":false}}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"output":{"choices":[{"message":{"content":[{"image":"https://example.test/qwen.png"}]}}]}
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteQwenImage,
		RequestBody: body,
		InboundPath: aliQwenMultimodalGenerationPath,
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Equal(t, aliQwenMultimodalGenerationPath, endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, aliQwenMultimodalGenerationPath, upstream.lastReq.URL.Path)
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "2K", result.ImageSize)
	require.Equal(t, BillableUnitTypeImage, result.BillableUnitType)
	require.Zero(t, result.RequestCount)
}

func TestNewAPIStyleImagesUnsupportedForNonAliCompatiblePlatforms(t *testing.T) {
	svc := &NewAPIStyleGatewayService{}
	account := &Account{
		Platform: PlatformDeepSeek,
		Extra:    map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	require.False(t, svc.SupportsForGroup(account, &Group{Platform: PlatformDeepSeek}, NewAPIStyleRouteImages))
}

func TestNewAPIStyleAliImagesInferCountFromDashScopeResponse(t *testing.T) {
	body := []byte(`{"model":"qwen-image","parameters":{"size":"4096*4096"}}`)
	responseBody := []byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://example.test/a.png"},{"image":"https://example.test/b.png"}]}}]}}`)

	require.Equal(t, 2, inferImageCount(body, responseBody))
	require.Equal(t, "4K", inferImageSize(body))
}

func TestNewAPIStyleAliImagesInferCountPrefersGeneratedCountOverRequestN(t *testing.T) {
	body := []byte(`{"model":"qwen-image","n":4,"parameters":{"n":4}}`)

	require.Equal(t, 1, inferImageCount(body, []byte(`{"usage":{"image_count":1}}`)))
	require.Equal(t, 2, inferImageCount(body, []byte(`{"data":[{"url":"https://example.test/a.png"},{"url":"https://example.test/b.png"}]}`)))
}

func TestNewAPIStyleAliQwenTTSOfficialRouteStillUsesRequestBilling(t *testing.T) {
	body := []byte(`{"model":"qwen3-tts-flash","input":{"text":"hello","voice":"Cherry","language_type":"English"}}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(`event:result
data:{"output":{"audio":{"data":"UklGRg=="}}}

`)),
	}}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteQwenTTS,
		RequestBody: body,
		InboundPath: aliQwenTTSGenerationPath,
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, 1, result.RequestCount)
	require.Equal(t, BillableUnitTypeRequest, result.BillableUnitType)
	require.Zero(t, result.ImageCount)
}
