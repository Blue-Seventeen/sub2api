package service

import (
	"testing"

	"github.com/tidwall/gjson"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIImagesResponseBody(t *testing.T) {
	t.Run("keeps_standard_url_shape", func(t *testing.T) {
		body := []byte(`{"created":123,"data":[{"url":"https://example.com/a.png","revised_prompt":"draw cat"}]}`)
		normalized, imageCount, ok := normalizeOpenAIImagesResponseBody(body)
		require.True(t, ok)
		require.Equal(t, 1, imageCount)
		require.Equal(t, "https://example.com/a.png", gjson.GetBytes(normalized, "data.0.url").String())
		require.Equal(t, "draw cat", gjson.GetBytes(normalized, "data.0.revised_prompt").String())
		require.Equal(t, int64(123), gjson.GetBytes(normalized, "created").Int())
	})

	t.Run("bridges_images_download_url_shape", func(t *testing.T) {
		body := []byte(`{"images":[{"download_url":"https://example.com/bridge.png","prompt":"bridge prompt"}]}`)
		normalized, imageCount, ok := normalizeOpenAIImagesResponseBody(body)
		require.True(t, ok)
		require.Equal(t, 1, imageCount)
		require.Equal(t, "https://example.com/bridge.png", gjson.GetBytes(normalized, "data.0.url").String())
		require.Equal(t, "bridge prompt", gjson.GetBytes(normalized, "data.0.revised_prompt").String())
		require.True(t, gjson.GetBytes(normalized, "created").Exists())
	})

	t.Run("bridges_nested_data_b64_json_object", func(t *testing.T) {
		body := []byte(`{"data":[{"b64_json":{"bytesBase64":"Zm9v"}}]}`)
		normalized, imageCount, ok := normalizeOpenAIImagesResponseBody(body)
		require.True(t, ok)
		require.Equal(t, 1, imageCount)
		require.Equal(t, "Zm9v", gjson.GetBytes(normalized, "data.0.b64_json").String())
	})

	t.Run("returns_false_for_non_image_payload", func(t *testing.T) {
		body := []byte(`{"message":"no images here"}`)
		normalized, imageCount, ok := normalizeOpenAIImagesResponseBody(body)
		require.False(t, ok)
		require.Equal(t, 0, imageCount)
		require.JSONEq(t, string(body), string(normalized))
	})
}
