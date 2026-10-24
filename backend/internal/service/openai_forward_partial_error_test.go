//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the public Forward dispatch and its outer result construction, not
// only the already-tested inner SSE/JSON handlers.
func TestOpenAIForwardPreservesAcceptedPartialResult(t *testing.T) {
	for _, route := range []string{"native", "passthrough", "grok"} {
		for _, stream := range []bool{false, true} {
			for _, outcome := range []string{"billable-error", "unaccepted-error", "request-error", "ruled-error", "semantic-error", "success"} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", route, stream, outcome), func(t *testing.T) {
					account := rawChatCompletionsTestAccount()
					account.Credentials["pool_mode"] = true
					account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
					account.Extra = map[string]any{"openai_responses_mode": "force_responses", "openai_passthrough": route == "passthrough"}
					model := "gpt-5.4"
					if route == "grok" {
						account.Platform = PlatformGrok
						model = "grok-3"
					}
					responseID := fmt.Sprintf("resp_outer_%s_%v_%s", route, stream, outcome)
					usage := `{"input_tokens":9,"output_tokens":1}`
					if outcome == "unaccepted-error" || outcome == "semantic-error" || outcome == "request-error" || outcome == "ruled-error" {
						usage = `{}`
					}
					output := `[]`
					if outcome == "semantic-error" {
						output = `[{"type":"function_call","id":"call_1","call_id":"call_1","name":"lookup","arguments":"{}"}]`
					}
					status, eventType, failure := "failed", "response.failed", `,"error":{"code":503,"type":"server_error","message":"temporary upstream failure"}`
					if outcome == "request-error" {
						failure = `,"error":{"code":400,"type":"invalid_request_error","message":"missing required parameter"}`
					}
					if outcome == "success" {
						status, eventType, failure = "completed", "response.completed", ""
						output = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`
					}
					payload := fmt.Sprintf("data: {\"type\":%q,\"response\":{\"id\":%q,\"model\":%q,\"status\":%q,\"output\":%s,\"usage\":%s%s}}\n\n", eventType, responseID, model, status, output, usage, failure)
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"outer-partial-rid"}}, Body: io.NopCloser(strings.NewReader(payload))}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
					body := []byte(fmt.Sprintf(`{"model":%q,"input":"hello","stream":%v,"reasoning":{"effort":"high"},"service_tier":"default"}`, model, stream))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					groupID := int64(8877)
					c.Set("api_key", &APIKey{ID: 9901, GroupID: &groupID})
					SetOpenAIHTTPResponseOwner(c, 9902, 9901)
					if outcome == "ruled-error" {
						bindPassthroughRule(c, account.Platform, []string{"temporary upstream failure"}, http.StatusTeapot)
					}
					result, err := svc.Forward(context.Background(), c, account, body)
					require.Len(t, upstream.requests, 1, "the outer wrapper must not replay an accepted terminal")
					require.True(t, strings.HasSuffix(upstream.requests[0].URL.Path, "/responses"))
					if outcome == "unaccepted-error" {
						var failover *UpstreamFailoverError
						require.ErrorAs(t, err, &failover)
						require.Nil(t, result)
						require.False(t, c.Writer.Written())
						return
					}
					if outcome == "success" {
						require.NoError(t, err)
					} else {
						var failover *UpstreamFailoverError
						require.Error(t, err)
						require.NotErrorAs(t, err, &failover)
						require.Contains(t, rec.Body.String(), "error")
						require.NotContains(t, rec.Body.String(), "response.completed")
					}
					if outcome == "request-error" || outcome == "ruled-error" {
						require.Nil(t, result, "writing an error is not upstream model acceptance or a billable request")
						return
					}
					require.NotNil(t, result, "ordinary error must not erase the inner accepted partial result")
					if outcome == "semantic-error" {
						require.Equal(t, OpenAIUsage{}, result.Usage)
					} else {
						require.Equal(t, 9, result.Usage.InputTokens)
						require.Equal(t, 1, result.Usage.OutputTokens)
					}
					require.Equal(t, "outer-partial-rid", result.RequestID)
					require.Equal(t, model, result.Model)
					if route == "passthrough" {
						require.Empty(t, result.UpstreamModel, "unmapped passthrough keeps its existing empty override")
					} else {
						require.Equal(t, model, result.UpstreamModel)
					}
					require.Equal(t, stream, result.Stream)
					if route == "grok" {
						require.Nil(t, result.ReasoningEffort, "grok-3 does not support reasoning effort")
					} else {
						require.NotNil(t, result.ReasoningEffort)
						require.Equal(t, "high", *result.ReasoningEffort)
					}
					bound, bindErr := svc.getOpenAIWSStateStore().GetResponseAccount(context.Background(), groupID, responseID)
					require.NoError(t, bindErr)
					if outcome == "success" && route != "grok" {
						require.Equal(t, account.ID, bound)
					} else {
						require.Zero(t, bound, "failed response must not acquire a success-only owner binding")
					}
				})
			}
		}
	}
}
