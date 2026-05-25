package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"gopkg.in/yaml.v3"
)

const (
	managedProxyProviderName = "subscription"
	managedProxyGroupName    = "AUTO"
	managedProxyListenerName = "managed-mixed"
	managedProxyConfigName   = "config.yaml"
)

var (
	defaultManagedProxyResolverMu sync.RWMutex
	defaultManagedProxyResolver   ManagedProxyResolver = managedProxyNoopResolver{}
)

type managedProxyProcessRunner interface {
	Start(ctx context.Context, binary string, args []string, dir string) (managedProxyProcess, error)
}

type managedProxyProcess interface {
	Pid() int
	Done() <-chan error
	Stop(ctx context.Context) error
}

type execManagedProxyProcessRunner struct{}

func (execManagedProxyProcessRunner) Start(ctx context.Context, binary string, args []string, dir string) (managedProxyProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execManagedProxyProcess{
		cmd:  cmd,
		done: make(chan error, 1),
	}
	go func() {
		p.done <- cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

type execManagedProxyProcess struct {
	cmd  *exec.Cmd
	done chan error
}

func (p *execManagedProxyProcess) Pid() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *execManagedProxyProcess) Done() <-chan error {
	if p == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return p.done
}

func (p *execManagedProxyProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	done := p.Done()
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		select {
		case <-done:
		default:
		}
		return ctx.Err()
	}
}

type ManagedProxyRuntime struct {
	cfg    config.ManagedProxyConfig
	repo   ProxySubscriptionRepository
	runner managedProxyProcessRunner

	mu      sync.RWMutex
	entries map[int64]*managedProxyRuntimeEntry

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type managedProxyRuntimeEntry struct {
	subscriptionID  int64
	status          string
	port            int
	revision        int64
	lastError       string
	startedAt       *time.Time
	updatedAt       time.Time
	process         managedProxyProcess
	instanceDir     string
	configPath      string
	reloadRequested bool
}

func NewManagedProxyRuntime(cfg *config.Config, repo ProxySubscriptionRepository) *ManagedProxyRuntime {
	runtimeCfg := config.ManagedProxyConfig{}
	if cfg != nil {
		runtimeCfg = cfg.ManagedProxy
	}
	return newManagedProxyRuntimeWithRunner(runtimeCfg, repo, execManagedProxyProcessRunner{})
}

func newManagedProxyRuntimeWithRunner(cfg config.ManagedProxyConfig, repo ProxySubscriptionRepository, runner managedProxyProcessRunner) *ManagedProxyRuntime {
	cfg = normalizeManagedProxyRuntimeConfig(cfg)
	if runner == nil {
		runner = execManagedProxyProcessRunner{}
	}
	return &ManagedProxyRuntime{
		cfg:     cfg,
		repo:    repo,
		runner:  runner,
		entries: make(map[int64]*managedProxyRuntimeEntry),
		stopCh:  make(chan struct{}),
	}
}

func ProvideManagedProxyRuntime(cfg *config.Config, repo ProxySubscriptionRepository) *ManagedProxyRuntime {
	rt := NewManagedProxyRuntime(cfg, repo)
	SetDefaultManagedProxyResolver(rt)
	rt.Start()
	return rt
}

func SetDefaultManagedProxyResolver(resolver ManagedProxyResolver) {
	defaultManagedProxyResolverMu.Lock()
	defer defaultManagedProxyResolverMu.Unlock()
	if resolver == nil {
		defaultManagedProxyResolver = managedProxyNoopResolver{}
		return
	}
	defaultManagedProxyResolver = resolver
}

func GetDefaultManagedProxyResolver() ManagedProxyResolver {
	defaultManagedProxyResolverMu.RLock()
	defer defaultManagedProxyResolverMu.RUnlock()
	return defaultManagedProxyResolver
}

func normalizeManagedProxyRuntimeConfig(cfg config.ManagedProxyConfig) config.ManagedProxyConfig {
	cfg.BindHost = strings.TrimSpace(cfg.BindHost)
	if cfg.BindHost == "" {
		cfg.BindHost = "127.0.0.1"
	}
	cfg.WorkDir = strings.TrimSpace(cfg.WorkDir)
	if cfg.WorkDir == "" {
		cfg.WorkDir = "./data/managed-proxy"
	}
	if cfg.SyncIntervalSec <= 0 {
		cfg.SyncIntervalSec = 30
	}
	if cfg.StartTimeoutSec <= 0 {
		cfg.StartTimeoutSec = 10
	}
	if cfg.MaxInstances <= 0 {
		cfg.MaxInstances = 32
	}
	if cfg.DefaultRefreshIntervalSec <= 0 {
		cfg.DefaultRefreshIntervalSec = 3600
	}
	cfg.HealthCheckURL = strings.TrimSpace(cfg.HealthCheckURL)
	if cfg.HealthCheckURL == "" {
		cfg.HealthCheckURL = "https://www.gstatic.com/generate_204"
	}
	return cfg
}

func (r *ManagedProxyRuntime) Start() {
	if r == nil {
		return
	}
	SetDefaultManagedProxyResolver(r)
	if !r.cfg.Enabled {
		return
	}
	if strings.TrimSpace(r.cfg.MihomoBinaryPath) == "" {
		r.recordGlobalError("mihomo binary path is required")
		return
	}
	if r.repo == nil {
		r.recordGlobalError("proxy subscription repository unavailable")
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.runLoop()
	}()
}

func (r *ManagedProxyRuntime) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
	r.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.StartTimeoutSec)*time.Second)
	defer cancel()
	r.stopAll(ctx)
	SetDefaultManagedProxyResolver(nil)
}

