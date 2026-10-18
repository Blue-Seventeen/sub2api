package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOfficialRelayStopsAtCompleteTerminalBoundary(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, async := range []bool{false, true} {
			name := "native"
			if passthrough {
				name = "passthrough"
			}
			if async {
				name += "/async"
			}
			t.Run(name, func(t *testing.T) {
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
				if async {
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
				}
				svc := &OpenAIGatewayService{cfg: cfg}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				pr, pw := io.Pipe()
				defer func() { _ = pr.Close() }()
				defer func() { _ = pw.Close() }()
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
				done := make(chan error, 1)
				go func() {
					account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
					if passthrough {
						result, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "model", "model")
						if err == nil && (result.usage.InputTokens != 3 || result.usage.OutputTokens != 2) {
							err = errors.New("terminal usage was not preserved")
						}
						done <- err
						return
					}
					result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "model", "model")
					if err == nil && (result.usage.InputTokens != 3 || result.usage.OutputTokens != 2) {
						err = errors.New("terminal usage was not preserved")
					}
					done <- err
				}()
				_, err := pw.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_boundary\",\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}}\n\n"))
				require.NoError(t, err)
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(300 * time.Millisecond):
					_ = pw.Close()
					<-done
					t.Fatal("a complete terminal event must finish without waiting for upstream EOF")
				}
				require.Contains(t, rec.Body.String(), "resp_boundary")
				require.True(t, strings.HasSuffix(rec.Body.String(), "\n\n"))
			})
		}
	}
}

func TestOfficialRelayDoesNotForwardAfterFailedTerminal(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"error\":{\"code\":\"invalid_request\",\"message\":\"request failed\"},\"usage\":{\"input_tokens\":17,\"output_tokens\":5}}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"must-not-emit\",\"usage\":{\"input_tokens\":999,\"output_tokens\":999}}\n\n"
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			var usage *OpenAIUsage
			var err error
			if passthrough {
				var result *openaiStreamingResultPassthrough
				result, err = svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "model", "model")
				usage = result.usage
			} else {
				var result *openaiStreamingResult
				result, err = svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "model", "model")
				usage = result.usage
			}
			require.ErrorContains(t, err, "upstream response failed")
			require.Equal(t, 17, usage.InputTokens)
			require.Equal(t, 5, usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "partial")
			require.Equal(t, 1, strings.Count(rec.Body.String(), `"type":"response.failed"`))
			require.NotContains(t, rec.Body.String(), "must-not-emit")
		})
	}
}

func TestOfficialRelayProgressiveUsageMergesBeforeTerminal(t *testing.T) {
	svc := &OpenAIGatewayService{}
	usage := &OpenAIUsage{}
	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"input_tokens":17,"input_tokens_details":{"cached_tokens":3}}}}`), "response.in_progress", usage)
	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"output_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":4,"ephemeral_1h_input_tokens":6}}}}`), "response.in_progress", usage)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 5, CacheReadInputTokens: 3, CacheCreationInputTokens: 10, CacheCreation5mTokens: 4, CacheCreation1hTokens: 6}, *usage)
	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"input_tokens":0,"output_tokens":0}}}`), "response.completed", usage)
	require.Equal(t, 17, usage.InputTokens, "an all-zero terminal must preserve progressive usage")
	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"input_tokens":2,"output_tokens":0}}}`), "response.completed", usage)
	require.Equal(t, OpenAIUsage{InputTokens: 2}, *usage, "a nonzero terminal replaces the entire progressive snapshot")
}

func TestOfficialRelayProgressiveUsageSurvivesSSEToJSON(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte("event: response.in_progress\ndata: {\"response\":{\"usage\":{\"input_tokens\":17}}}\n\n" +
				"event: response.in_progress\ndata: {\"response\":{\"usage\":{\"output_tokens\":5}}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_usage\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n")
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			var usage *OpenAIUsage
			if passthrough {
				result, err := svc.handlePassthroughSSEToJSON(resp, c, account, body, "model", "model")
				require.NoError(t, err)
				usage = result.usage
			} else {
				result, err := svc.handleSSEToJSON(resp, c, account, body, "model", "model")
				require.NoError(t, err)
				usage = result.usage
			}
			require.Equal(t, 17, usage.InputTokens)
			require.Equal(t, 5, usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "resp_usage")
		})
	}
}

func TestOfficialRelayFirstOutputDeadlineSurvivesMetadataBacklog(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 1,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()
	// Keep a bounded metadata backlog ready when the one-shot deadline expires.
	preamble := strings.Repeat("data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_pending\"}}\n\n", 50000)
	body := &firstOutputCloseTrackingBody{ReadCloser: structReadCloser{Reader: io.MultiReader(strings.NewReader(preamble), pr), Closer: pr}, closed: make(chan struct{})}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	done := make(chan error, 1)
	go func() {
		_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now().Add(-950*time.Millisecond), "model", "model")
		done <- err
	}()
	select {
	case err := <-done:
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		require.Equal(t, http.StatusGatewayTimeout, failover.StatusCode)
		require.Contains(t, string(failover.ResponseBody), "first_output_timeout")
	case <-time.After(time.Second):
		_ = pw.Close()
		<-done
		t.Fatal("queued metadata consumed the one-shot first-output deadline")
	}
	require.Empty(t, rec.Body.String(), "metadata must remain private before semantic output")
}

type structReadCloser struct {
	io.Reader
	io.Closer
}
