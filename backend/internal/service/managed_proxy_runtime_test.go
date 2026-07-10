package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestExecManagedProxyProcessRunnerKeepsProcessAfterStartContextCancel(t *testing.T) {
	if len(os.Args) > 0 && os.Args[len(os.Args)-1] == "managed-proxy-helper" {
		time.Sleep(5 * time.Second)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	process, err := (execManagedProxyProcessRunner{}).Start(ctx, os.Args[0], []string{
		"-test.run=TestExecManagedProxyProcessRunnerKeepsProcessAfterStartContextCancel",
		"--",
		"managed-proxy-helper",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	cancel()
	select {
	case err := <-process.Done():
		t.Fatalf("process exited after start context cancel: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := process.Stop(stopCtx); err != nil && stopCtx.Err() == nil {
		t.Fatalf("stop process: %v", err)
	}
}

func TestManagedProxyRuntimeStartEntryUsesAbsoluteConfigPath(t *testing.T) {
	runner := &capturingManagedProxyRunner{}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
	}, nil, runner)

	err := runtime.startEntry(context.Background(), ProxySubscription{
		ID:                 7,
		Name:               "test",
		SubscriptionURL:    "https://example.com/subscription.yaml",
		Status:             ProxySubscriptionStatusActive,
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		Revision:           1,
		Nodes: []ProxySubscriptionNode{{
			Name:         "HK-01",
			ProviderName: "node-hk01",
			Type:         "ss",
			Username:     "mpu_hk",
			Password:     "mpp_hk",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: HK-01\ntype: ss\nserver: 8.8.8.8\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	})
	if err != nil {
		t.Fatalf("start entry: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		runtime.stopEntry(stopCtx, 7)
	})

	if !filepath.IsAbs(runner.dir) {
		t.Fatalf("runner dir is not absolute: %q", runner.dir)
	}
	if !strings.Contains(runner.dir, runtime.nodeID) {
		t.Fatalf("runner dir %q must include node id %q", runner.dir, runtime.nodeID)
	}
	if len(runner.args) != 4 {
		t.Fatalf("unexpected runner args: %#v", runner.args)
	}
	configPath := runner.args[1]
	if !filepath.IsAbs(configPath) {
		t.Fatalf("config path is not absolute: %q", configPath)
	}
	if want := filepath.Join(runner.dir, managedProxyConfigName); configPath != want {
		t.Fatalf("config path mismatch: got=%q want=%q", configPath, want)
	}
	if runner.args[3] != runner.dir {
		t.Fatalf("mihomo -d dir mismatch: got=%q want=%q", runner.args[3], runner.dir)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("generated config missing: %v", err)
	}
}

func TestManagedProxyRuntimeReloadCreatesStartingPlaceholder(t *testing.T) {
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
	}, nil, &capturingManagedProxyRunner{})

	runtime.Reload(7)

	status := runtime.GetStatus(7)
	if status.Status != ManagedProxyRuntimeStatusStarting {
		t.Fatalf("status = %q, want %q", status.Status, ManagedProxyRuntimeStatusStarting)
	}
	if status.SubscriptionID != 7 {
		t.Fatalf("subscription id = %d, want 7", status.SubscriptionID)
	}
	if status.NodeID == "" {
		t.Fatal("expected runtime status to include node_id")
	}
}

func TestManagedProxyRuntimeDoesNotWriteLocalRuntimeErrorsToSharedLastError(t *testing.T) {
	repo := &managedProxyRuntimeRepoStub{
		active: []ProxySubscription{{
			ID:                 9,
			Name:               "empty",
			SubscriptionURL:    "https://example.com/subscription.yaml",
			Status:             ProxySubscriptionStatusActive,
			RefreshIntervalSec: 3600,
			Revision:           1,
		}},
	}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
	}, repo, &capturingManagedProxyRunner{})

	runtime.syncOnce(context.Background())

	if repo.setLastErrorCalls != 0 {
		t.Fatalf("SetLastError calls = %d, want 0", repo.setLastErrorCalls)
	}
	status := runtime.GetStatus(9)
	if status.Status != ManagedProxyRuntimeStatusError {
		t.Fatalf("status = %q, want %q", status.Status, ManagedProxyRuntimeStatusError)
	}
	if status.LastError == "" {
		t.Fatal("expected local runtime status to keep last_error")
	}
}

