package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	ProxySubscriptionStatusActive   = StatusActive
	ProxySubscriptionStatusInactive = "inactive"

	ProxySubscriptionNodeStatusActive   = StatusActive
	ProxySubscriptionNodeStatusInactive = "inactive"

	ManagedProxyRuntimeStatusDisabled = "disabled"
	ManagedProxyRuntimeStatusStarting = "starting"
	ManagedProxyRuntimeStatusRunning  = "running"
	ManagedProxyRuntimeStatusStopped  = "stopped"
	ManagedProxyRuntimeStatusError    = "error"
)

var (
	ErrProxySubscriptionNotFound = errors.New("proxy subscription not found")
	ErrManagedProxyUnavailable   = errors.New("managed proxy runtime unavailable")
	ErrManagedProxyDisabled      = errors.New("managed proxy runtime disabled")
)

type ProxySubscription struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	SubscriptionURL    string `json:"subscription_url"`
	Status             string `json:"status"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
	TestURL            string `json:"test_url"`
	Revision           int64  `json:"revision"`
	LastError          string `json:"last_error,omitempty"`
	// RawDNSConfig stores sanitized DNS policy fields used by Mihomo node resolution.
	// It is intentionally hidden from API JSON responses.
	RawDNSConfig string                  `json:"-"`
	ProxyID      *int64                  `json:"proxy_id,omitempty"`
	ProxyIDs     []int64                 `json:"proxy_ids,omitempty"`
	Nodes        []ProxySubscriptionNode `json:"nodes,omitempty"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

type ProxySubscriptionNode struct {
	ID             int64     `json:"id"`
	SubscriptionID int64     `json:"subscription_id"`
	ProxyID        *int64    `json:"proxy_id,omitempty"`
	NodeKey        string    `json:"node_key"`
	Name           string    `json:"name"`
	ProviderName   string    `json:"provider_name"`
	Type           string    `json:"type"`
	Server         string    `json:"server,omitempty"`
	Port           int       `json:"port,omitempty"`
	Username       string    `json:"username,omitempty"`
	Password       string    `json:"password,omitempty"`
	RawConfig      string    `json:"-"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateProxySubscriptionInput struct {
	Name               string `json:"name"`
	SubscriptionURL    string `json:"subscription_url"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
	TestURL            string `json:"test_url"`
}

