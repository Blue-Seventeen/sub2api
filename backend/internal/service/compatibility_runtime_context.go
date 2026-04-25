package service

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	compatibilityClientProfileContextKey = "compatibility_client_profile"
	compatibilityInboundProtocolKey      = "compatibility_inbound_protocol"
	compatibilityRouteContextKey         = "compatibility_route"
	compatibilityFallbackChainContextKey = "compatibility_fallback_chain"
	compatibilityUpstreamTransportKey    = "compatibility_upstream_transport"
)

type CompatibilityLogFields struct {
	ClientProfile      string
	CompatibilityRoute string
	FallbackChain      string
	UpstreamTransport  string
}

func SetCompatibilityClientProfile(c *gin.Context, profile ClientProfile) {
	if c == nil {
		return
	}
	normalized := profile.Normalize()
	if normalized == ClientProfileUnknown {
		return
	}
	c.Set(compatibilityClientProfileContextKey, string(normalized))
	if c.Request != nil {
		c.Request = c.Request.WithContext(WithClientProfile(c.Request.Context(), normalized))
	}
}

func GetCompatibilityClientProfile(c *gin.Context) ClientProfile {
	if c == nil {
		return ClientProfileUnknown
	}
	if raw, ok := c.Get(compatibilityClientProfileContextKey); ok {
		switch v := raw.(type) {
		case ClientProfile:
			return v.Normalize()
		case string:
			return ClientProfile(v).Normalize()
		}
	}
	if c.Request != nil {
		if profile, ok := ClientProfileFromContext(c.Request.Context()); ok {
			return profile
		}
	}
	return ClientProfileUnknown
}

func SetCompatibilityInboundProtocol(c *gin.Context, protocol InboundProtocol) {
	if c == nil {
		return
	}
	normalized := protocol.Normalize()
	if normalized == InboundProtocolUnknown {
		return
	}
	c.Set(compatibilityInboundProtocolKey, string(normalized))
	if c.Request != nil {
		c.Request = c.Request.WithContext(WithInboundProtocol(c.Request.Context(), normalized))
	}
}

func GetCompatibilityInboundProtocol(c *gin.Context) InboundProtocol {
	if c == nil {
		return InboundProtocolUnknown
	}
	if raw, ok := c.Get(compatibilityInboundProtocolKey); ok {
		switch v := raw.(type) {
		case InboundProtocol:
			return v.Normalize()
		case string:
			return InboundProtocol(v).Normalize()
		}
	}
	if c.Request != nil {
		if protocol, ok := InboundProtocolFromContext(c.Request.Context()); ok {
			return protocol
		}
	}
	return InboundProtocolUnknown
}

func SetCompatibilityRoute(c *gin.Context, route CompatibilityRoute) {
	if c == nil {
		return
	}
	normalized := route.Normalize()
	if normalized == CompatibilityRouteUnknown {
		return
	}
	c.Set(compatibilityRouteContextKey, string(normalized))
}

func GetCompatibilityRoute(c *gin.Context) CompatibilityRoute {
	if c == nil {
		return CompatibilityRouteUnknown
	}
	if raw, ok := c.Get(compatibilityRouteContextKey); ok {
		switch v := raw.(type) {
		case CompatibilityRoute:
			return v.Normalize()
		case string:
			return CompatibilityRoute(v).Normalize()
		}
	}
	return CompatibilityRouteUnknown
}

func SetCompatibilityUpstreamTransport(c *gin.Context, transport UpstreamTransport) {
	if c == nil {
		return
	}
	normalized := transport.Normalize()
	if normalized == UpstreamTransportUnknown {
		return
	}
	c.Set(compatibilityUpstreamTransportKey, string(normalized))
}

func GetCompatibilityUpstreamTransport(c *gin.Context) UpstreamTransport {
	if c == nil {
		return UpstreamTransportUnknown
	}
	if raw, ok := c.Get(compatibilityUpstreamTransportKey); ok {
		switch v := raw.(type) {
		case UpstreamTransport:
			return v.Normalize()
		case string:
			return UpstreamTransport(v).Normalize()
		}
	}
	return UpstreamTransportUnknown
}

func AppendCompatibilityFallbackStage(c *gin.Context, stage string) {
	if c == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return
	}
	existing := GetCompatibilityFallbackStages(c)
	if len(existing) > 0 && existing[len(existing)-1] == stage {
		return
	}
	existing = append(existing, stage)
	c.Set(compatibilityFallbackChainContextKey, existing)
}

func SetCompatibilityFallbackStages(c *gin.Context, stages []string) {
	if c == nil {
		return
	}
	normalized := make([]string, 0, len(stages))
	for _, stage := range stages {
		if stage = strings.TrimSpace(stage); stage != "" {
			normalized = append(normalized, stage)
		}
	}
	if len(normalized) == 0 {
		return
	}
	c.Set(compatibilityFallbackChainContextKey, normalized)
}

func GetCompatibilityFallbackStages(c *gin.Context) []string {
	if c == nil {
		return nil
	}
	raw, ok := c.Get(compatibilityFallbackChainContextKey)
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case string:
		parts := strings.Split(v, "->")
		out := make([]string, 0, len(parts))
		for _, item := range parts {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}

func GetCompatibilityFallbackChain(c *gin.Context) string {
	return strings.Join(GetCompatibilityFallbackStages(c), " -> ")
}

func CompatibilityLogFieldsFromContext(c *gin.Context) CompatibilityLogFields {
	return CompatibilityLogFields{
		ClientProfile:      strings.TrimSpace(string(GetCompatibilityClientProfile(c))),
		CompatibilityRoute: strings.TrimSpace(string(GetCompatibilityRoute(c))),
		FallbackChain:      strings.TrimSpace(GetCompatibilityFallbackChain(c)),
		UpstreamTransport:  strings.TrimSpace(string(GetCompatibilityUpstreamTransport(c))),
	}
}
