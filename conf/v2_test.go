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
	if cfg.RuntimeConfig.GoMemLimit != "256MiB" || cfg.RuntimeConfig.GOGC != 120 {
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
