//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompactFallbackPreservesAcceptanceAndRuleBarriers(t *testing.T) {
	for _, route := range []string{"native-stream", "native-buffered", "passthrough-stream"} {
		for _, boundary := range []string{"eligible", "usage", "provider-usage", "terminal-output", "prior-output", "global-rule", "custom-exclude", "canceled", "committed", "policy", "not-explicit"} {
			t.Run(route+"/"+boundary, func(t *testing.T) {
				c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
				if boundary != "not-explicit" {
					MarkOpenAINativeCompactionV2(c)
				}
				account, _ := compactFallbackManagedProxyAccount()
				prefix, extra, errType := "", "", "invalid_request_error"
				switch boundary {
				case "usage":
					extra = `,"usage":{"input_tokens":9,"output_tokens":1}`
				case "provider-usage":
					prefix = "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"billable_seconds\":2}}}\n\n"
				case "terminal-output":
					extra = `,"output":[{"type":"function_call","id":"call_1","name":"lookup","arguments":"{}"}]`
				case "prior-output":
					prefix = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"accepted\"}\n\n"
				case "global-rule":
					bindPassthroughRule(c, PlatformOpenAI, []string{"context window exceeded"}, http.StatusTeapot)
				case "custom-exclude":
					account.Type = AccountTypeAPIKey
					account.Credentials["custom_error_codes_enabled"] = true
					account.Credentials["custom_error_codes"] = []any{float64(503)}
					require.True(t, account.IsCustomErrorCodesEnabled())
					require.False(t, account.ShouldHandleErrorCode(http.StatusBadRequest))
				case "canceled":
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				case "committed":
					MarkResponseCommitted(c)
				case "policy":
					errType = "content_policy_violation"
				}
				data := prefix + fmt.Sprintf("data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"context_length_exceeded\",\"type\":%q,\"message\":\"context window exceeded\"}%s}}\n\n", errType, extra)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
				var err error
				switch route {
				case "native-stream":
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				case "native-buffered":
					_, err = svc.handleSSEToJSON(resp, c, account, []byte(data), "gpt-5.5", "gpt-5.5")
				case "passthrough-stream":
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				}
				require.Error(t, err)
				var signal *openAICompactFallbackSignal
				if boundary == "eligible" {
					require.ErrorAs(t, err, &signal, "explicit compact repair must retain its existing model fallback")
					require.False(t, c.Writer.Written())
				} else {
					require.NotErrorAs(t, err, &signal, "acceptance and explicit policy barriers prohibit compact model fallback")
				}
			})
		}
	}
}