func (r *ManagedProxyRuntime) ResolveProxyURL(_ context.Context, subscriptionID int64) (string, error) {
	if r == nil || !r.cfg.Enabled {
		return "", ErrManagedProxyDisabled
	}
	r.mu.RLock()
	entry := r.entries[subscriptionID]
	if entry == nil {
		r.mu.RUnlock()
		return "", ErrManagedProxyUnavailable
	}
	status := entry.status
	port := entry.port
	lastErr := entry.lastError
	r.mu.RUnlock()
	if status != ManagedProxyRuntimeStatusRunning || port <= 0 {
		if lastErr != "" {
			return "", fmt.Errorf("%w: %s", ErrManagedProxyUnavailable, lastErr)
		}
		return "", ErrManagedProxyUnavailable
	}
	return fmt.Sprintf("socks5h://%s", net.JoinHostPort(r.cfg.BindHost, strconv.Itoa(port))), nil
}

func (r *ManagedProxyRuntime) GetStatus(subscriptionID int64) ManagedProxyRuntimeStatus {
	now := time.Now()
	if r == nil {
		return ManagedProxyRuntimeStatus{
			Enabled:        false,
			Status:         ManagedProxyRuntimeStatusDisabled,
			SubscriptionID: subscriptionID,
			UpdatedAt:      now,
		}
	}
	if !r.cfg.Enabled {
		return ManagedProxyRuntimeStatus{
			Enabled:        false,
			Status:         ManagedProxyRuntimeStatusDisabled,
			SubscriptionID: subscriptionID,
			UpdatedAt:      now,
		}
	}
	r.mu.RLock()
	entry := r.entries[subscriptionID]
	if entry == nil {
		r.mu.RUnlock()
		return ManagedProxyRuntimeStatus{
			Enabled:        true,
			Status:         ManagedProxyRuntimeStatusStopped,
			SubscriptionID: subscriptionID,
			UpdatedAt:      now,
		}
	}
	status := r.entryStatusLocked(entry)
	r.mu.RUnlock()
	return status
}

func (r *ManagedProxyRuntime) Reload(subscriptionID int64) {
	if r == nil || subscriptionID <= 0 {
		return
	}
	now := time.Now()
	r.mu.Lock()
	if entry := r.entries[subscriptionID]; entry != nil {
		entry.reloadRequested = true
		entry.updatedAt = now
		if entry.process == nil && entry.status != ManagedProxyRuntimeStatusRunning {
			entry.status = ManagedProxyRuntimeStatusStarting
			entry.lastError = ""
		}
	} else if r.cfg.Enabled {
		r.entries[subscriptionID] = &managedProxyRuntimeEntry{
			subscriptionID:  subscriptionID,
			status:          ManagedProxyRuntimeStatusStarting,
			updatedAt:       now,
			reloadRequested: true,
		}
	}
	r.mu.Unlock()
	if r.cfg.Enabled {
		go r.syncOnce(context.Background())
	}
}