func TestManagedProxyRuntimeEnsureRunningDeduplicatesConcurrentStart(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, nil, runner)
	sub := managedProxyRuntimeTestSubscription(11, 1)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.ensureRunning(context.Background(), sub)
	}()

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	if err := runtime.ensureRunning(context.Background(), sub); err != nil {
		t.Fatalf("second ensureRunning returned error: %v", err)
	}
	if calls := runner.StartCalls(); calls != 1 {
		t.Fatalf("start calls while first start is in progress = %d, want 1", calls)
	}
	close(runner.release)
	if err := <-errCh; err != nil {
		t.Fatalf("first ensureRunning returned error: %v", err)
	}
	if calls := runner.StartCalls(); calls != 1 {
		t.Fatalf("start calls after release = %d, want 1", calls)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		runtime.stopEntry(stopCtx, sub.ID)
	})
}

func TestManagedProxyRuntimeEnsureRunningDeduplicatesConcurrentRevisionRestart(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, nil, runner)
	sub := managedProxyRuntimeTestSubscription(15, 2)
	oldProcess := newBlockingStopManagedProxyProcess()

	runtime.mu.Lock()
	runtime.entries[sub.ID] = &managedProxyRuntimeEntry{
		subscriptionID: sub.ID,
		status:         ManagedProxyRuntimeStatusRunning,
		process:        oldProcess,
		revision:       1,
		updatedAt:      time.Now(),
	}
	runtime.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.ensureRunning(context.Background(), sub)
	}()

	select {
	case <-oldProcess.stopStarted:
	case <-time.After(time.Second):
		t.Fatal("old process stop did not start")
	}
	if err := runtime.ensureRunning(context.Background(), sub); err != nil {
		t.Fatalf("second ensureRunning returned error: %v", err)
	}
	if calls := runner.StartCalls(); calls != 0 {
		t.Fatalf("start calls before old process stop completed = %d, want 0", calls)
	}

	close(oldProcess.stopRelease)
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start after old process stopped")
	}
	if calls := runner.StartCalls(); calls != 1 {
		t.Fatalf("start calls while restart is in progress = %d, want 1", calls)
	}
	close(runner.release)
	if err := <-errCh; err != nil {
		t.Fatalf("first ensureRunning returned error: %v", err)
	}
	if calls := runner.StartCalls(); calls != 1 {
		t.Fatalf("start calls after restart release = %d, want 1", calls)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		runtime.stopEntry(stopCtx, sub.ID)
	})
}

func TestManagedProxyRuntimeStartEntrySkipsAlreadyRunningSameRevision(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	close(runner.release)
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, nil, runner)
	sub := managedProxyRuntimeTestSubscription(12, 1)

	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatalf("first startEntry returned error: %v", err)
	}
	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatalf("second startEntry returned error: %v", err)
	}
	if calls := runner.StartCalls(); calls != 1 {
		t.Fatalf("start calls = %d, want 1", calls)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		runtime.stopEntry(stopCtx, sub.ID)
	})
}

func TestManagedProxyRuntimeStartIsIdempotent(t *testing.T) {
	sub := managedProxyRuntimeTestSubscription(18, 1)
	repo := &managedProxyRuntimeRepoStub{
		active:           []ProxySubscription{sub},
		listActiveNotify: make(chan struct{}, 4),
	}
	runner := newBlockingManagedProxyRunner()
	close(runner.release)
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		SyncIntervalSec:  3600,
		StartTimeoutSec:  5,
	}, repo, runner)

	runtime.Start()
	runtime.Start()
	select {
	case <-repo.listActiveNotify:
	case <-time.After(time.Second):
		t.Fatal("runtime did not perform initial sync")
	}
	select {
	case <-repo.listActiveNotify:
		t.Fatal("second Start triggered duplicate runLoop")
	case <-time.After(100 * time.Millisecond):
	}
	runtime.Stop()
}

