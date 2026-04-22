package service

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func normalizeTopPForCompatibleBody(body []byte, _ *Account, _ string) ([]byte, error) {
	return normalizeTopPForCompatibleBodyRaw(body), nil
}

func normalizeTopPForCompatibleBodyRaw(body []byte) []byte {
	topP := gjson.GetBytes(body, "top_p")
	if !topP.Exists() || topP.Type != gjson.Number {
		return body
	}
	value := topP.Float()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return body
	}
	switch {
	case value >= 1:
		updated, err := sjson.SetBytes(body, "top_p", 0.99)
		if err == nil {
			return updated
		}
	case value <= 0:
		updated, err := sjson.SetBytes(body, "top_p", 0.001)
		if err == nil {
			return updated
		}
	}
	return body
}

func normalizeStopStringToArray(body []byte) []byte {
	stop := gjson.GetBytes(body, "stop")
	if !stop.Exists() || stop.Type != gjson.String {
		return body
	}
	updated, err := sjson.SetBytes(body, "stop", []string{stop.String()})
	if err != nil {
		return body
	}
	return updated
}

func stripDataPrefixFromImageURLs(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	changed := false

	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, value := range typed {
				switch v := value.(type) {
				case string:
					if key == "url" || key == "image_url" {
						if trimmed, ok := stripDataImagePrefix(v); ok {
							typed[key] = trimmed
							changed = true
						}
					}
				default:
					walk(v)
				}
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}

	walk(payload)
	if !changed {
		return body
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func normalizeDeveloperRoleToSystem(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return body
	}

	changed := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, ok := msgMap["role"].(string)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			msgMap["role"] = "system"
			changed = true
		}
	}
	if !changed {
		return body
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func stripDataImagePrefix(v string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(v))
	if !strings.HasPrefix(lower, "data:image/") {
		return v, false
	}
	idx := strings.Index(lower, "base64,")
	if idx < 0 {
		return v, false
	}
	origIdx := strings.Index(strings.TrimSpace(v), "base64,")
	if origIdx < 0 {
		return v, false
	}
	trimmed := strings.TrimSpace(v)[origIdx+len("base64,"):]
	if trimmed == "" {
		return v, false
	}
	return trimmed, true
}
