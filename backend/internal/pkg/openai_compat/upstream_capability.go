// Package openai_compat provides capability detection helpers for OpenAI-compatible upstreams.
package openai_compat

// AccountResponsesSupport describes effective support for OpenAI Responses API.
type AccountResponsesSupport int

const (
	// ResponsesSupportUnknown means the account has not been probed yet.
	ResponsesSupportUnknown AccountResponsesSupport = iota

	// ResponsesSupportYes means the upstream supports /v1/responses.
	ResponsesSupportYes

	// ResponsesSupportNo means the upstream should use /v1/chat/completions directly.
	ResponsesSupportNo
)

// ResponsesSupportMode describes account-level Responses API routing override.
type ResponsesSupportMode string

const (
	// ResponsesSupportModeAuto follows the automatic capability probe result.
	ResponsesSupportModeAuto ResponsesSupportMode = "auto"

	// ResponsesSupportModeForceResponses forces routing to /v1/responses.
	ResponsesSupportModeForceResponses ResponsesSupportMode = "force_responses"

	// ResponsesSupportModeForceChatCompletions forces routing to /v1/chat/completions.
	ResponsesSupportModeForceChatCompletions ResponsesSupportMode = "force_chat_completions"
)

// ExtraKeyResponsesMode stores the manual Responses API routing override in accounts.extra.
const ExtraKeyResponsesMode = "openai_responses_mode"

// ExtraKeyResponsesSupported stores the automatic capability probe result in accounts.extra.
const ExtraKeyResponsesSupported = "openai_responses_supported"

// NormalizeResponsesSupportMode normalizes account-level Responses API routing overrides.
func NormalizeResponsesSupportMode(mode string) ResponsesSupportMode {
	switch ResponsesSupportMode(mode) {
	case ResponsesSupportModeForceResponses:
		return ResponsesSupportModeForceResponses
	case ResponsesSupportModeForceChatCompletions:
		return ResponsesSupportModeForceChatCompletions
	default:
		return ResponsesSupportModeAuto
	}
}

// ResolveResponsesSupport reads manual routing override first, then automatic probe result.
func ResolveResponsesSupport(extra map[string]any) AccountResponsesSupport {
	if extra == nil {
		return ResponsesSupportUnknown
	}
	if mode, ok := extra[ExtraKeyResponsesMode].(string); ok {
		switch NormalizeResponsesSupportMode(mode) {
		case ResponsesSupportModeForceResponses:
			return ResponsesSupportYes
		case ResponsesSupportModeForceChatCompletions:
			return ResponsesSupportNo
		}
	}
	v, ok := extra[ExtraKeyResponsesSupported]
	if !ok {
		return ResponsesSupportUnknown
	}
	supported, ok := v.(bool)
	if !ok {
		return ResponsesSupportUnknown
	}
	if supported {
		return ResponsesSupportYes
	}
	return ResponsesSupportNo
}

// ShouldUseResponsesAPI returns false only when the account is known or forced to use chat completions.
func ShouldUseResponsesAPI(extra map[string]any) bool {
	return ResolveResponsesSupport(extra) != ResponsesSupportNo
}
