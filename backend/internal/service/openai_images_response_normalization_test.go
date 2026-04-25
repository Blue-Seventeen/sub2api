package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIImagesResponseBody_FromLegacyImagesArray(t *testing.T) {
	body := []byte(`{
		"created": 1710000100,
		"revised_prompt": "draw a cat",
		"images": [
			{"download_url":"https://files.example.com/cat.png?sig=1"},
			{"b64_json":{"bytesBase64":"QUJD"}}
		]
	}`)

	normalized, count, ok := normalizeOpenAIImagesResponseBody(body)
	require.True(t, ok)
	require.Equal(t, 2, count)
	require.Equal(t, int64(1710000100), gjson.GetBytes(normalized, "created").Int())
	require.Equal(t, "https://files.example.com/cat.png?sig=1", gjson.GetBytes(normalized, "data.0.url").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(normalized, "data.0.revised_prompt").String())
	require.Equal(t, "QUJD", gjson.GetBytes(normalized, "data.1.b64_json").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(normalized, "data.1.revised_prompt").String())
}

func TestBuildNormalizedOpenAIImageResponseItems_MergesPointerInfoAndDedupes(t *testing.T) {
	body := []byte(`{
		"message": {
			"metadata": {
				"dalle": {
					"prompt": "cat astronaut"
				}
			}
		},
		"output": [
			{
				"type": "image_generation_call",
				"asset_pointer": "file-service://file_123",
				"download_url": "https://files.example.com/cat.png"
			}
		],
		"trace": "file-service://file_123",
		"data": [
			{
				"url": "https://files.example.com/cat.png"
			}
		]
	}`)

	items := buildNormalizedOpenAIImageResponseItems(body)
	require.Len(t, items, 1)
	require.Equal(t, "https://files.example.com/cat.png", items[0].URL)
	require.Equal(t, "cat astronaut", items[0].RevisedPrompt)
	require.Empty(t, items[0].B64JSON)
}

func TestHandleOpenAIImagesNonStreamingResponse_NormalizesLegacyPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: ioNopCloserString(`{
			"images": [
				{
					"download_url": "https://files.example.com/cat.png",
					"revised_prompt": "draw a cat"
				}
			],
			"usage": {
				"input_tokens": 3,
				"output_tokens": 5,
				"output_tokens_details": {
					"image_tokens": 2
				}
			}
		}`),
	}

	svc := &OpenAIGatewayService{}
	before := time.Now().Unix()
	usage, imageCount, err := svc.handleOpenAIImagesNonStreamingResponse(resp, c)
	after := time.Now().Unix()

	require.NoError(t, err)
	require.Equal(t, 1, imageCount)
	require.Equal(t, 3, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Equal(t, 2, usage.ImageOutputTokens)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "https://files.example.com/cat.png", gjson.Get(rec.Body.String(), "data.0.url").String())
	require.Equal(t, "draw a cat", gjson.Get(rec.Body.String(), "data.0.revised_prompt").String())
	created := gjson.Get(rec.Body.String(), "created").Int()
	require.GreaterOrEqual(t, created, before)
	require.LessOrEqual(t, created, after)
}

func ioNopCloserString(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}
