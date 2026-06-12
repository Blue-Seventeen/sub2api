package service

import (
	"net"
	"regexp"
	"strings"
	"time"
)

const userVisibleNetworkRedaction = "*.*.*.*"

var (
	userVisibleURLHostRegex     = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)(?:[^@\s/"']+@)?(\[[0-9a-f:.%]+\]|[a-z0-9.-]+)(:\d{1,5})?`)
	userVisibleDomainRegex      = regexp.MustCompile(`\b(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}\b`)
	userVisibleIPv4Regex        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	userVisibleBracketIPv6Regex = regexp.MustCompile(`\[[0-9A-Fa-f:.%]+\]`)
	userVisibleBareIPv6Regex    = regexp.MustCompile(`[0-9A-Fa-f]{0,4}:[0-9A-Fa-f:.%]{2,}`)
)

// UserErrorRequest is the redacted failed-request view returned to end users.
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
	case "upstream", "network":
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
		return []string{"upstream", "network"}, nil
	case "internal":
		return []string{"internal"}, nil
	case "rate_limit":
		return nil, []string{"rate_limit_error"}
	case "quota":
		return nil, []string{"billing_error", "subscription_error"}
	case "invalid_request":
		return nil, []string{"invalid_request_error"}
	default:
		return nil, nil
	}
}

func sanitizeUserVisibleErrorText(text string) string {
	if text == "" {
		return ""
	}

	out := userVisibleURLHostRegex.ReplaceAllString(text, `${1}`+userVisibleNetworkRedaction+`${3}`)
	out = userVisibleDomainRegex.ReplaceAllString(out, userVisibleNetworkRedaction)
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
