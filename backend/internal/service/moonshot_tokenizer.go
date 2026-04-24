package service

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

const (
	moonshotTokenizerPattern = `(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\n\p{L}\p{N}]?\p{L}+|\p{N}{1,3}| ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+`
	moonshotBaseTokenID      = 163584
	moonshotReservedTokens   = 256
	moonshotImageTokenCost   = 256
)

// Source:
// - https://huggingface.co/moonshotai/Kimi-K2-Instruct/raw/main/tokenization_kimi.py
// - https://huggingface.co/moonshotai/Kimi-K2-Instruct/raw/main/chat_template.jinja
//
//go:embed tokenizer_assets/kimi_k2.tiktoken.model
var moonshotTokenizerModel []byte

var (
	moonshotTokenizerOnce sync.Once
	moonshotTokenizerInst *tiktoken.Tiktoken
	moonshotTokenizerErr  error
)

type moonshotPromptMessage struct {
	Role    string
	Content string
}

func EstimateCompatibleInputTokensForPlatform(platform string, parsed *ParsedRequest) int {
	if strings.EqualFold(strings.TrimSpace(platform), PlatformMoonshot) {
		return EstimateMoonshotCompatibleInputTokens(parsed)
	}
	return EstimateCompatibleInputTokens(parsed)
}

func EstimateMoonshotCompatibleInputTokens(parsed *ParsedRequest) int {
	if parsed == nil {
		return 1
	}

	tokenizer, err := getMoonshotTokenizer()
	if err != nil {
		slog.Warn("moonshot tokenizer unavailable", "error", err)
		return EstimateCompatibleInputTokens(parsed)
	}

	prompt, imageCount := buildMoonshotPromptFromParsedRequest(parsed)
	if prompt == "" {
		return EstimateCompatibleInputTokens(parsed)
	}

	tokens := tokenizer.Encode(prompt, moonshotAllowedSpecialTokens(), nil)
	total := len(tokens) + imageCount*moonshotImageTokenCost
	if total < 1 {
		return 1
	}
	return total
}

func getMoonshotTokenizer() (*tiktoken.Tiktoken, error) {
	moonshotTokenizerOnce.Do(func() {
		ranks, err := parseMoonshotMergeableRanks(moonshotTokenizerModel)
		if err != nil {
			moonshotTokenizerErr = err
			return
		}

		specialTokens := moonshotSpecialTokens()
		bpe, err := tiktoken.NewCoreBPE(ranks, specialTokens, moonshotTokenizerPattern)
		if err != nil {
			moonshotTokenizerErr = err
			return
		}

		encoding := &tiktoken.Encoding{
			Name:           "moonshot_kimi_k2",
			PatStr:         moonshotTokenizerPattern,
			MergeableRanks: ranks,
			SpecialTokens:  specialTokens,
			ExplicitNVocab: moonshotBaseTokenID + moonshotReservedTokens,
		}

		specialTokenSet := make(map[string]any, len(specialTokens))
		for token := range specialTokens {
			specialTokenSet[token] = true
		}

		moonshotTokenizerInst = tiktoken.NewTiktoken(bpe, encoding, specialTokenSet)
	})

	return moonshotTokenizerInst, moonshotTokenizerErr
}

func parseMoonshotMergeableRanks(contents []byte) (map[string]int, error) {
	lines := strings.Split(string(contents), "\n")
	ranks := make(map[string]int, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, err
		}
		rank, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, err
		}
		ranks[string(token)] = rank
	}
	return ranks, nil
}

func moonshotSpecialTokens() map[string]int {
	known := map[int]string{
		163584: "[BOS]",
		163585: "[EOS]",
		163586: "<|im_end|>",
		163587: "<|im_user|>",
		163588: "<|im_assistant|>",
		163590: "<|start_header_id|>",
		163591: "<|end_header_id|>",
		163593: "[EOT]",
		163594: "<|im_system|>",
		163595: "<|tool_calls_section_begin|>",
		163596: "<|tool_calls_section_end|>",
		163597: "<|tool_call_begin|>",
		163598: "<|tool_call_argument_begin|>",
		163599: "<|tool_call_end|>",
		163601: "<|im_middle|>",
		163838: "[UNK]",
		163839: "[PAD]",
	}

	out := make(map[string]int, moonshotReservedTokens)
	for id := moonshotBaseTokenID; id < moonshotBaseTokenID+moonshotReservedTokens; id++ {
		token, ok := known[id]
		if !ok {
			token = "<|reserved_token_" + strconv.Itoa(id) + "|>"
		}
		out[token] = id
	}
	return out
}