func (r *ManagedProxyRuntime) runLoop() {
	r.syncOnce(context.Background())
	ticker := time.NewTicker(time.Duration(r.cfg.SyncIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.syncOnce(context.Background())
		case <-r.stopCh:
			return
		}
	}
}

func (r *ManagedProxyRuntime) syncOnce(ctx context.Context) {
	if r == nil || !r.cfg.Enabled || r.repo == nil {
		return
	}
	subs, err := r.repo.ListActive(ctx)
	if err != nil {
		logger.LegacyPrintf("service.managed_proxy", "list active managed proxy subscriptions failed: %v", err)
		return
	}
	if len(subs) > r.cfg.MaxInstances {
		sort.Slice(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })
		for _, sub := range subs[r.cfg.MaxInstances:] {
			_ = r.repo.SetLastError(ctx, sub.ID, fmt.Sprintf("managed proxy max_instances=%d exceeded on this node", r.cfg.MaxInstances))
		}
		subs = subs[:r.cfg.MaxInstances]
	}

	active := make(map[int64]ProxySubscription, len(subs))
	for _, sub := range subs {
		active[sub.ID] = sub
		if err := r.ensureRunning(ctx, sub); err != nil {
			msg := sanitizeManagedProxyError(err)
			logger.LegacyPrintf("service.managed_proxy", "ensure managed proxy runtime failed: subscription_id=%d err=%s", sub.ID, msg)
			_ = r.repo.SetLastError(ctx, sub.ID, msg)
		}
	}
	r.stopInactive(ctx, active)
}

func (r *ManagedProxyRuntime) ensureRunning(ctx context.Context, sub ProxySubscription) error {
	if sub.ID <= 0 {
		return ErrProxySubscriptionNotFound
	}
	r.mu.RLock()
	entry := r.entries[sub.ID]
	needsStart := entry == nil || entry.process == nil || entry.status != ManagedProxyRuntimeStatusRunning || entry.revision != sub.Revision || entry.reloadRequested
	r.mu.RUnlock()
	if !needsStart {
		return nil
	}
	if entry != nil && entry.process != nil {
		r.stopEntry(ctx, sub.ID)
	}
	return r.startEntry(ctx, sub)
}

func (r *ManagedProxyRuntime) startEntry(parent context.Context, sub ProxySubscription) error {
	if strings.TrimSpace(r.cfg.MihomoBinaryPath) == "" {
		r.setEntryError(sub.ID, "mihomo binary path is required")
		return ErrManagedProxyUnavailable
	}
	port, controllerPort, err := allocateManagedProxyPortPair(r.cfg.BindHost)
	if err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	instanceDir := filepath.Join(r.cfg.WorkDir, fmt.Sprintf("subscription-%d", sub.ID))
	instanceDir, err = filepath.Abs(instanceDir)
	if err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	if err := os.MkdirAll(instanceDir, 0o700); err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	if err := os.MkdirAll(filepath.Join(instanceDir, "providers"), 0o700); err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	secret, err := managedProxyRandomHexString(24)
	if err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	cfgBytes, err := buildMihomoConfigYAML(sub, managedProxyMihomoConfigOptions{
		BindHost:         r.cfg.BindHost,
		MixedPort:        port,
		ControllerPort:   controllerPort,
		ControllerSecret: secret,
		HealthCheckURL:   managedProxyFirstNonEmptyString(sub.TestURL, r.cfg.HealthCheckURL),
	})
	if err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	configPath := filepath.Join(instanceDir, managedProxyConfigName)
	if err := os.WriteFile(configPath, cfgBytes, 0o600); err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}

	now := time.Now()
	r.mu.Lock()
	r.entries[sub.ID] = &managedProxyRuntimeEntry{
		subscriptionID: sub.ID,
		status:         ManagedProxyRuntimeStatusStarting,
		port:           port,
		revision:       sub.Revision,
		updatedAt:      now,
		instanceDir:    instanceDir,
		configPath:     configPath,
	}
	r.mu.Unlock()

	startCtx, cancel := context.WithTimeout(parent, time.Duration(r.cfg.StartTimeoutSec)*time.Second)
	process, err := r.runner.Start(startCtx, strings.TrimSpace(r.cfg.MihomoBinaryPath), []string{"-f", configPath, "-d", instanceDir}, instanceDir)
	cancel()
	if err != nil {
		r.setEntryError(sub.ID, err.Error())
		return err
	}
	startedAt := time.Now()
	r.mu.Lock()
	entry := r.entries[sub.ID]
	if entry == nil {
		entry = &managedProxyRuntimeEntry{subscriptionID: sub.ID}
		r.entries[sub.ID] = entry
	}
	entry.status = ManagedProxyRuntimeStatusRunning
	entry.port = port
	entry.revision = sub.Revision
	entry.lastError = ""
	entry.startedAt = &startedAt
	entry.updatedAt = startedAt
	entry.process = process
	entry.instanceDir = instanceDir
	entry.configPath = configPath
	entry.reloadRequested = false
	r.mu.Unlock()

	if r.repo != nil {
		_ = r.repo.SetLastError(parent, sub.ID, "")
	}
	go r.watchProcess(sub.ID, process)
	return nil
}

