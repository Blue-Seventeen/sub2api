package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesEveryConcretePlatform(t *testing.T) {
	require.ElementsMatch(t, []string{
		"anthropic",
		"openai",
		"gemini",
		"antigravity",
		"grok",
		"kimi",
		"zhipu",
		"deepseek",
		"minimax",
		"volcengine", "ali", "moonshot", "perplexity", "mistral",
		"siliconflow", "openrouter", "suno", "kling", "midjourney",
	}, AllPlatforms())
}
