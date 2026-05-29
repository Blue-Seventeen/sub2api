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
