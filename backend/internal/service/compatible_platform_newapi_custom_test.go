package service

import "testing"

func TestAccountUseNewAPIStyleInterface(t *testing.T) {
	enabledGroup := &Group{
		ID:                          1,
		Platform:                    PlatformMoonshot,
		Status:                      StatusActive,
		Hydrated:                    true,
		NewAPIStyleInterfaceEnabled: true,
	}
	tests := []struct {
		name     string
		account  *Account
		group    *Group
		expected bool
	}{
		{
			name:     "new api only platform is forced on",
			account:  &Account{Platform: PlatformPerplexity},
			expected: true,
		},
		{
			name: "existing compatible platform honors explicit switch",
			account: &Account{
				Platform: PlatformMoonshot,
				Extra: map[string]any{
					AccountExtraNewAPIStyleInterfaceEnabled: true,
				},
			},
			expected: true,
		},
		{
			name:     "existing compatible platform defaults off",
			account:  &Account{Platform: PlatformMoonshot},
			expected: false,
		},
		{
			name:     "group switch enables existing compatible platform",
			account:  &Account{Platform: PlatformMoonshot},
			group:    enabledGroup,
			expected: true,
		},
		{
			name: "group off preserves account-level switch",
			account: &Account{
				Platform: PlatformMoonshot,
				Extra: map[string]any{
					AccountExtraNewAPIStyleInterfaceEnabled: true,
				},
			},
			group: &Group{
				ID:                          1,
				Platform:                    PlatformMoonshot,
				Status:                      StatusActive,
				Hydrated:                    true,
				NewAPIStyleInterfaceEnabled: false,
			},
			expected: true,
		},
		{
			name: "unsupported platform ignores stale extra",
			account: &Account{
				Platform: "unsupported",
				Extra: map[string]any{
					AccountExtraNewAPIStyleInterfaceEnabled: true,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.account.UseNewAPIStyleInterfaceForGroup(tt.group); got != tt.expected {
				t.Fatalf("UseNewAPIStyleInterfaceForGroup() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApplyCompatibleUsageFallbackAppliesToNewAPIOnlyPlatforms(t *testing.T) {
	result := &ForwardResult{
		Usage: ClaudeUsage{
			OutputTokens: 7,
		},
		Model: "sonar",
	}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"sonar","messages":[{"role":"user","content":"hello from new api style"}]}`)), PlatformOpenAI)
	if err != nil {
		t.Fatalf("ParseGatewayRequest() error = %v", err)
	}
	account := &Account{Platform: PlatformPerplexity}

	applyCompatibleUsageFallback(result, account, nil, EstimateCompatibleInputTokensForPlatform(account.Platform, parsed))

	if result.Usage.InputTokens <= 0 {
		t.Fatalf("InputTokens = %d, want fallback estimate > 0", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 7 {
		t.Fatalf("OutputTokens = %d, want preserved output tokens 7", result.Usage.OutputTokens)
	}
}

func TestApplyCompatibleUsageFallbackDoesNotTouchNonCompatiblePlatforms(t *testing.T) {
	result := &ForwardResult{Usage: ClaudeUsage{OutputTokens: 7}, Model: "claude-sonnet-4"}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`)), PlatformOpenAI)
	if err != nil {
		t.Fatalf("ParseGatewayRequest() error = %v", err)
	}
	account := &Account{Platform: PlatformAnthropic}

	applyCompatibleUsageFallback(result, account, nil, EstimateCompatibleInputTokensForPlatform(account.Platform, parsed))

	if result.Usage.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0 for non-compatible platform", result.Usage.InputTokens)
	}
}

func TestNewAPIStyleGatewaySupportsExplicitCapabilityMatrix(t *testing.T) {
	svc := &NewAPIStyleGatewayService{}
	enabledExtra := map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true}
	openAIGroupEnabled := &Group{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, NewAPIStyleInterfaceEnabled: true}
	deepSeekGroupEnabled := &Group{ID: 2, Platform: PlatformDeepSeek, Status: StatusActive, Hydrated: true, NewAPIStyleInterfaceEnabled: true}
	zhipuGroupEnabled := &Group{ID: 3, Platform: PlatformZhipu, Status: StatusActive, Hydrated: true, NewAPIStyleInterfaceEnabled: true}
	aliGroup := &Group{ID: 4, Platform: PlatformAli, Status: StatusActive, Hydrated: true}

	tests := []struct {
		name     string
		account  *Account
		group    *Group
		route    NewAPIStyleRoute
		expected bool
	}{
		{
			name:     "existing compatible chat can use openai compatible path",
			account:  &Account{Platform: PlatformDeepSeek, Extra: enabledExtra},
			route:    NewAPIStyleRouteChatCompletions,
			expected: true,
		},
		{
			name:     "existing compatible embeddings can use openai compatible path",
			account:  &Account{Platform: PlatformDeepSeek, Extra: enabledExtra},
			route:    NewAPIStyleRouteEmbeddings,
			expected: true,
		},
		{
			name:     "group switch enables compatible embeddings without account extra",
			account:  &Account{Platform: PlatformDeepSeek},
			group:    deepSeekGroupEnabled,
			route:    NewAPIStyleRouteEmbeddings,
			expected: true,
		},
		{
			name:     "anthropic embeddings are unsupported",
			account:  &Account{Platform: PlatformAnthropic, Extra: enabledExtra},
			route:    NewAPIStyleRouteEmbeddings,
			expected: false,
		},
		{
			name:     "task platforms do not expose embeddings",
			account:  &Account{Platform: PlatformSuno},
			route:    NewAPIStyleRouteEmbeddings,
			expected: false,
		},
		{
			name:     "existing compatible messages falls back to old custom path",
			account:  &Account{Platform: PlatformMoonshot, Extra: enabledExtra},
			route:    NewAPIStyleRouteMessages,
			expected: false,
		},
		{
			name:     "openrouter messages are not blindly forwarded to unsupported path",
			account:  &Account{Platform: PlatformOpenRouter},
			route:    NewAPIStyleRouteMessages,
			expected: false,
		},
		{
			name:     "anthropic messages can use native messages path",
			account:  &Account{Platform: PlatformAnthropic, Extra: enabledExtra},
			route:    NewAPIStyleRouteMessages,
			expected: true,
		},
		{
			name:     "openai responses can use native responses path",
			account:  &Account{Platform: PlatformOpenAI, Extra: enabledExtra},
			route:    NewAPIStyleRouteResponses,
			expected: true,
		},
		{
			name:     "openai audio can use native audio path",
			account:  &Account{Platform: PlatformOpenAI, Extra: enabledExtra},
			route:    NewAPIStyleRouteAudio,
			expected: true,
		},
		{
			name:     "zhipu audio can use glm official audio path when enabled",
			account:  &Account{Platform: PlatformZhipu, Extra: enabledExtra},
			route:    NewAPIStyleRouteAudio,
			expected: true,
		},
		{
			name:     "group switch enables zhipu audio without account extra",
			account:  &Account{Platform: PlatformZhipu},
			group:    zhipuGroupEnabled,
			route:    NewAPIStyleRouteAudio,
			expected: true,
		},
		{
			name:     "zhipu audio still requires new api style switch",
			account:  &Account{Platform: PlatformZhipu},
			route:    NewAPIStyleRouteAudio,
			expected: false,
		},
		{
			name:     "qwen tts official route requires ali platform only",
			account:  &Account{Platform: PlatformAli},
			group:    aliGroup,
			route:    NewAPIStyleRouteQwenTTS,
			expected: true,
		},
		{
			name:     "qwen tts official route rejects other compatible platforms",
			account:  &Account{Platform: PlatformDeepSeek, Extra: enabledExtra},
			group:    deepSeekGroupEnabled,
			route:    NewAPIStyleRouteQwenTTS,
			expected: false,
		},
		{
			name:     "deepseek audio is unsupported",
			account:  &Account{Platform: PlatformDeepSeek, Extra: enabledExtra},
			route:    NewAPIStyleRouteAudio,
			expected: false,
		},
		{
			name:     "moonshot audio is unsupported",
			account:  &Account{Platform: PlatformMoonshot, Extra: enabledExtra},
			route:    NewAPIStyleRouteAudio,
			expected: false,
		},
		{
			name:     "group switch enables openai responses without account extra",
			account:  &Account{Platform: PlatformOpenAI},
			group:    openAIGroupEnabled,
			route:    NewAPIStyleRouteResponses,
			expected: true,
		},
		{
			name:     "group switch enables compatible chat without account extra",
			account:  &Account{Platform: PlatformDeepSeek},
			group:    deepSeekGroupEnabled,
			route:    NewAPIStyleRouteChatCompletions,
			expected: true,
		},
		{
			name:     "suno route requires suno platform",
			account:  &Account{Platform: PlatformSuno},
			route:    NewAPIStyleRouteSuno,
			expected: true,
		},
		{
			name:     "suno route rejects other new api platforms",
			account:  &Account{Platform: PlatformOpenRouter},
			route:    NewAPIStyleRouteSuno,
			expected: false,
		},
		{
			name:     "kling route requires kling platform",
			account:  &Account{Platform: PlatformKling},
			route:    NewAPIStyleRouteKling,
			expected: true,
		},
		{
			name:     "midjourney route requires midjourney platform",
			account:  &Account{Platform: PlatformMidjourney},
			route:    NewAPIStyleRouteMidjourney,
			expected: true,
		},
		{
			name:     "rerank is limited to siliconflow generic endpoint",
			account:  &Account{Platform: PlatformSiliconFlow},
			route:    NewAPIStyleRouteRerank,
			expected: true,
		},
		{
			name:     "openrouter rerank is unsupported",
			account:  &Account{Platform: PlatformOpenRouter},
			route:    NewAPIStyleRouteRerank,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := svc.SupportsForGroup(tt.account, tt.group, tt.route); got != tt.expected {
				t.Fatalf("SupportsForGroup(%s, %s) = %v, want %v", tt.account.Platform, tt.route, got, tt.expected)
			}
		})
	}
}

