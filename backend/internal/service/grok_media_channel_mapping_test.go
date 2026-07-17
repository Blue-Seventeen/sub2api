package service

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardGrokMediaAppliesChannelMappedModelBeforeNormalization(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"public-grok-image","prompt":"draw a cat"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := grokMediaChannelMappingAccount()

	result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json", "grok-imagine")
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"grok-imagine-image-quality","prompt":"draw a cat"}`, string(upstream.lastBody))
	require.Equal(t, "grok-imagine-image-quality", result.UpstreamModel)
}

func TestForwardGrokMediaAppliesChannelMappedModelAfterMultipartConversion(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.WriteField("model", "public-grok-edit"))
	require.NoError(t, writer.WriteField("prompt", "edit image"))
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="image"; filename="input.png"`)
	partHeader.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(partHeader)
	require.NoError(t, err)
	_, err = part.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(buf.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := grokMediaChannelMappingAccount()

	_, err = svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesEdits, "", buf.Bytes(), writer.FormDataContentType(), "grok-imagine-edit-upstream")
	require.NoError(t, err)
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.Equal(t, "grok-imagine-edit-upstream", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "image.url").String(), "data:image/png;base64,"))
}

func grokMediaChannelMappingAccount() *Account {
	return &Account{
		ID:          9061,
		Name:        "grok-channel-mapping",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "api-key",
			"base_url": "https://xai.test/v1",
		},
	}
}