type UpdateProxySubscriptionInput struct {
	Name               string `json:"name"`
	SubscriptionURL    string `json:"subscription_url"`
	Status             string `json:"status"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
	TestURL            string `json:"test_url"`
}

type ManagedProxyRuntimeStatus struct {
	Enabled        bool       `json:"enabled"`
	NodeID         string     `json:"node_id,omitempty"`
	Status         string     `json:"status"`
	SubscriptionID int64      `json:"subscription_id,omitempty"`
	LocalURL       string     `json:"local_url,omitempty"`
	Port           int        `json:"port,omitempty"`
	Revision       int64      `json:"revision,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at,omitempty"`
}

type ProxySubscriptionRepository interface {
	CreateWithNodes(ctx context.Context, sub *ProxySubscription, nodes []ProxySubscriptionNode) (*ProxySubscription, []Proxy, error)
	List(ctx context.Context) ([]ProxySubscription, error)
	ListActive(ctx context.Context) ([]ProxySubscription, error)
	Get(ctx context.Context, id int64) (*ProxySubscription, error)
	GetByProxyID(ctx context.Context, proxyID int64) (*ProxySubscription, error)
	Update(ctx context.Context, sub *ProxySubscription) error
	UpdateWithNodes(ctx context.Context, sub *ProxySubscription, nodes []ProxySubscriptionNode) error
	DeleteWithProxy(ctx context.Context, id int64) error
	IncrementRevision(ctx context.Context, id int64) (*ProxySubscription, error)
	SyncNodes(ctx context.Context, subscriptionID int64, rawDNSConfig string, nodes []ProxySubscriptionNode) ([]Proxy, error)
	GetNodeByProxyID(ctx context.Context, proxyID int64) (*ProxySubscriptionNode, error)
	SetNodeStatusByProxyID(ctx context.Context, proxyID int64, status string) error
	ListProxyIDsBySubscriptionID(ctx context.Context, subscriptionID int64) ([]int64, error)
	SetLastError(ctx context.Context, id int64, message string) error
}

type ManagedProxyResolver interface {
	ResolveProxyURL(ctx context.Context, subscriptionID int64) (string, error)
	GetStatus(subscriptionID int64) ManagedProxyRuntimeStatus
	Reload(subscriptionID int64)
}

type managedProxyNoopResolver struct{}

func (managedProxyNoopResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return "", ErrManagedProxyDisabled
}

func (managedProxyNoopResolver) GetStatus(subscriptionID int64) ManagedProxyRuntimeStatus {
	return ManagedProxyRuntimeStatus{
		Enabled:        false,
		NodeID:         CurrentNodeID(),
		Status:         ManagedProxyRuntimeStatusDisabled,
		SubscriptionID: subscriptionID,
		UpdatedAt:      time.Now(),
	}
}

func (managedProxyNoopResolver) Reload(int64) {}

func ValidateProxySubscriptionInput(input *CreateProxySubscriptionInput, defaultRefreshIntervalSec int, defaultTestURL string) error {
	if input == nil {
		return errors.New("subscription is required")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return errors.New("name is required")
	}
	input.SubscriptionURL = strings.TrimSpace(input.SubscriptionURL)
	if err := validateHTTPURL(input.SubscriptionURL, "subscription_url"); err != nil {
		return err
	}
	if input.RefreshIntervalSec <= 0 {
		input.RefreshIntervalSec = defaultRefreshIntervalSec
	}
	if input.RefreshIntervalSec <= 0 {
		input.RefreshIntervalSec = 3600
	}
	input.TestURL = strings.TrimSpace(input.TestURL)
	if input.TestURL == "" {
		input.TestURL = strings.TrimSpace(defaultTestURL)
	}
	if input.TestURL == "" {
		input.TestURL = "https://www.gstatic.com/generate_204"
	}
	return validateHTTPURL(input.TestURL, "test_url")
}

func ValidateProxySubscriptionUpdate(input *UpdateProxySubscriptionInput) error {
	if input == nil {
		return errors.New("subscription is required")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.SubscriptionURL = strings.TrimSpace(input.SubscriptionURL)
	input.Status = strings.TrimSpace(input.Status)
	input.TestURL = strings.TrimSpace(input.TestURL)
	if input.SubscriptionURL != "" {
		if err := validateHTTPURL(input.SubscriptionURL, "subscription_url"); err != nil {
			return err
		}
	}
	if input.Status != "" && input.Status != StatusActive && input.Status != ProxySubscriptionStatusInactive {
		return errors.New("status must be active or inactive")
	}
	if input.RefreshIntervalSec < 0 {
		return errors.New("refresh_interval_sec must be positive")
	}
	if input.TestURL != "" {
		if err := validateHTTPURL(input.TestURL, "test_url"); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPURL(raw, field string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s is invalid", field)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("%s must include host", field)
	}
	if u.Fragment != "" {
		return fmt.Errorf("%s must not include fragment", field)
	}
	return nil
}

func ResolveProxyURL(ctx context.Context, proxy *Proxy) string {
	if proxy == nil {
		return ""
	}
	if proxy.IsManagedMihomoSubscription() {
		resolver := GetDefaultManagedProxyResolver()
		if resolver != nil {
			if proxyURL, err := resolver.ResolveProxyURL(ctx, *proxy.SubscriptionID); err == nil && proxyURL != "" {
				return managedProxyURLForProxy(proxyURL, proxy)
			}
		}
		return fmt.Sprintf("managed-proxy://unavailable/%d", *proxy.SubscriptionID)
	}
	return proxy.URL()
}

func managedProxyURLForProxy(proxyURL string, proxy *Proxy) string {
	if proxy == nil || proxy.Username == "" || proxy.Password == "" {
		return proxyURL
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return proxyURL
	}
	u.User = url.UserPassword(proxy.Username, proxy.Password)
	return u.String()
}
