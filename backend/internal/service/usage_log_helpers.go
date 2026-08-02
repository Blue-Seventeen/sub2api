package service

import "strings"

func optionalTrimmedStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// optionalUpstreamModelPtr keeps the upstream model whenever it carries
// mapping information that is not represented by both stored model fields.
func optionalUpstreamModelPtr(raw, model, requestedModel string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if trimmed == strings.TrimSpace(model) && trimmed == strings.TrimSpace(requestedModel) {
		return nil
	}
	return &trimmed
}

func forwardResultBillingModel(requestedModel, upstreamModel string) string {
	if trimmed := strings.TrimSpace(requestedModel); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(upstreamModel)
}

func optionalInt64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}