func moonshotAllowedSpecialTokens() []string {
	return []string{"all"}
}

func buildMoonshotPromptFromParsedRequest(parsed *ParsedRequest) (string, int) {
	messages := make([]moonshotPromptMessage, 0, len(parsed.Messages)+1)
	imageCount := 0

	if parsed.HasSystem {
		systemText, systemImages := flattenMoonshotContent(parsed.System)
		imageCount += systemImages
		messages = append(messages, moonshotPromptMessage{
			Role:    "system",
			Content: systemText,
		})
	}

	for _, raw := range parsed.Messages {
		msg, images, ok := normalizeMoonshotPromptMessage(raw)
		if !ok {
			continue
		}
		imageCount += images
		messages = append(messages, msg)
	}

	if len(messages) == 0 {
		return "", imageCount
	}

	var sb strings.Builder
	sb.WriteString("[BOS]")

	offset := 0
	if messages[0].Role == "system" {
		sb.WriteString("<|im_system|>")
		sb.WriteString(messages[0].Content)
		sb.WriteString("<|im_end|>")
		offset = 1
	}

	for i := offset; i < len(messages); i++ {
		roleToken := moonshotRoleToken(messages[i].Role)
		sb.WriteString(roleToken)
		sb.WriteString(messages[i].Content)
		sb.WriteString("<|im_end|>")
	}

	sb.WriteString("<|im_assistant|>")
	return sb.String(), imageCount
}

func moonshotRoleToken(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "<|im_assistant|>"
	case "system":
		return "<|im_system|>"
	default:
		return "<|im_user|>"
	}
}

func normalizeMoonshotPromptMessage(raw any) (moonshotPromptMessage, int, bool) {
	msgMap, ok := raw.(map[string]any)
	if !ok {
		return moonshotPromptMessage{}, 0, false
	}

	role := strings.ToLower(strings.TrimSpace(stringValue(msgMap["role"])))
	if role == "" {
		role = "user"
	}

	content, images := flattenMoonshotContent(msgMap["content"])
	if toolCalls, exists := msgMap["tool_calls"]; exists {
		toolCallsText := compactJSON(toolCalls)
		if toolCallsText != "" {
			content += toolCallsText
		}
	}
	if name := stringValue(msgMap["name"]); name != "" {
		content = name + ":" + content
	}

	return moonshotPromptMessage{
		Role:    role,
		Content: content,
	}, images, true
}

func flattenMoonshotContent(raw any) (string, int) {
	switch v := raw.(type) {
	case nil:
		return "", 0
	case string:
		return v, 0
	case []any:
		var sb strings.Builder
		imageCount := 0
		for _, item := range v {
			text, images := flattenMoonshotContent(item)
			sb.WriteString(text)
			imageCount += images
		}
		return sb.String(), imageCount
	case map[string]any:
		typeName := strings.ToLower(strings.TrimSpace(stringValue(v["type"])))
		switch typeName {
		case "text", "input_text", "output_text":
			return stringValue(v["text"]), 0
		case "thinking":
			return stringValue(v["thinking"]), 0
		case "image", "input_image", "image_url":
			return "", 1
		case "tool_use":
			name := stringValue(v["name"])
			input := compactJSON(v["input"])
			return name + input, 0
		case "tool_result":
			return flattenMoonshotContent(v["content"])
		}

		if _, ok := v["image_url"]; ok {
			return "", 1
		}
		if text := stringValue(v["text"]); text != "" {
			return text, 0
		}
		if thinking := stringValue(v["thinking"]); thinking != "" {
			return thinking, 0
		}
		return compactJSON(v), 0
	default:
		return compactJSON(v), 0
	}
}

func compactJSON(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func stringValue(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