func TestManagedProxyRuntimePendingReloadDuringStartTriggersFollowupSync(t *testing.T) {
	sub := managedProxyRuntimeTestSubscription(19, 1)
	repo := &managedProxyRuntimeRepoStub{
		active:           []ProxySubscription{sub},
		listActiveNotify: make(chan struct{}, 4),
	}
	runner := newBlockingManagedProxyRunner()
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, repo, runner)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.startEntry(context.Background(), sub)
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	runtime.Reload(sub.ID)
	close(runner.release)
	if err := <-errCh; err != nil {
		t.Fatalf("startEntry returned error: %v", err)
	}
	select {
	case <-repo.listActiveNotify:
	case <-time.After(time.Second):
		t.Fatal("pending reload did not trigger follow-up sync")
	}
	runtime.Stop()
}

func TestManagedProxyRuntimeStopWaitsForWatcher(t *testing.T) {
	runner := &deferredDoneManagedProxyRunner{}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, nil, runner)
	sub := managedProxyRuntimeTestSubscription(20, 1)

	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatalf("startEntry returned error: %v", err)
	}
	stopDone := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned before process watcher observed Done")
	case <-time.After(50 * time.Millisecond):
	}
	runner.process.closeDone()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for watcher")
	}
}

func TestManagedProxyRuntimeStopDuringStartPreventsOldStartFromBecomingRunning(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, nil, runner)
	sub := managedProxyRuntimeTestSubscription(13, 1)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.startEntry(context.Background(), sub)
	}()

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	runtime.stopEntry(context.Background(), sub.ID)
	close(runner.release)
	if err := <-errCh; err != nil {
		t.Fatalf("startEntry returned error: %v", err)
	}
	status := runtime.GetStatus(sub.ID)
	if status.Status == ManagedProxyRuntimeStatusRunning {
		t.Fatalf("status = %q, want non-running after stop during start", status.Status)
	}
}

func TestManagedProxyRuntimeStopWaitsForReloadTriggeredSync(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	sub := managedProxyRuntimeTestSubscription(14, 1)
	repo := &managedProxyRuntimeRepoStub{active: []ProxySubscription{sub}}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, repo, runner)

	runtime.Reload(sub.ID)
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	stopDone := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned before reload-triggered start completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(runner.release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for reload-triggered sync to finish")
	}
	status := runtime.GetStatus(sub.ID)
	if status.Status == ManagedProxyRuntimeStatusRunning {
		t.Fatalf("status = %q, want non-running after Stop", status.Status)
	}
}

func TestManagedProxyRuntimeReloadAfterStopDoesNotStart(t *testing.T) {
	runner := newBlockingManagedProxyRunner()
	sub := managedProxyRuntimeTestSubscription(16, 1)
	repo := &managedProxyRuntimeRepoStub{active: []ProxySubscription{sub}}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
		Enabled:          true,
		MihomoBinaryPath: "mihomo",
		WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
		BindHost:         "127.0.0.1",
		StartTimeoutSec:  5,
	}, repo, runner)

	runtime.Stop()
	runtime.Reload(sub.ID)
	runtime.syncOnce(context.Background())

	if calls := runner.StartCalls(); calls != 0 {
		t.Fatalf("start calls after Stop = %d, want 0", calls)
	}
}

func TestManagedProxyRuntimeRejectsNonLoopbackBindHost(t *testing.T) {
	for _, bindHost := range []string{"0.0.0.0", "::", "192.0.2.10"} {
		t.Run(bindHost, func(t *testing.T) {
			runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{
				Enabled:          true,
				MihomoBinaryPath: "mihomo",
				WorkDir:          filepath.Join(t.TempDir(), "managed-proxy"),
				BindHost:         bindHost,
				StartTimeoutSec:  5,
			}, nil, &capturingManagedProxyRunner{})

			err := runtime.startEntry(context.Background(), managedProxyRuntimeTestSubscription(17, 1))
			if err == nil {
				t.Fatal("expected non-loopback bind_host to be rejected")
			}
			status := runtime.GetStatus(17)
			if !strings.Contains(status.LastError, "loopback-only") {
				t.Fatalf("last error = %q, want loopback-only rejection", status.LastError)
			}
		})
	}
}

