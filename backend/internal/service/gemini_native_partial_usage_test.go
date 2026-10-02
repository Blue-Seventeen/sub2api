//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestGeminiForwardNative_PartialStreamPreservesUsage(t *testing.T) {
	const partial = `data: {"candidates":[{"content":{"parts":[{"text":"partial"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}` + "\n\n"
	for _, readErr := range []error{nil, errors.New("upstream read failed")} {
		name := "eof"
		if readErr != nil {
			name = "read_error"
		}
		t.Run(name, func(t *testing.T) {
			svc := newGeminiSignalService("text/event-stream", partial)
			if readErr != nil {
				svc.httpUpstream.(*geminiCompatHTTPUpstreamStub).response.Body = io.NopCloser(io.MultiReader(strings.NewReader(partial), iotest.ErrReader(readErr)))
			}
			c, rec := newGeminiNativeTestContext(t)
			result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(), "gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, partial, rec.Body.String(), "native Gemini bytes stay untouched")
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
		})
	}
}

func TestGeminiForwardNative_TerminalThenReadErrorKeepsSuccess(t *testing.T) {
	for _, terminal := range []string{
		`{"candidates":[{"finishReason":"STOP"}]}`,
		`[DONE]`,
		`{"promptFeedback":{"blockReason":"SAFETY"}}`,
		`{"error":{"code":429,"message":"quota exhausted","status":"RESOURCE_EXHAUSTED"}}`,
	} {
		t.Run(terminal, func(t *testing.T) {
			body := `data: {"candidates":[{"content":{"parts":[{"text":"complete"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}` + "\n\ndata: " + terminal + "\n\n"
			svc := newGeminiSignalService("text/event-stream", body)
			svc.httpUpstream.(*geminiCompatHTTPUpstreamStub).response.Body = io.NopCloser(io.MultiReader(strings.NewReader(body), iotest.ErrReader(errors.New("connection reset after terminal"))))
			c, rec := newGeminiNativeTestContext(t)
			result, err := svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(), "gemini-3.7-flash", "streamGenerateContent", true, geminiSignalTestRequest())
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, body, rec.Body.String())
		})
	}
}
