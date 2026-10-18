package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"net/http"
)

// ValidateAccountCreateProxyConfig keeps UI-only values out of persisted bindings.
// Update requests retain their existing proxy_id=0 convention for clearing a binding.
func ValidateAccountCreateProxyConfig(proxyID *int64, extra map[string]any) error {
	if proxyID != nil && *proxyID <= 0 {
		return infraerrors.New(http.StatusBadRequest, "INVALID_PROXY_ID", "proxy_id must be a positive ID or null; automatic mode uses extra.auto_select_proxy=true")
	}
	if value, exists := extra["auto_select_proxy"]; exists {
		enabled, ok := value.(bool)
		if !ok {
			return infraerrors.New(http.StatusBadRequest, "INVALID_AUTO_SELECT_PROXY", "extra.auto_select_proxy must be a boolean")
		}
		if enabled && proxyID != nil {
			return infraerrors.New(http.StatusBadRequest, "PROXY_MODE_CONFLICT", "automatic proxy mode cannot include a fixed proxy_id")
		}
	}
	return nil
}

// ResolveAccountCreationAuthProxyID resolves a one-shot authorization connection,
// without changing the proxy mode that will be saved on the account.
func ResolveAccountCreationAuthProxyID(ctx context.Context, proxyID *int64, extra map[string]any) (*int64, error) {
	if err := ValidateAccountCreateProxyConfig(proxyID, extra); err != nil {
		return nil, err
	}
	if enabled, _ := extra["auto_select_proxy"].(bool); !enabled {
		return proxyID, nil
	}
	svc := GetDefaultProxyAutoProbeService()
	if svc == nil || !svc.snapshotConfig().Enabled {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "AUTO_PROXY_UNAVAILABLE", "Automatic proxy probing is disabled or not ready; enable probing and wait for a healthy proxy, or select a fixed proxy")
	}
	best := svc.getBestProxy(ctx)
	if best == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "AUTO_PROXY_UNAVAILABLE", "No healthy automatic proxy is available; wait for probing or select a fixed proxy")
	}
	id := best.ID
	return &id, nil
}

// resolveAuthorizationProxyURL rejects a disappeared binding instead of falling
// back to a direct connection between proxy selection and authorization.
func resolveAuthorizationProxyURL(ctx context.Context, repo ProxyRepository, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if *proxyID <= 0 || repo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "AUTH_PROXY_UNAVAILABLE", "The selected authorization proxy is unavailable; select another proxy")
	}
	proxy, err := repo.GetByID(ctx, *proxyID)
	if err != nil || proxy == nil {
		return "", infraerrors.New(http.StatusServiceUnavailable, "AUTH_PROXY_UNAVAILABLE", "The selected authorization proxy is unavailable; select another proxy")
	}
	return checkedAuthorizationProxyURL(ctx, proxy)
}

func checkedAuthorizationProxyURL(ctx context.Context, proxy *Proxy) (string, error) {
	value := ResolveProxyURL(ctx, proxy)
	if _, _, err := proxyurl.Parse(value); err != nil {
		return "", infraerrors.New(http.StatusServiceUnavailable, "AUTH_PROXY_UNAVAILABLE", "The selected authorization proxy is unavailable; select another proxy")
	}
	return value, nil
}

func resolveAccountAuthorizationProxyURL(ctx context.Context, account *Account, repo ProxyRepository) (string, error) {
	if account == nil {
		return "", nil
	}
	if !account.IsAutoSelectProxyEnabled() {
		if account.ProxyID != nil && account.Proxy != nil {
			return checkedAuthorizationProxyURL(ctx, account.Proxy)
		}
		return resolveAuthorizationProxyURL(ctx, repo, account.ProxyID)
	}
	// New-account privacy work runs asynchronously; do not mutate the returned account.
	resolved := *account
	proxy := resolveAccountProxy(ctx, &resolved, repo)
	if proxy == nil {
		return "", infraerrors.New(http.StatusServiceUnavailable, "AUTO_PROXY_UNAVAILABLE", "No healthy automatic proxy is available; wait for probing or select a fixed proxy")
	}
	return checkedAuthorizationProxyURL(ctx, proxy)
}
