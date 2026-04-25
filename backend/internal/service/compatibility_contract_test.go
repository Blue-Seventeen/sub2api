package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderCapabilityForPlatform(t *testing.T) {
	tests := []struct {
		name string
		got  ProviderCapability
		want ProviderCapability
	}{
		{
			name: "anthropic",
			got:  ProviderCapabilityForPlatform(PlatformAnthropic),
			want: ProviderCapability{
				Platform:              PlatformAnthropic,
				SupportsMessages:      true,
				SupportsTools:         true,
				SupportsReasoning:     true,
				SupportsLateUsage:     true,
				PreferredFallbackLane: []CompatibilityRoute{CompatibilityRouteAnthropicNativeMessages},
			},
		},
		{
			name: "openai",
			got:  ProviderCapabilityForPlatform(PlatformOpenAI),
			want: ProviderCapability{
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
			},
		},
		{
			name: "gemini",
			got:  ProviderCapabilityForPlatform(PlatformGemini),
			want: ProviderCapability{
				Platform:              PlatformGemini,
				SupportsTools:         true,
				SupportsReasoning:     true,
				PreferredFallbackLane: []CompatibilityRoute{CompatibilityRouteAnthropicNativeMessages},
			},
		},
		{
			name: "ali",
			got:  ProviderCapabilityForPlatform(PlatformAli),
			want: ProviderCapability{
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
			},
		},
		{
			name: "moonshot",
			got:  ProviderCapabilityForPlatform(PlatformMoonshot),
			want: ProviderCapability{
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
			},
		},
		{
			name: "unknown_platform_normalizes",
			got:  ProviderCapabilityForPlatform("  CuStOm-UpStReAm  "),
			want: ProviderCapability{
				Platform: "custom-upstream",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

func TestCompatibilityEnumNormalize(t *testing.T) {
	require.Equal(t, ClientProfileCherryStudio, ClientProfile("  ChErRy_StUdIo ").Normalize())
	require.Equal(t, InboundProtocolOpenAIImages, InboundProtocol("  OPENAI_IMAGES ").Normalize())
	require.Equal(t, CompatibilityRouteCompatibleChatFallback, CompatibilityRoute(" compatible_chat_fallback ").Normalize())
	require.Equal(t, UpstreamTransportWSV2, UpstreamTransport(" WS_V2 ").Normalize())
}
