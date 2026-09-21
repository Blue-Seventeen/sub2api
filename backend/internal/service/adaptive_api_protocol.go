package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// isCNNativeAnthropicIngress reports whether the inbound Anthropic protocol
// must stay on the provider's native Anthropic endpoint. Explicit Anthropic
// and adaptive protocols are authoritative over stale capability probes.
func isCNNativeAnthropicIngress(account *Account) bool {
	return account != nil && account.IsCNProvider() &&
		(account.IsAnthropicProtocol() || account.IsAdaptiveAPIProtocol())
}

func isAdaptiveResponsesShapedChatIngress(account *Account, body []byte) bool {
	return account != nil && account.IsAdaptiveAPIProtocol() &&
		!account.SupportsNativeCNResponses() &&
		!gjson.GetBytes(body, "messages").Exists() &&
		gjson.GetBytes(body, "input").Exists()
}

func responsesShapedChatBody(body []byte) ([]byte, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		return nil, err
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, err
	}
	return json.Marshal(chatReq)
}
