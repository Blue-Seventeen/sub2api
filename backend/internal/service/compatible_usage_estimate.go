package service

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// EstimateCompatibleInputTokens provides a lightweight local estimation for
// compatible-provider prompt tokens. It is primarily used as a fallback when
// certain relay/upstream paths omit prompt usage in successful responses.
func EstimateCompatibleInputTokens(parsed *ParsedRequest) int {
	if parsed == nil {
		return 1
	}
	totalChars := 0
	totalImages := 0
	if system, ok := parsed.SystemValue(); ok {
		totalChars += estimateCompatibleChars(system, &totalImages)
		totalChars += 16
	}
	var messages []any
	if err := parsed.DecodeMessages(&messages); err == nil {
		for _, msg := range messages {
			totalChars += estimateCompatibleChars(msg, &totalImages)
			totalChars += 12
		}
	}
	if totalChars == 0 {
		totalChars = parsed.Body.Len()
	}
	tokens := totalChars/4 + 1 + totalImages*256
	if tokens < 1 {
		return 1
	}
	return tokens
}

func estimateCompatibleChars(value any, imageCount *int) int {
	switch v := value.(type) {
	case nil:
		return 0
	case string:
		return utf8.RuneCountInString(v)
	case []any:
		total := 0
		for _, item := range v {
			total += estimateCompatibleChars(item, imageCount)
		}
		return total
	case map[string]any:
		total := 0
		if t, ok := v["type"].(string); ok && (strings.Contains(strings.ToLower(t), "image") || t == "input_image") {
			if imageCount != nil {
				*imageCount = *imageCount + 1
			}
		}
		for _, item := range v {
			total += estimateCompatibleChars(item, imageCount)
		}
		return total
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		return len(raw)
	}
}