func TestParseProxySubscriptionNodes(t *testing.T) {
	nodes, err := ParseProxySubscriptionNodes([]byte(`
proxies:
  - name: HK-01
    type: ss
    server: 8.8.8.8
    port: 8388
    cipher: aes-128-gcm
    password: remote-secret
  - name: JP-01
    type: trojan
    server: 1.1.1.1
    port: 443
    password: remote-secret
`))
	if err != nil {
		t.Fatalf("parse nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("node count mismatch: got=%d want=2", len(nodes))
	}
	if nodes[0].Name != "HK-01" || nodes[0].Type != "ss" || nodes[0].Server != "8.8.8.8" || nodes[0].Port != 8388 {
		t.Fatalf("unexpected first node: %#v", nodes[0])
	}
	if !strings.Contains(nodes[0].RawConfig, "remote-secret") {
		t.Fatalf("raw config was not preserved: %q", nodes[0].RawConfig)
	}
}

func TestParseProxySubscriptionNodes_StableKeyIgnoresMutableConfig(t *testing.T) {
	first, err := ParseProxySubscriptionNodes([]byte(`
proxies:
  - name: HK-01
    type: ss
    server: 8.8.8.8
    port: 8388
    cipher: aes-128-gcm
    password: old-secret
`))
	if err != nil {
		t.Fatalf("parse first nodes: %v", err)
	}
	second, err := ParseProxySubscriptionNodes([]byte(`
proxies:
  - name: HK-01
    type: ss
    server: 8.8.8.8
    port: 8388
    cipher: aes-128-gcm
    password: new-secret
`))
	if err != nil {
		t.Fatalf("parse second nodes: %v", err)
	}
	if first[0].NodeKey != second[0].NodeKey {
		t.Fatalf("node key changed after mutable config update: got=%q want=%q", second[0].NodeKey, first[0].NodeKey)
	}
	if !strings.Contains(second[0].RawConfig, "new-secret") {
		t.Fatalf("raw config was not updated: %q", second[0].RawConfig)
	}
}

func TestBuildMihomoConfigYAML_NodeAuthRoutesToSpecificProxy(t *testing.T) {
	cfg, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 9,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		Nodes: []ProxySubscriptionNode{
			{
				Name:         "HK-01",
				ProviderName: "node-hk01",
				Type:         "ss",
				Username:     "mpu_hk",
				Password:     "mpp_hk",
				Status:       ProxySubscriptionNodeStatusActive,
				RawConfig:    "name: HK-01\ntype: ss\nserver: 8.8.8.8\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
			},
			{
				Name:         "JP-01",
				ProviderName: "node-jp01",
				Type:         "trojan",
				Username:     "mpu_jp",
				Password:     "mpp_jp",
				Status:       ProxySubscriptionNodeStatusActive,
				RawConfig:    "name: JP-01\ntype: trojan\nserver: 1.1.1.1\nport: 443\npassword: remote-secret\n",
			},
		},
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	text := string(cfg)
	for _, expected := range []string{
		"listeners:",
		"type: mixed",
		"username: mpu_hk",
		"password: mpp_hk",
		"name: node-hk01",
		"IN-USER,mpu_hk,node-hk01",
		"IN-USER,mpu_jp,node-jp01",
		"MATCH,REJECT",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated config missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "proxy-providers:") || strings.Contains(text, "subscription.yaml") {
		t.Fatalf("generated node config must not use remote proxy-providers:\n%s", text)
	}
}

func TestBuildMihomoConfigYAMLRequiresActiveNodes(t *testing.T) {
	_, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 9,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err == nil {
		t.Fatal("expected empty-node managed proxy config to fail fast")
	}
}

// TestBuildMihomoConfigYAMLRestoresSubscriptionDNSPolicy 覆盖验收标准 3/4：
// 订阅带 DNS policy 时，runtime 配置需包含 dns.nameserver-policy，并且不再用系统 DNS
// 为 policy 域名生成 hosts pin（避免钉死到错误 IP）。域名节点在此路径下不做系统 DNS 解析，
// 因此该用例完全离线可复现。
func TestBuildMihomoConfigYAMLRestoresSubscriptionDNSPolicy(t *testing.T) {
	cfg, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 21,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		RawDNSConfig:       "nameserver-policy:\n  \"+.entry.example.qpon\": tcp://dns.example:8080\n",
		Nodes: []ProxySubscriptionNode{{
			Name:         "HK-01",
			ProviderName: "node-hk01",
			Type:         "ss",
			Username:     "mpu_hk",
			Password:     "mpp_hk",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: HK-01\ntype: ss\nserver: t.hk01.entry.example.qpon\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err != nil {
		t.Fatalf("build config with dns policy: %v", err)
	}
	text := string(cfg)
	for _, expected := range []string{
		"dns:",
		"nameserver-policy",
		"entry.example.qpon",
		"tcp://dns.example:8080",
		"enable: true",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated config missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "hosts:") {
		t.Fatalf("generated config must not pin hosts when subscription supplies dns policy:\n%s", text)
	}
}

func TestBuildMihomoConfigYAMLRejectsNonPolicyDomainWhenPolicyExists(t *testing.T) {
	_, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 22,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		RawDNSConfig:       "nameserver-policy:\n  \"+.entry.example.invalid\": tcp://dns.example:8080\n",
		Nodes: []ProxySubscriptionNode{{
			Name:         "US-01",
			ProviderName: "node-us01",
			Type:         "ss",
			Username:     "mpu_us",
			Password:     "mpp_us",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: US-01\ntype: ss\nserver: other.example.invalid\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err == nil {
		t.Fatal("expected non-policy domain to keep DNS validation and be rejected")
	}
}

func TestBuildMihomoConfigYAMLRejectsRawConfigServerOutsidePolicy(t *testing.T) {
	_, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 23,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		RawDNSConfig:       "nameserver-policy:\n  \"+.entry.example.qpon\": tcp://dns.example:8080\n",
		Nodes: []ProxySubscriptionNode{{
			Name:         "US-01",
			ProviderName: "node-us01",
			Type:         "ss",
			Server:       "t.hk01.entry.example.qpon",
			Username:     "mpu_us",
			Password:     "mpp_us",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: US-01\ntype: ss\nserver: other.example.invalid\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err == nil {
		t.Fatal("expected RawConfig server outside policy to keep DNS validation and be rejected")
	}
}

// TestBuildManagedProxyDNSConfig 验证 DNS 段构建：强制 enable、缺省 nameserver 时补默认。
func TestBuildManagedProxyDNSConfig(t *testing.T) {
	if cfg, err := buildManagedProxyDNSConfig(""); err != nil || cfg != nil {
		t.Fatalf("empty raw dns config should yield nil map, got cfg=%v err=%v", cfg, err)
	}

	cfg, err := buildManagedProxyDNSConfig("listen: 0.0.0.0:53\nfake-ip-range: 198.18.0.1/16\nnameserver_policy:\n  \"+.unsafe.example\": tcp://unsafe.example:8080\nnameserver-policy:\n  \"+.entry.example.qpon\": tcp://dns.example:8080\n")
	if err != nil {
		t.Fatalf("build dns config: %v", err)
	}
	if enable, _ := cfg["enable"].(bool); !enable {
		t.Fatalf("expected enable=true, got: %#v", cfg["enable"])
	}
	if _, ok := cfg["nameserver"]; !ok {
		t.Fatalf("expected default nameserver to be injected when missing: %#v", cfg)
	}
	if _, ok := cfg["nameserver-policy"]; !ok {
		t.Fatalf("expected nameserver-policy to be preserved: %#v", cfg)
	}
	for _, dropped := range []string{"listen", "fake-ip-range", "nameserver_policy"} {
		if _, ok := cfg[dropped]; ok {
			t.Fatalf("expected dns field %q to be dropped, got: %#v", dropped, cfg)
		}
	}

	withNS, err := buildManagedProxyDNSConfig("enable: false\nnameserver:\n  - https://1.1.1.1/dns-query\n")
	if err != nil {
		t.Fatalf("build dns config with nameserver: %v", err)
	}
	if enable, _ := withNS["enable"].(bool); !enable {
		t.Fatalf("expected enable forced to true, got: %#v", withNS["enable"])
	}
	ns, ok := withNS["nameserver"].([]any)
	if !ok || len(ns) != 1 {
		t.Fatalf("existing nameserver should be preserved as-is, got: %#v", withNS["nameserver"])
	}
}

func TestBuildMihomoConfigYAMLBlocksStoredPrivateNode(t *testing.T) {
	_, err := buildMihomoConfigYAML(ProxySubscription{
		ID:                 9,
		Name:               "subscription",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		Nodes: []ProxySubscriptionNode{{
			Name:         "local-node",
			ProviderName: "node-local",
			Type:         "ss",
			Username:     "mpu_local",
			Password:     "mpp_local",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: local-node\ntype: ss\nserver: 127.0.0.1\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	}, managedProxyMihomoConfigOptions{
		BindHost:         "127.0.0.1",
		MixedPort:        39065,
		ControllerPort:   39066,
		ControllerSecret: "secret",
		HealthCheckURL:   "https://example.com/health",
	})
	if err == nil {
		t.Fatal("expected private stored node server to be blocked")
	}
}

type managedProxyRuntimeRepoStub struct {
	active            []ProxySubscription
	setLastErrorCalls int
	listActiveNotify  chan struct{}
}

func (r *managedProxyRuntimeRepoStub) CreateWithNodes(context.Context, *ProxySubscription, []ProxySubscriptionNode) (*ProxySubscription, []Proxy, error) {
	panic("unexpected CreateWithNodes")
}
func (r *managedProxyRuntimeRepoStub) List(context.Context) ([]ProxySubscription, error) {
	panic("unexpected List")
}
func (r *managedProxyRuntimeRepoStub) ListActive(context.Context) ([]ProxySubscription, error) {
	if r.listActiveNotify != nil {
		select {
		case r.listActiveNotify <- struct{}{}:
		default:
		}
	}
	return append([]ProxySubscription(nil), r.active...), nil
}
func (r *managedProxyRuntimeRepoStub) Get(context.Context, int64) (*ProxySubscription, error) {
	panic("unexpected Get")
}
func (r *managedProxyRuntimeRepoStub) GetByProxyID(context.Context, int64) (*ProxySubscription, error) {
	panic("unexpected GetByProxyID")
}
func (r *managedProxyRuntimeRepoStub) Update(context.Context, *ProxySubscription) error {
	panic("unexpected Update")
}
func (r *managedProxyRuntimeRepoStub) UpdateWithNodes(context.Context, *ProxySubscription, []ProxySubscriptionNode) error {
	panic("unexpected UpdateWithNodes")
}
func (r *managedProxyRuntimeRepoStub) DeleteWithProxy(context.Context, int64) error {
	panic("unexpected DeleteWithProxy")
}
func (r *managedProxyRuntimeRepoStub) IncrementRevision(context.Context, int64) (*ProxySubscription, error) {
	panic("unexpected IncrementRevision")
}
func (r *managedProxyRuntimeRepoStub) SyncNodes(context.Context, int64, string, []ProxySubscriptionNode) ([]Proxy, error) {
	panic("unexpected SyncNodes")
}
func (r *managedProxyRuntimeRepoStub) GetNodeByProxyID(context.Context, int64) (*ProxySubscriptionNode, error) {
	panic("unexpected GetNodeByProxyID")
}
func (r *managedProxyRuntimeRepoStub) SetNodeStatusByProxyID(context.Context, int64, string) error {
	panic("unexpected SetNodeStatusByProxyID")
}
func (r *managedProxyRuntimeRepoStub) ListProxyIDsBySubscriptionID(context.Context, int64) ([]int64, error) {
	panic("unexpected ListProxyIDsBySubscriptionID")
}
func (r *managedProxyRuntimeRepoStub) SetLastError(context.Context, int64, string) error {
	r.setLastErrorCalls++
	return nil
}

func managedProxyRuntimeTestSubscription(id, revision int64) ProxySubscription {
	return ProxySubscription{
		ID:                 id,
		Name:               "runtime-test",
		SubscriptionURL:    "https://example.com/subscription.yaml",
		Status:             ProxySubscriptionStatusActive,
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
		Revision:           revision,
		Nodes: []ProxySubscriptionNode{{
			Name:         "HK-01",
			ProviderName: "node-hk01",
			Type:         "ss",
			Username:     "mpu_hk",
			Password:     "mpp_hk",
			Status:       ProxySubscriptionNodeStatusActive,
			RawConfig:    "name: HK-01\ntype: ss\nserver: 8.8.8.8\nport: 8388\ncipher: aes-128-gcm\npassword: remote-secret\n",
		}},
	}
}

type capturingManagedProxyRunner struct {
	binary  string
	args    []string
	dir     string
	process *fakeManagedProxyProcess
}

func (r *capturingManagedProxyRunner) Start(ctx context.Context, binary string, args []string, dir string) (managedProxyProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.binary = binary
	r.args = append([]string(nil), args...)
	r.dir = dir
	r.process = &fakeManagedProxyProcess{done: make(chan error)}
	return r.process, nil
}

type blockingManagedProxyRunner struct {
	mu      sync.Mutex
	once    sync.Once
	started chan struct{}
	release chan struct{}
	calls   int
}

func newBlockingManagedProxyRunner() *blockingManagedProxyRunner {
	return &blockingManagedProxyRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingManagedProxyRunner) Start(ctx context.Context, _ string, _ []string, _ string) (managedProxyProcess, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() { close(r.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return &fakeManagedProxyProcess{done: make(chan error)}, nil
	}
}

func (r *blockingManagedProxyRunner) StartCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type deferredDoneManagedProxyRunner struct {
	process *deferredDoneManagedProxyProcess
}

func (r *deferredDoneManagedProxyRunner) Start(ctx context.Context, _ string, _ []string, _ string) (managedProxyProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.process = &deferredDoneManagedProxyProcess{done: make(chan error)}
	return r.process, nil
}

type deferredDoneManagedProxyProcess struct {
	done chan error
	once sync.Once
}

func (p *deferredDoneManagedProxyProcess) Pid() int {
	return 789
}

func (p *deferredDoneManagedProxyProcess) Done() <-chan error {
	return p.done
}

func (p *deferredDoneManagedProxyProcess) Stop(context.Context) error {
	return nil
}

func (p *deferredDoneManagedProxyProcess) closeDone() {
	p.once.Do(func() { close(p.done) })
}

type fakeManagedProxyProcess struct {
	done chan error
	once sync.Once
}

func (p *fakeManagedProxyProcess) Pid() int {
	return 123
}

func (p *fakeManagedProxyProcess) Done() <-chan error {
	return p.done
}

func (p *fakeManagedProxyProcess) Stop(context.Context) error {
	p.once.Do(func() {
		close(p.done)
	})
	return nil
}

type blockingStopManagedProxyProcess struct {
	done        chan error
	stopStarted chan struct{}
	stopRelease chan struct{}
	startOnce   sync.Once
	doneOnce    sync.Once
}

func newBlockingStopManagedProxyProcess() *blockingStopManagedProxyProcess {
	return &blockingStopManagedProxyProcess{
		done:        make(chan error),
		stopStarted: make(chan struct{}),
		stopRelease: make(chan struct{}),
	}
}

func (p *blockingStopManagedProxyProcess) Pid() int {
	return 456
}

func (p *blockingStopManagedProxyProcess) Done() <-chan error {
	return p.done
}

func (p *blockingStopManagedProxyProcess) Stop(ctx context.Context) error {
	p.startOnce.Do(func() { close(p.stopStarted) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stopRelease:
		p.doneOnce.Do(func() { close(p.done) })
		return nil
	}
}