func TestShouldWarnCompatibleZeroCostUsesGroupNewAPIStyleSwitch(t *testing.T) {
	account := &Account{Platform: PlatformDeepSeek}
	group := &Group{
		ID:                          1,
		Platform:                    PlatformDeepSeek,
		Status:                      StatusActive,
		Hydrated:                    true,
		NewAPIStyleInterfaceEnabled: true,
	}
	result := &ForwardResult{
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}
	cost := &CostBreakdown{}

	if !shouldWarnCompatibleZeroCost(account, group, result, cost) {
		t.Fatalf("shouldWarnCompatibleZeroCost() = false, want true for group-enabled New-API style account")
	}
}

func TestNormalizeNewAPIStyleInterfaceExtra(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		extra    map[string]any
		wantKey  bool
		wantBool bool
	}{
		{
			name:     "new api only platform is persisted enabled",
			platform: PlatformPerplexity,
			extra: map[string]any{
				AccountExtraNewAPIStyleInterfaceEnabled: false,
			},
			wantKey:  true,
			wantBool: true,
		},
		{
			name:     "supported native platform keeps explicit true",
			platform: PlatformOpenAI,
			extra: map[string]any{
				AccountExtraNewAPIStyleInterfaceEnabled: true,
			},
			wantKey:  true,
			wantBool: true,
		},
		{
			name:     "supported native platform drops explicit false",
			platform: PlatformOpenAI,
			extra: map[string]any{
				AccountExtraNewAPIStyleInterfaceEnabled: false,
			},
			wantKey: false,
		},
		{
			name:     "unsupported platform drops stale key",
			platform: "unsupported",
			extra: map[string]any{
				AccountExtraNewAPIStyleInterfaceEnabled: true,
			},
			wantKey: false,
		},
		{
			name:     "nil new api only extra returns enabled map",
			platform: PlatformOpenRouter,
			extra:    nil,
			wantKey:  true,
			wantBool: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeNewAPIStyleInterfaceExtra(tt.platform, tt.extra)
			value, ok := got[AccountExtraNewAPIStyleInterfaceEnabled]
			if ok != tt.wantKey {
				t.Fatalf("key exists = %v, want %v; extra=%v", ok, tt.wantKey, got)
			}
			if tt.wantKey && value != tt.wantBool {
				t.Fatalf("key value = %v, want %v", value, tt.wantBool)
			}
		})
	}
}
