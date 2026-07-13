package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func buildGrokResponsesURLForAccount(account *Account) (string, error) {
	if account == nil {
		return "", fmt.Errorf("grok account is required")
	}
	if account.Type == AccountTypeAPIKey {
		return xai.BuildAPIKeyResponsesURL(account.GetGrokBaseURL())
	}
	return xai.BuildResponsesURL(account.GetGrokBaseURL())
}

func buildGrokChatCompletionsURLForAccount(account *Account) (string, error) {
	if account == nil {
		return "", fmt.Errorf("grok account is required")
	}
	if account.Type == AccountTypeAPIKey {
		return xai.BuildAPIKeyChatCompletionsURL(account.GetGrokBaseURL())
	}
	return xai.BuildChatCompletionsURL(account.GetGrokBaseURL())
}

func buildGrokMediaURLForAccount(account *Account, endpoint GrokMediaEndpoint, requestID string) (string, error) {
	if account == nil {
		return "", fmt.Errorf("grok account is required")
	}
	baseURL := account.GetGrokBaseURL()
	if account.Type == AccountTypeAPIKey {
		switch endpoint {
		case GrokMediaEndpointImagesGenerations:
			return xai.BuildAPIKeyImagesGenerationsURL(baseURL)
		case GrokMediaEndpointImagesEdits:
			return xai.BuildAPIKeyImagesEditsURL(baseURL)
		case GrokMediaEndpointVideosGenerations:
			return xai.BuildAPIKeyVideosGenerationsURL(baseURL)
		case GrokMediaEndpointVideosEdits:
			return xai.BuildAPIKeyVideosEditsURL(baseURL)
		case GrokMediaEndpointVideosExtensions:
			return xai.BuildAPIKeyVideosExtensionsURL(baseURL)
		case GrokMediaEndpointVideoStatus:
			return xai.BuildAPIKeyVideoURL(baseURL, requestID)
		default:
			return "", fmt.Errorf("unsupported grok media endpoint: %s", endpoint)
		}
	}
	return endpoint.upstreamURL(baseURL, requestID)
}
