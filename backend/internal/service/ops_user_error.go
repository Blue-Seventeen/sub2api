package service

import (
	"net"
	"regexp"
	"strings"
	"time"
)

const userVisibleNetworkRedaction = "*.*.*.*"

var (
	userVisibleURLRegex         = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)(?:[^@\s/"'?]+@)?(\[[0-9a-f:.%]+\]|[a-z0-9.-]+)(:\d{1,5})?`)
	userVisibleAPIKeyValueRegex = regexp.MustCompile(`(?i)(\b(?:[a-z0-9_-]*(?:api[_-]?key|apikey)|x-api-key|x-goog-api-key|key)\b"?\s*[:=]\s*"?)([^"'\s,}&\]]{6,})("?)`)
	userVisibleTokenValueRegex  = regexp.MustCompile(`(?i)(\b(?:access[_-]?token|refresh[_-]?token|id[_-]?token|session[_-]?token|token)\b"?\s*[:=]\s*"?)([^"'\s,}&\]]{6,})("?)`)
	userVisibleSkTokenRegex     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`)
	userVisibleBearerRegex      = regexp.MustCompile(`(?i)\b(bearer\s+)([A-Za-z0-9._~+/\-]{8,}=*)`)
	userVisibleBasicAuthRegex   = regexp.MustCompile(`(?i)\b(basic\s+)([A-Za-z0-9+/]{8,}=*)`)
	userVisibleSecretValueRegex = regexp.MustCompile(`(?i)(\b(?:client[_-]?secret|password|passwd|cookie|set-cookie)\b"?\s*[:=]\s*"?)([^"'\s,}&\]]{6,})("?)`)
	userVisibleJWTRegex         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	userVisibleAccountRegex     = regexp.MustCompile(`(?i)(\b(?:account[_-]?id|accountid|account[_-]?name|accountname)\b"?\s*[:=]\s*"?)([^,"\s}]+)("?)`)
	userVisibleDomainRegex      = regexp.MustCompile(`\b(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}\b`)
	userVisibleLocalhostRegex   = regexp.MustCompile(`(?i)\blocalhost\b`)
	userVisibleIPv4Regex        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	userVisibleBracketIPv6Regex = regexp.MustCompile(`\[[0-9A-Fa-f:.%]+\]`)
	userVisibleBareIPv6Regex    = regexp.MustCompile(`[0-9A-Fa-f]{0,4}:[0-9A-Fa-f:.%]{2,}`)
)

// UserErrorRequest is the redacted failed-request view returned to end users.
// UserErrorRequest 是面向终端用户的"错误请求"精简脱敏视图（白名单）。
// 严禁包含 account / api_key_prefix / upstream_endpoint / user_email 等
// 敏感或内部字段。注：message（网关标准化错误描述）与 key_name
// （用户自有 API Key 名称，KeysView 中本就可见）经产品决策对该用户开放；
// user_agent / group_name / request_type / stream 均为该用户自己请求的属性。
// client_ip、account、upstream、credential 相关字段不进入该结构；
// error_body 仅在详情接口（GetUserErrorRequestDetail）按归属校验后返回。
type UserErrorRequest struct {
	ID              int64     `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	Model           string    `json:"model"`
	InboundEndpoint string    `json:"inbound_endpoint"`
	StatusCode      int       `json:"status_code"`
	Category        string    `json:"category"`
	Platform        string    `json:"platform"`
	Message         string    `json:"message"`
	KeyName         string    `json:"key_name"`
	KeyDeleted      bool      `json:"key_deleted"`
	GroupName       string    `json:"group_name,omitempty"`
	RequestType     *int16    `json:"request_type,omitempty"`
	Stream          bool      `json:"stream"`
	UserAgent       string    `json:"user_agent,omitempty"`
}

// UserErrorRequestList is the paginated failed-request result for users.
type UserErrorRequestList struct {
	Items    []*UserErrorRequest `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// MapUserErrorCategory maps internal ops dimensions to stable user-facing codes.
func MapUserErrorCategory(phase, errType string) string {
	switch phase {
	case "auth":
		return "auth"
	case "routing":
		return "service_unavailable"
	case "account_auth", "upstream", "network":
		return "upstream"
	case "internal":
		return "internal"
	case "request":
		switch errType {
		case "rate_limit_error":
			return "rate_limit"
		case "billing_error", "subscription_error":
			return "quota"
		case "invalid_request_error":
			return "invalid_request"
		case "cyber_policy":
			return "cyber"
		}
	}
	return "other"
}

// CategoryToFilter maps user-facing category codes back to ops filter dimensions.
func CategoryToFilter(category string) (phases []string, errorTypes []string) {
	switch category {
	case "auth":
		return []string{"auth"}, nil
	case "service_unavailable":
		return []string{"routing"}, nil
	case "upstream":
		return []string{"account_auth", "upstream", "network"}, nil
	case "internal":
		return []string{"internal"}, nil
	case "rate_limit":
		return nil, []string{"rate_limit_error"}
	case "quota":
		return nil, []string{"billing_error", "subscription_error"}
	case "invalid_request":
		return nil, []string{"invalid_request_error"}
	case "cyber":
		return []string{"request"}, []string{"cyber_policy"}
	default:
		return nil, nil
	}
}

func sanitizeUserVisibleErrorText(text string) string {
	if text == "" {
		return ""
	}
	return sanitizeUserVisibleNetworkText(sanitizeUserVisibleCredentialText(text))
}

func sanitizeUserVisibleCredentialText(text string) string {
	out := text
	out = replaceUserVisibleCapture(out, userVisibleAPIKeyValueRegex, 2, maskUserVisibleSecret)
	out = replaceUserVisibleCapture(out, userVisibleTokenValueRegex, 2, maskUserVisibleSecret)
	out = replaceUserVisibleCapture(out, userVisibleBearerRegex, 2, maskUserVisibleSecret)
	out = replaceUserVisibleCapture(out, userVisibleBasicAuthRegex, 2, maskUserVisibleSecret)
	out = replaceUserVisibleCapture(out, userVisibleSecretValueRegex, 2, maskUserVisibleSecret)
	out = userVisibleJWTRegex.ReplaceAllStringFunc(out, maskUserVisibleSecret)
	out = userVisibleSkTokenRegex.ReplaceAllStringFunc(out, maskUserVisibleSecret)
	out = replaceUserVisibleCapture(out, userVisibleAccountRegex, 2, func(string) string { return "***" })
	return out
}

func sanitizeUserVisibleNetworkText(text string) string {
	out := text
	out = userVisibleURLRegex.ReplaceAllString(out, `${1}`+userVisibleNetworkRedaction+`${3}`)
	out = userVisibleDomainRegex.ReplaceAllString(out, userVisibleNetworkRedaction)
	out = userVisibleLocalhostRegex.ReplaceAllString(out, userVisibleNetworkRedaction)
	out = userVisibleIPv4Regex.ReplaceAllStringFunc(out, func(match string) string {
		if ip := net.ParseIP(match); ip != nil && ip.To4() != nil {
			return userVisibleNetworkRedaction
		}
		return match
	})
	out = userVisibleBracketIPv6Regex.ReplaceAllStringFunc(out, func(match string) string {
		host := strings.Trim(match, "[]")
		if idx := strings.LastIndex(host, "%"); idx >= 0 {
			host = host[:idx]
		}
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			return "[" + userVisibleNetworkRedaction + "]"
		}
		return match
	})
	out = userVisibleBareIPv6Regex.ReplaceAllStringFunc(out, func(match string) string {
		host := match
		if idx := strings.LastIndex(host, "%"); idx >= 0 {
			host = host[:idx]
		}
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			return userVisibleNetworkRedaction
		}
		return match
	})
	return out
}

func SanitizeUserVisibleErrorText(text string) string {
	return sanitizeUserVisibleErrorText(text)
}

func replaceUserVisibleCapture(input string, re *regexp.Regexp, group int, repl func(string) string) string {
	return re.ReplaceAllStringFunc(input, func(match string) string {
		indexes := re.FindStringSubmatchIndex(match)
		startIdx := group * 2
		endIdx := startIdx + 1
		if len(indexes) <= endIdx || indexes[startIdx] < 0 || indexes[endIdx] < 0 {
			return match
		}
		start := indexes[startIdx]
		end := indexes[endIdx]
		return match[:start] + repl(match[start:end]) + match[end:]
	})
}

func maskUserVisibleSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "***"
	}

	prefixLen := 6
	suffixLen := 4
	if strings.HasPrefix(strings.ToLower(secret), "sk-") {
		prefixLen = 8
	}
	if len(secret) <= prefixLen+suffixLen {
		if len(secret) <= 4 {
			return "***"
		}
		return secret[:2] + "..." + secret[len(secret)-2:]
	}
	return secret[:prefixLen] + "..." + secret[len(secret)-suffixLen:]
}

// ToUserErrorRequest converts an internal ops error to the user-safe list view.
func ToUserErrorRequest(e *OpsErrorLog) *UserErrorRequest {
	if e == nil {
		return nil
	}
	model := e.RequestedModel
	if model == "" {
		model = e.Model
	}
	return &UserErrorRequest{
		ID:              e.ID,
		CreatedAt:       e.CreatedAt,
		Model:           model,
		InboundEndpoint: e.InboundEndpoint,
		StatusCode:      e.StatusCode,
		Category:        MapUserErrorCategory(e.Phase, e.Type),
		Platform:        e.Platform,
		Message:         sanitizeUserVisibleErrorText(e.Message),
		KeyName:         e.APIKeyName,
		KeyDeleted:      e.APIKeyDeleted,
		GroupName:       e.GroupName,
		RequestType:     e.RequestType,
		Stream:          e.Stream,
		UserAgent:       e.UserAgent,
	}
}

// UserErrorRequestDetail is the redacted detail view returned to end users.
type UserErrorRequestDetail struct {
	UserErrorRequest
	ErrorBody          string `json:"error_body"`
	UpstreamStatusCode *int   `json:"upstream_status_code,omitempty"`
}

// ToUserErrorRequestDetail converts an internal ops detail to a user-safe detail view.
func ToUserErrorRequestDetail(e *OpsErrorLogDetail) *UserErrorRequestDetail {
	if e == nil {
		return nil
	}
	base := ToUserErrorRequest(&e.OpsErrorLog)
	return &UserErrorRequestDetail{
		UserErrorRequest:   *base,
		ErrorBody:          sanitizeUserVisibleErrorText(e.ErrorBody),
		UpstreamStatusCode: e.UpstreamStatusCode,
	}
}
