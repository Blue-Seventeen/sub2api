package service

import "strings"

type ClientProfile string

const (
	ClientProfileUnknown          ClientProfile = ""
	ClientProfileClaudeCode       ClientProfile = "claude_code"
	ClientProfileCodex            ClientProfile = "codex"
	ClientProfileCherryStudio     ClientProfile = "cherry_studio"
	ClientProfileGenericOpenAI    ClientProfile = "generic_openai"
	ClientProfileGenericAnthropic ClientProfile = "generic_anthropic"
)

type InboundProtocol string

const (
	InboundProtocolUnknown               InboundProtocol = ""
	InboundProtocolAnthropicMessages     InboundProtocol = "anthropic_messages"
	InboundProtocolOpenAIResponsesHTTP   InboundProtocol = "openai_responses_http"
	InboundProtocolOpenAIResponsesWS     InboundProtocol = "openai_responses_ws"
	InboundProtocolOpenAIChatCompletions InboundProtocol = "openai_chat_completions"
	InboundProtocolOpenAIImages          InboundProtocol = "openai_images"
)

type CanonicalPart string

const (
	CanonicalPartText       CanonicalPart = "text"
	CanonicalPartToolCall   CanonicalPart = "tool_call"
	CanonicalPartToolResult CanonicalPart = "tool_result"
	CanonicalPartReasoning  CanonicalPart = "reasoning"
	CanonicalPartImage      CanonicalPart = "image"
	CanonicalPartFile       CanonicalPart = "file"
)

type CompatibilityRoute string

const (
	CompatibilityRouteUnknown                     CompatibilityRoute = ""
	CompatibilityRouteAnthropicNativeMessages     CompatibilityRoute = "anthropic_native_messages"
	CompatibilityRouteAnthropicResponsesBridge    CompatibilityRoute = "anthropic_responses_bridge"
	CompatibilityRouteAnthropicChatBridge         CompatibilityRoute = "anthropic_chat_completions_bridge"
	CompatibilityRouteOpenAIResponsesNative       CompatibilityRoute = "openai_responses_native"
	CompatibilityRouteOpenAIResponsesWSV2         CompatibilityRoute = "openai_responses_ws_v2"
	CompatibilityRouteOpenAIChatCompletionsNative CompatibilityRoute = "openai_chat_completions_native"
	CompatibilityRouteOpenAIImagesNative          CompatibilityRoute = "openai_images_native"
	CompatibilityRouteOpenAIImagesResponsesBridge CompatibilityRoute = "openai_images_responses_bridge"
	CompatibilityRouteCompatibleMessagesNative    CompatibilityRoute = "compatible_messages_native"
	CompatibilityRouteCompatibleResponsesNative   CompatibilityRoute = "compatible_responses_native"
	CompatibilityRouteCompatibleChatNative        CompatibilityRoute = "compatible_chat_native"
	CompatibilityRouteCompatibleEndpointRelay     CompatibilityRoute = "compatible_endpoint_relay"
	CompatibilityRouteCompatibleChatFallback      CompatibilityRoute = "compatible_chat_fallback"
)

type UpstreamTransport string

const (
	UpstreamTransportUnknown       UpstreamTransport = ""
	UpstreamTransportHTTPJSON      UpstreamTransport = "http_json"
	UpstreamTransportHTTPMultipart UpstreamTransport = "http_multipart"
	UpstreamTransportSSE           UpstreamTransport = "sse"
	UpstreamTransportWSV2          UpstreamTransport = "ws_v2"
)

type ProviderCapability struct {
	Platform              string
	SupportsMessages      bool
	SupportsChat          bool
	SupportsResponses     bool
	SupportsImages        bool
	SupportsTools         bool
	SupportsReasoning     bool
	SupportsPreviousResp  bool
	SupportsWS            bool
	SupportsLateUsage     bool
	SupportsImageURL      bool
	SupportsImageB64JSON  bool
	PreferredFallbackLane []CompatibilityRoute
}

func (p ClientProfile) Normalize() ClientProfile {
	switch strings.ToLower(strings.TrimSpace(string(p))) {
	case string(ClientProfileClaudeCode):
		return ClientProfileClaudeCode
	case string(ClientProfileCodex):
		return ClientProfileCodex
	case string(ClientProfileCherryStudio):
		return ClientProfileCherryStudio
	case string(ClientProfileGenericOpenAI):
		return ClientProfileGenericOpenAI
	case string(ClientProfileGenericAnthropic):
		return ClientProfileGenericAnthropic
	default:
		return ClientProfileUnknown
	}
}

func (p InboundProtocol) Normalize() InboundProtocol {
	switch strings.ToLower(strings.TrimSpace(string(p))) {
	case string(InboundProtocolAnthropicMessages):
		return InboundProtocolAnthropicMessages
	case string(InboundProtocolOpenAIResponsesHTTP):
		return InboundProtocolOpenAIResponsesHTTP
	case string(InboundProtocolOpenAIResponsesWS):
		return InboundProtocolOpenAIResponsesWS
	case string(InboundProtocolOpenAIChatCompletions):
		return InboundProtocolOpenAIChatCompletions
	case string(InboundProtocolOpenAIImages):
		return InboundProtocolOpenAIImages
	default:
		return InboundProtocolUnknown
	}
}

