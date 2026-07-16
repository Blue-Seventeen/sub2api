package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompatibleUsageFallbackMarksEstimatedInputTokens(t *testing.T) {
	result := &ForwardResult{}
	account := &Account{
		Platform: PlatformMoonshot,
		Extra: map[string]any{
			AccountExtraNewAPIStyleInterfaceEnabled: true,
		},
	}

	applyCompatibleUsageFallback(result, account, &Group{Platform: PlatformMoonshot}, 17)

	require.Equal(t, 17, result.Usage.InputTokens)
	require.True(t, result.UsageEstimated)
}

func TestCompatibleUsageFallbackMarksDefaultCompatiblePlatformWithoutNewAPIStyle(t *testing.T) {
	result := &ForwardResult{}
	account := &Account{Platform: PlatformZhipu}

	applyCompatibleUsageFallback(result, account, &Group{Platform: PlatformZhipu}, 23)

	require.Equal(t, 23, result.Usage.InputTokens)
	require.True(t, result.UsageEstimated)
}
