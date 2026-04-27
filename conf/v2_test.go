package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromPathConfigV2SingleNode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := `
panel:
  url: "https://panel.example.com"
  token: "test-token"
  node_id: 7
  timeout: 18
kernel:
  config_dir: "/var/lib/v2node"
  log_level: "warn"
log:
  level: "info"
runtime:
  gomemlimit: "256MiB"
  gogc: 120
  auto_hy2_port_forward: true
health_port: 65530
realtime:
  enabled: true
  ping_interval: 15
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := New()
	if err := cfg.LoadFromPath(path); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.HealthPort != 65530 {
		t.Fatalf("unexpected health port: %d", cfg.HealthPort)
	}
	if cfg.LogConfig.Level != "info" || cfg.LogConfig.CoreLevel != "warn" {
		t.Fatalf("unexpected log config: %+v", cfg.LogConfig)
	}
	if cfg.RuntimeConfig.GoMemLimit != "256MiB" || cfg.RuntimeConfig.GOGC != 120 || !cfg.RuntimeConfig.AutoHY2PortForward {
		t.Fatalf("unexpected runtime config: %+v", cfg.RuntimeConfig)
	}
	if len(cfg.NodeConfigs) != 1 {
		t.Fatalf("unexpected node count: %d", len(cfg.NodeConfigs))
	}
	node := cfg.NodeConfigs[0]
	if node.APIHost != "https://panel.example.com" || node.Key != "test-token" || node.NodeID != 7 || node.Timeout != 18 {
		t.Fatalf("unexpected node config: %+v", node)
	}
	if node.ConfigDir != "/var/lib/v2node" {
		t.Fatalf("unexpected config dir: %s", node.ConfigDir)
	}
}

func TestLoadFromPathConfigV2MultiNodeConfigDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
panel:
  url: "https://panel.example.com"
  token: "shared-token"
kernel:
  config_dir: "/var/lib/v2node"
nodes:
  - node_id: 1
  - node_id: 2
    config_dir: "/srv/v2node/custom-2"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := New()
	if err := cfg.LoadFromPath(path); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if len(cfg.NodeConfigs) != 2 {
		t.Fatalf("unexpected node count: %d", len(cfg.NodeConfigs))
	}
	if cfg.NodeConfigs[0].ConfigDir != "/var/lib/v2node/node-1" {
		t.Fatalf("unexpected first node config dir: %s", cfg.NodeConfigs[0].ConfigDir)
	}
	if cfg.NodeConfigs[1].ConfigDir != "/srv/v2node/custom-2" {
		t.Fatalf("unexpected second node config dir: %s", cfg.NodeConfigs[1].ConfigDir)
	}
}

func TestLoadFromPathConfigV2MachineMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
panel:
  url: "https://panel.example.com"
  token: "shared-token"
machine:
  enabled: true
nodes:
  - node_id: 1
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := New()
	if err := cfg.LoadFromPath(path); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if !cfg.MachineConfig.Enabled {
		t.Fatalf("expected machine mode enabled")
	}
	if !cfg.MachineConfig.ContinueOnError {
		t.Fatalf("expected machine mode to default continue_on_error to true")
	}
}

func TestLoadFromPathConfigV2MachineModeMultiplePanels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
machine:
  enabled: true
nodes:
  - url: "https://panel-a.example.com"
    token: "token-a"
    node_id: 1
  - url: "https://panel-b.example.com"
    token: "token-b"
    node_id: 8
    config_dir: "/srv/panel-b-node-8"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := New()
	if err := cfg.LoadFromPath(path); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if len(cfg.NodeConfigs) != 2 {
		t.Fatalf("unexpected node count: %d", len(cfg.NodeConfigs))
	}
	if cfg.NodeConfigs[0].APIHost != "https://panel-a.example.com" || cfg.NodeConfigs[0].Key != "token-a" {
		t.Fatalf("unexpected first node config: %+v", cfg.NodeConfigs[0])
	}
	if cfg.NodeConfigs[0].ConfigDir != "/etc/v2node/node-1" {
		t.Fatalf("unexpected first node config dir: %s", cfg.NodeConfigs[0].ConfigDir)
	}
	if cfg.NodeConfigs[1].APIHost != "https://panel-b.example.com" || cfg.NodeConfigs[1].ConfigDir != "/srv/panel-b-node-8" {
		t.Fatalf("unexpected second node config: %+v", cfg.NodeConfigs[1])
	}
}

func TestLoadFromPathConfigV2MachineProfiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
panel:
  timeout: 12
machine:
  profiles:
    - name: "site-a"
      url: "https://panel-a.example.com"
      token: "machine-token-a"
      machine_id: 11
    - name: "site-b"
      url: "https://panel-b.example.com"
      token: "machine-token-b"
      machine_id: 22
      timeout: 20
      config_dir: "/srv/site-b"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg := New()
	if err := cfg.LoadFromPath(path); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if !cfg.MachineConfig.Enabled || !cfg.MachineConfig.ContinueOnError {
		t.Fatalf("unexpected machine config: %+v", cfg.MachineConfig)
	}
	if len(cfg.NodeConfigs) != 0 {
		t.Fatalf("expected profile-only config to defer node resolution, got %+v", cfg.NodeConfigs)
	}
	if len(cfg.MachineConfig.Profiles) != 2 {
		t.Fatalf("unexpected machine profile count: %d", len(cfg.MachineConfig.Profiles))
	}
	first := cfg.MachineConfig.Profiles[0]
	if first.Name != "site-a" || first.APIHost != "https://panel-a.example.com" || first.Key != "machine-token-a" || first.MachineID != 11 || first.Timeout != 12 {
		t.Fatalf("unexpected first profile: %+v", first)
	}
	second := cfg.MachineConfig.Profiles[1]
	if second.Timeout != 20 || second.ConfigDir != "/srv/site-b" {
		t.Fatalf("unexpected second profile: %+v", second)
	}
}

func TestResolveConfigPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	yamlPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(yamlPath, []byte("panel:\n  url: x\n"), 0644); err != nil {
		t.Fatalf("write yaml failed: %v", err)
	}

	if got := ResolveConfigPath(jsonPath); got != yamlPath {
		t.Fatalf("unexpected resolved path: got %s want %s", got, yamlPath)
	}
}