func (r CompatibilityRoute) Normalize() CompatibilityRoute {
	switch strings.ToLower(strings.TrimSpace(string(r))) {
	case string(CompatibilityRouteAnthropicNativeMessages):
		return CompatibilityRouteAnthropicNativeMessages
	case string(CompatibilityRouteAnthropicResponsesBridge):
		return CompatibilityRouteAnthropicResponsesBridge
	case string(CompatibilityRouteAnthropicChatBridge):
		return CompatibilityRouteAnthropicChatBridge
	case string(CompatibilityRouteOpenAIResponsesNative):
		return CompatibilityRouteOpenAIResponsesNative
	case string(CompatibilityRouteOpenAIResponsesWSV2):
		return CompatibilityRouteOpenAIResponsesWSV2
	case string(CompatibilityRouteOpenAIChatCompletionsNative):
		return CompatibilityRouteOpenAIChatCompletionsNative
	case string(CompatibilityRouteOpenAIImagesNative):
		return CompatibilityRouteOpenAIImagesNative
	case string(CompatibilityRouteOpenAIImagesResponsesBridge):
		return CompatibilityRouteOpenAIImagesResponsesBridge
	case string(CompatibilityRouteCompatibleMessagesNative):
		return CompatibilityRouteCompatibleMessagesNative
	case string(CompatibilityRouteCompatibleResponsesNative):
		return CompatibilityRouteCompatibleResponsesNative
	case string(CompatibilityRouteCompatibleChatNative):
		return CompatibilityRouteCompatibleChatNative
	case string(CompatibilityRouteCompatibleEndpointRelay):
		return CompatibilityRouteCompatibleEndpointRelay
	case string(CompatibilityRouteCompatibleChatFallback):
		return CompatibilityRouteCompatibleChatFallback
	default:
		return CompatibilityRouteUnknown
	}
}

func (t UpstreamTransport) Normalize() UpstreamTransport {
	switch strings.ToLower(strings.TrimSpace(string(t))) {
	case string(UpstreamTransportHTTPJSON):
		return UpstreamTransportHTTPJSON
	case string(UpstreamTransportHTTPMultipart):
		return UpstreamTransportHTTPMultipart
	case string(UpstreamTransportSSE):
		return UpstreamTransportSSE
	case string(UpstreamTransportWSV2):
		return UpstreamTransportWSV2
	default:
		return UpstreamTransportUnknown
	}
}

func ProviderCapabilityForPlatform(platform string) ProviderCapability {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformAnthropic:
		return ProviderCapability{
			Platform:              PlatformAnthropic,
			SupportsMessages:      true,
			SupportsTools:         true,
			SupportsReasoning:     true,
			SupportsLateUsage:     true,
			PreferredFallbackLane: []CompatibilityRoute{CompatibilityRouteAnthropicNativeMessages},
		}
	case PlatformOpenAI:
		return ProviderCapability{
			Platform:             PlatformOpenAI,
			SupportsChat:         true,
			SupportsResponses:    true,
			SupportsImages:       true,
			SupportsTools:        true,
			SupportsReasoning:    true,
			SupportsPreviousResp: true,
			SupportsWS:           true,
			SupportsImageURL:     true,
			SupportsImageB64JSON: true,
			PreferredFallbackLane: []CompatibilityRoute{
				CompatibilityRouteOpenAIResponsesNative,
				CompatibilityRouteOpenAIChatCompletionsNative,
				CompatibilityRouteOpenAIImagesNative,
			},
		}
	case PlatformGemini:
		return ProviderCapability{
			Platform:              PlatformGemini,
			SupportsTools:         true,
			SupportsReasoning:     true,
			PreferredFallbackLane: []CompatibilityRoute{CompatibilityRouteAnthropicNativeMessages},
		}
	case PlatformZhipu:
		return ProviderCapability{
			Platform:          PlatformZhipu,
			SupportsMessages:  true,
			SupportsChat:      true,
			SupportsTools:     true,
			SupportsReasoning: true,
			SupportsLateUsage: true,
			PreferredFallbackLane: []CompatibilityRoute{
				CompatibilityRouteCompatibleMessagesNative,
				CompatibilityRouteCompatibleEndpointRelay,
			},
		}
	case PlatformAli:
		return ProviderCapability{
			Platform:          PlatformAli,
			SupportsMessages:  true,
			SupportsChat:      true,
			SupportsResponses: true,
			SupportsTools:     true,
			SupportsReasoning: true,
			PreferredFallbackLane: []CompatibilityRoute{
				CompatibilityRouteCompatibleResponsesNative,
				CompatibilityRouteCompatibleChatNative,
				CompatibilityRouteCompatibleEndpointRelay,
			},
		}
	case PlatformMoonshot:
		return ProviderCapability{
			Platform:          PlatformMoonshot,
			SupportsMessages:  true,
			SupportsChat:      true,
			SupportsTools:     true,
			SupportsReasoning: true,
			SupportsLateUsage: true,
			PreferredFallbackLane: []CompatibilityRoute{
				CompatibilityRouteCompatibleMessagesNative,
				CompatibilityRouteCompatibleEndpointRelay,
				CompatibilityRouteCompatibleChatFallback,
			},
		}
	case PlatformDeepSeek, PlatformVolcEngine:
		return ProviderCapability{
			Platform:          strings.ToLower(strings.TrimSpace(platform)),
			SupportsChat:      true,
			SupportsTools:     true,
			SupportsReasoning: true,
			PreferredFallbackLane: []CompatibilityRoute{
				CompatibilityRouteCompatibleChatNative,
				CompatibilityRouteCompatibleEndpointRelay,
			},
		}
	default:
		return ProviderCapability{Platform: strings.ToLower(strings.TrimSpace(platform))}
	}
}
