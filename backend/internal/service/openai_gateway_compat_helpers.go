package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

const OpenAIFastTierUltrafast = "ultrafast"

func explicitModelMappingClaims(account Account, model string) bool {
	if account.Credentials == nil || model == "" {
		return false
	}
	mapped, ok := stringMappingFromRaw(account.Credentials["model_mapping"])[model]
	return ok && strings.TrimSpace(mapped) != ""
}

func openAIAlphaSearchSchedulingModel(account *Account, requestedModel string) string {
	return canonicalOpenAIAccountSchedulingModel(account, requestedModel)
}

func anthropicSpeedModel(parsed *ParsedRequest, result *ForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if parsed == nil {
		return ""
	}
	return parsed.Model
}

func anthropicSpeedServiceTier(account *Account, speed, model string) *string {
	if account == nil || account.Platform != PlatformAnthropic || speed != "fast" || account.IsBedrock() || !modelSupportsAnthropicFastMode(model) {
		return nil
	}
	tier := "fast"
	return &tier
}

// modelSupportsAnthropicFastMode reports whether the model accepts the
// Anthropic Fast (priority) tier. Fable/Mythos 5.2 price it, as do the Opus 5
// and Opus 4.8 catalogs.
func modelSupportsAnthropicFastMode(model string) bool {
	// 5.2 家族按完整模型 ID 精确匹配（含 provider/日期后缀归一），避免 "-5-2"
	// 前缀误伤 "-5-20" 这类不同版本。
	if claude.IsFable52(model) || claude.IsMythos52(model) {
		return true
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if !strings.Contains(model, "opus") {
		return false
	}
	// "opus-5" 必须先判：不能用裸 "5" 匹配，否则 claude-opus-4-5 会被误判。
	if strings.Contains(model, "opus-5") || strings.Contains(model, "opus5") {
		return true
	}
	return strings.Contains(model, "opus-4.8") || strings.Contains(model, "opus-4-8") || strings.Contains(model, "opus4.8")
}

func shouldForwardOpenAIResponsesViaRawChatCompletions(account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	// Model-routed providers use their protocol rules as the source of truth.
	// Probe metadata must not collapse a model that is explicitly routed to
	// Responses or Anthropic back to the generic Chat Completions path.
	if account.routesByModel() {
		return false
	}
	if account.RoutesProtocolByInbound() {
		switch account.GetAPIProtocol() {
		case APIProtocolChatCompletions:
			return true
		case APIProtocolAdaptive:
			return !account.SupportsNativeCNResponses()
		default:
			return false
		}
	}
	return !openai_compat.ShouldUseResponsesAPI(account.Extra)
}

func openAIStream403AccountFailure(payload []byte, message string) bool {
	return isOpenAIUpstreamAccessStateError(message, payload) || openAIStreamCredentialAuthFailure(payload)
}

func invalidNonStreamingJSONFailoverError(
	ctx context.Context,
	rateLimitService *RateLimitService,
	resp *http.Response,
	account *Account,
	body []byte,
	parseErr error,
	requestedModel ...string,
) error {
	const statusCode = http.StatusBadGateway
	accountID, accountName := int64(0), ""
	retryable := false
	if account != nil {
		accountID, accountName = account.ID, account.Name
		retryable = account.IsPoolMode() && account.IsPoolModeRetryableStatus(statusCode)
	}
	logger.LegacyPrintf("service.gateway", "Account %d(%s): upstream returned non-JSON 2xx response, attempting failover: status=%d request_id=%s error=%v", accountID, accountName, resp.StatusCode, resp.Header.Get("x-request-id"), parseErr)
	if rateLimitService != nil && account != nil {
		if len(requestedModel) > 0 {
			rateLimitService.HandleUpstreamError(ctx, account, statusCode, resp.Header, body, requestedModel[0])
		} else {
			rateLimitService.HandleUpstreamError(ctx, account, statusCode, resp.Header, body)
		}
	}
	return &UpstreamFailoverError{StatusCode: statusCode, ResponseBody: body, ResponseHeaders: resp.Header, RetryableOnSameAccount: retryable}
}
