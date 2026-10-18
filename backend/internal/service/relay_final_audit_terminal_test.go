package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAntigravityTerminal_IdleAfterFinishKeepsSuccess(t *testing.T) {
	const terminal = "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"complete\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":4}}}\n\n"
	for _, mode := range []string{"gemini_stream", "gemini_buffer", "claude_stream", "claude_buffer"} {
		t.Run(mode, func(t *testing.T) {
			svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1, MaxLineSize: defaultMaxLineSize}})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			defer func() { _ = writer.Close() }()
			written := make(chan struct{})
			go func() {
				_, _ = io.WriteString(writer, terminal)
				close(written)
			}()
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
			var result *antigravityStreamResult
			var err error
			switch mode {
			case "gemini_stream":
				result, err = svc.handleGeminiStreamingResponse(c, resp, time.Now())
			case "gemini_buffer":
				result, err = svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
			case "claude_stream":
				result, err = svc.handleClaudeStreamingResponse(c, resp, time.Now(), "gemini-2.5-flash")
			case "claude_buffer":
				_, result, err = svc.collectClaudeStreamResponse(c, resp, time.Now(), "gemini-2.5-flash")
			}
			<-written
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 4, result.usage.OutputTokens)
			require.NotContains(t, rec.Body.String(), "event: error")
		})
	}
}

func TestHandleClaudeStreamingResponse_ReadErrorMarksErrorCommitted(t *testing.T) {
	svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":4}}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(io.MultiReader(strings.NewReader(body), iotest.ErrReader(errors.New("upstream reset"))))}
	_, err := svc.handleClaudeStreamingResponse(c, resp, time.Now(), "gemini-2.5-flash")
	require.Error(t, err)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error"))
	require.True(t, IsResponseCommitted(c), "handler must not append a second error frame")
}

func TestHandleBedrockStreamingResponse_AfterTerminalFailureKeepsSuccess(t *testing.T) {
	for _, mode := range []string{"read_error", "idle"} {
		t.Run(mode, func(t *testing.T) {
			svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			body := append(bedrockChunkResponseFrameForTest(`{"type":"message_delta","usage":{"output_tokens":4}}`), bedrockChunkResponseFrameForTest(`{"type":"message_stop"}`)...)
			reader := io.Reader(io.MultiReader(bytes.NewReader(body), iotest.ErrReader(errors.New("upstream reset"))))
			if mode == "idle" {
				pipeReader, pipeWriter := io.Pipe()
				defer func() { _ = pipeReader.Close() }()
				defer func() { _ = pipeWriter.Close() }()
				go func() { _, _ = pipeWriter.Write(body) }()
				reader = pipeReader
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(reader)}
			result, err := svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-sonnet")
			require.NoError(t, err)
			require.Equal(t, 4, result.usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "message_stop")
		})
	}
}
