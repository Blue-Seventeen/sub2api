package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	ProxySourceManual             = "manual"
	ProxySourceMihomoSubscription = "mihomo_subscription"
)

type Proxy struct {
	ID             int64
	Name           string
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	Status         string
	SourceType     string
	SubscriptionID *int64
	RuntimeStatus  *ManagedProxyRuntimeStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

func (p *Proxy) IsManagedMihomoSubscription() bool {
	return p != nil && p.SourceType == ProxySourceMihomoSubscription && p.SubscriptionID != nil && *p.SubscriptionID > 0
}

func (p *Proxy) URL() string {
	if p != nil && p.IsManagedMihomoSubscription() {
		return fmt.Sprintf("managed-proxy://subscription/%d", *p.SubscriptionID)
	}
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

type ProxyWithAccountCount struct {
	Proxy
	AccountCount             int64
	ActiveEgressAccountCount int64
	LatencyMs                *int64
	LatencyStatus            string
	LatencyMessage           string
	IPAddress                string
	Country                  string
	CountryCode              string
	Region                   string
	City                     string
	QualityStatus            string
	QualityScore             *int
	QualityGrade             string
	QualitySummary           string
	QualityChecked           *int64
}

type ProxyAccountSummary struct {
	ID       int64
	Name     string
	Platform string
	Type     string
	Notes    *string
}
