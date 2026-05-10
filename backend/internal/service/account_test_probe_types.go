package service

import "strings"

const (
	AccountTestTypeAuto      = "auto"
	AccountTestTypeText      = "text"
	AccountTestTypeImage     = "image"
	AccountTestTypeASR       = "asr"
	AccountTestTypeTTS       = "tts"
	AccountTestTypeVideo     = "video"
	AccountTestTypeTask      = "task"
	AccountTestTypeEmbedding = "embedding"
	AccountTestTypeRerank    = "rerank"
)

func normalizeAccountTestType(testType string) string {
	normalized := strings.ToLower(strings.TrimSpace(testType))
	switch normalized {
	case "", AccountTestTypeAuto:
		return AccountTestTypeAuto
	case AccountTestTypeText:
		return AccountTestTypeText
	case AccountTestTypeImage:
		return AccountTestTypeImage
	case AccountTestTypeASR:
		return AccountTestTypeASR
	case AccountTestTypeTTS:
		return AccountTestTypeTTS
	case AccountTestTypeVideo:
		return AccountTestTypeVideo
	case AccountTestTypeTask:
		return AccountTestTypeTask
	case AccountTestTypeEmbedding:
		return AccountTestTypeEmbedding
	case AccountTestTypeRerank:
		return AccountTestTypeRerank
	default:
		return normalized
	}
}
