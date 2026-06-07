package service

func usageTokensHaveTokenUsage(tokens UsageTokens) bool {
	return tokens.InputTokens > 0 ||
		tokens.OutputTokens > 0 ||
		tokens.CacheCreationTokens > 0 ||
		tokens.CacheReadTokens > 0 ||
		tokens.CacheCreation5mTokens > 0 ||
		tokens.CacheCreation1hTokens > 0 ||
		tokens.ImageOutputTokens > 0
}