func (r *ManagedProxyRuntime) watchProcess(subscriptionID int64, process managedProxyProcess) {
	err, ok := <-process.Done()
	if !ok {
		err = nil
	}
	r.mu.Lock()
	entry := r.entries[subscriptionID]
	if entry != nil && entry.process == process {
		entry.process = nil
		entry.port = 0
		entry.updatedAt = time.Now()
		if err != nil {
			entry.status = ManagedProxyRuntimeStatusError
			entry.lastError = sanitizeManagedProxyError(err)
		} else {
			entry.status = ManagedProxyRuntimeStatusStopped
			entry.lastError = ""
		}
	}
	r.mu.Unlock()
	if err != nil && r.repo != nil {
		_ = r.repo.SetLastError(context.Background(), subscriptionID, sanitizeManagedProxyError(err))
	}
}

func (r *ManagedProxyRuntime) stopInactive(ctx context.Context, active map[int64]ProxySubscription) {
	r.mu.RLock()
	ids := make([]int64, 0, len(r.entries))
	for id := range r.entries {
		if _, ok := active[id]; !ok {
			ids = append(ids, id)
		}
	}
	r.mu.RUnlock()
	for _, id := range ids {
		r.stopEntry(ctx, id)
	}
}

func (r *ManagedProxyRuntime) stopAll(ctx context.Context) {
	r.mu.RLock()
	ids := make([]int64, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	for _, id := range ids {
		r.stopEntry(ctx, id)
	}
}

func (r *ManagedProxyRuntime) stopEntry(ctx context.Context, subscriptionID int64) {
	r.mu.Lock()
	entry := r.entries[subscriptionID]
	if entry == nil {
		r.mu.Unlock()
		return
	}
	process := entry.process
	entry.process = nil
	entry.port = 0
	entry.status = ManagedProxyRuntimeStatusStopped
	entry.updatedAt = time.Now()
	r.mu.Unlock()
	if process != nil {
		stopCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.StartTimeoutSec)*time.Second)
		_ = process.Stop(stopCtx)
		cancel()
	}
}

func (r *ManagedProxyRuntime) setEntryError(subscriptionID int64, message string) {
	r.mu.Lock()
	entry := r.entries[subscriptionID]
	if entry == nil {
		entry = &managedProxyRuntimeEntry{subscriptionID: subscriptionID}
		r.entries[subscriptionID] = entry
	}
	entry.status = ManagedProxyRuntimeStatusError
	entry.port = 0
	entry.lastError = sanitizeManagedProxyError(errors.New(message))
	entry.updatedAt = time.Now()
	r.mu.Unlock()
}

func (r *ManagedProxyRuntime) entryStatusLocked(entry *managedProxyRuntimeEntry) ManagedProxyRuntimeStatus {
	status := ManagedProxyRuntimeStatus{
		Enabled:        r.cfg.Enabled,
		Status:         ManagedProxyRuntimeStatusStopped,
		SubscriptionID: entry.subscriptionID,
		UpdatedAt:      entry.updatedAt,
	}
	if entry == nil {
		return status
	}
	status.Status = entry.status
	status.Port = entry.port
	status.Revision = entry.revision
	status.LastError = entry.lastError
	status.StartedAt = entry.startedAt
	if entry.port > 0 && entry.status == ManagedProxyRuntimeStatusRunning {
		status.LocalURL = fmt.Sprintf("socks5h://%s", net.JoinHostPort(r.cfg.BindHost, strconv.Itoa(entry.port)))
	}
	return status
}

func (r *ManagedProxyRuntime) recordGlobalError(message string) {
	logger.LegacyPrintf("service.managed_proxy", "managed proxy runtime disabled: %s", message)
}

type managedProxyMihomoConfigOptions struct {
	BindHost         string
	MixedPort        int
	ControllerPort   int
	ControllerSecret string
	HealthCheckURL   string
}

func buildMihomoConfigYAML(sub ProxySubscription, opts managedProxyMihomoConfigOptions) ([]byte, error) {
	if err := validateHTTPURL(sub.SubscriptionURL, "subscription_url"); err != nil {
		return nil, err
	}
	if opts.MixedPort <= 0 || opts.MixedPort > 65535 {
		return nil, fmt.Errorf("invalid mihomo mixed port")
	}
	if sub.RefreshIntervalSec <= 0 {
		sub.RefreshIntervalSec = 3600
	}
	bindHost := strings.TrimSpace(opts.BindHost)
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	healthURL := strings.TrimSpace(opts.HealthCheckURL)
	if healthURL == "" {
		healthURL = "https://www.gstatic.com/generate_204"
	}
	if err := validateHTTPURL(healthURL, "test_url"); err != nil {
		return nil, err
	}
	controllerPort := opts.ControllerPort
	if controllerPort <= 0 {
		var err error
		controllerPort, err = allocateManagedProxyPort(bindHost)
		if err != nil {
			return nil, err
		}
	}
	secret := opts.ControllerSecret
	if strings.TrimSpace(secret) == "" {
		var err error
		secret, err = managedProxyRandomHexString(24)
		if err != nil {
			return nil, err
		}
	}
	if len(sub.Nodes) > 0 {
		return buildMihomoNodeConfigYAML(sub, managedProxyMihomoConfigOptions{
			BindHost:         bindHost,
			MixedPort:        opts.MixedPort,
			ControllerPort:   controllerPort,
			ControllerSecret: secret,
			HealthCheckURL:   healthURL,
		})
	}
	cfg := map[string]any{
		"allow-lan":                 false,
		"bind-address":              bindHost,
		"external-controller":       net.JoinHostPort(bindHost, strconv.Itoa(controllerPort)),
		"global-client-fingerprint": "chrome",
		"log-level":                 "warning",
		"mixed-port":                opts.MixedPort,
		"mode":                      "rule",
		"secret":                    secret,
		"profile": map[string]any{
			"store-selected": false,
			"store-fake-ip":  false,
		},
		"proxy-providers": map[string]any{
			managedProxyProviderName: map[string]any{
				"type":     "http",
				"url":      sub.SubscriptionURL,
				"path":     "./providers/subscription.yaml",
				"interval": sub.RefreshIntervalSec,
				"health-check": map[string]any{
					"enable":   true,
					"url":      healthURL,
					"interval": sub.RefreshIntervalSec,
				},
			},
		},
		"proxy-groups": []map[string]any{
			{
				"name":     managedProxyGroupName,
				"type":     "url-test",
				"use":      []string{managedProxyProviderName},
				"url":      healthURL,
				"interval": sub.RefreshIntervalSec,
			},
		},
		"rules": []string{
			"MATCH," + managedProxyGroupName,
		},
	}
	return yaml.Marshal(cfg)
}

func buildMihomoNodeConfigYAML(sub ProxySubscription, opts managedProxyMihomoConfigOptions) ([]byte, error) {
	users := make([]map[string]any, 0, len(sub.Nodes))
	proxies := make([]map[string]any, 0, len(sub.Nodes))
	rules := make([]string, 0, len(sub.Nodes)+1)
	for _, node := range sub.Nodes {
		if node.Status != "" && node.Status != ProxySubscriptionNodeStatusActive {
			continue
		}
		if strings.TrimSpace(node.Username) == "" || strings.TrimSpace(node.Password) == "" {
			return nil, fmt.Errorf("managed proxy node %s is missing local auth", node.Name)
		}
		proxyConfig, err := managedProxyNodeRawConfig(node)
		if err != nil {
			return nil, err
		}
		users = append(users, map[string]any{
			"username": node.Username,
			"password": node.Password,
		})
		proxies = append(proxies, proxyConfig)
		rules = append(rules, fmt.Sprintf("IN-USER,%s,%s", node.Username, node.ProviderName))
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("managed proxy subscription %d has no active nodes", sub.ID)
	}
	rules = append(rules, "MATCH,REJECT")
	cfg := map[string]any{
		"allow-lan":                 false,
		"bind-address":              opts.BindHost,
		"external-controller":       net.JoinHostPort(opts.BindHost, strconv.Itoa(opts.ControllerPort)),
		"global-client-fingerprint": "chrome",
		"log-level":                 "warning",
		"mode":                      "rule",
		"secret":                    opts.ControllerSecret,
		"profile": map[string]any{
			"store-selected": false,
			"store-fake-ip":  false,
		},
		"listeners": []map[string]any{
			{
				"name":   managedProxyListenerName,
				"type":   "mixed",
				"listen": opts.BindHost,
				"port":   opts.MixedPort,
				"users":  users,
			},
		},
		"proxies": proxies,
		"rules":   rules,
	}
	return yaml.Marshal(cfg)
}

func managedProxyNodeRawConfig(node ProxySubscriptionNode) (map[string]any, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(node.RawConfig), &raw); err != nil {
		return nil, fmt.Errorf("parse managed proxy node %s: %w", node.Name, err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("managed proxy node %s has empty config", node.Name)
	}
	raw["name"] = node.ProviderName
	return raw, nil
}

func allocateManagedProxyPort(bindHost string) (int, error) {
	host := strings.TrimSpace(bindHost)
	if host == "" {
		host = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("failed to allocate local port")
	}
	return addr.Port, nil
}

func allocateManagedProxyPortPair(bindHost string) (int, int, error) {
	host := strings.TrimSpace(bindHost)
	if host == "" {
		host = "127.0.0.1"
	}
	first, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = first.Close() }()
	second, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = second.Close() }()
	firstAddr, ok := first.Addr().(*net.TCPAddr)
	if !ok || firstAddr.Port <= 0 {
		return 0, 0, fmt.Errorf("failed to allocate local port")
	}
	secondAddr, ok := second.Addr().(*net.TCPAddr)
	if !ok || secondAddr.Port <= 0 {
		return 0, 0, fmt.Errorf("failed to allocate controller port")
	}
	return firstAddr.Port, secondAddr.Port, nil
}

func managedProxyRandomHexString(size int) (string, error) {
	if size <= 0 {
		size = 24
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func managedProxyFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sanitizeManagedProxyError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	for _, prefix := range []string{"http://", "https://"} {
		for {
			idx := strings.Index(message, prefix)
			if idx < 0 {
				break
			}
			end := idx + len(prefix)
			for end < len(message) {
				ch := message[end]
				if ch <= ' ' || ch == '"' || ch == '\'' || ch == ')' || ch == ']' || ch == '}' {
					break
				}
				end++
			}
			raw := message[idx:end]
			redacted := raw
			if u, parseErr := url.Parse(raw); parseErr == nil && u.Host != "" {
				u.RawQuery = ""
				u.Fragment = ""
				redacted = u.Redacted()
			}
			message = message[:idx] + redacted + message[end:]
		}
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}
