package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/core"
	"github.com/keli-123456/kelinode/node"
)

func TestReloadReplacesNodesAndPreservesReloadChannel(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yml")
	configBody := `
panel:
  url: "https://panel.example.com"
  token: "reload-token"
  node_id: 12
  timeout: 15
realtime:
  enabled: true
  ping_interval: 15
health_port: 65530
`
	if err := os.WriteFile(configPath, []byte(configBody), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	oldNodes := &node.Node{}
	oldCore := core.New(conf.New())
	oldReloadCh := make(chan struct{}, 1)
	oldCore.ReloadCh = oldReloadCh

	newNodes := &node.Node{}
	newCore := core.New(conf.New())
	health := newHealthState(configPath)
	runtimeState := &runtimeTuningState{}

	var callOrder []string
	restore := overrideReloadFactories(
		func(nodes []conf.NodeConfig, realtime conf.RealtimeConfig) (*node.Node, error) {
			callOrder = append(callOrder, "new-node")
			if len(nodes) != 1 || nodes[0].APIHost != "https://panel.example.com" || nodes[0].NodeID != 12 {
				t.Fatalf("unexpected node config: %+v", nodes)
			}
			if !realtime.Enabled || realtime.PingInterval != 15 {
				t.Fatalf("unexpected realtime config: %+v", realtime)
			}
			return newNodes, nil
		},
		func(cfg *conf.Conf) *core.V2Core {
			callOrder = append(callOrder, "new-core")
			if len(cfg.NodeConfigs) != 1 || cfg.NodeConfigs[0].APIHost != "https://panel.example.com" {
				t.Fatalf("unexpected loaded config: %+v", cfg.NodeConfigs)
			}
			return newCore
		},
		func(coreInstance *core.V2Core, nodesInstance *node.Node) error {
			callOrder = append(callOrder, "start-core")
			if coreInstance != newCore || nodesInstance != newNodes {
				t.Fatalf("unexpected start core inputs")
			}
			return nil
		},
		func(nodesInstance *node.Node, nodes []conf.NodeConfig, coreInstance *core.V2Core) error {
			callOrder = append(callOrder, "start-node")
			if nodesInstance != newNodes || coreInstance != newCore {
				t.Fatalf("unexpected start node inputs")
			}
			if len(nodes) != 1 || nodes[0].NodeID != 12 {
				t.Fatalf("unexpected node list: %+v", nodes)
			}
			return nil
		},
		func(nodesInstance *node.Node) error {
			callOrder = append(callOrder, "close-node")
			if nodesInstance != oldNodes {
				t.Fatalf("unexpected old nodes pointer")
			}
			return nil
		},
		func(coreInstance *core.V2Core) error {
			callOrder = append(callOrder, "close-core")
			if coreInstance != oldCore {
				t.Fatalf("unexpected old core pointer")
			}
			return nil
		},
	)
	defer restore()

	nodesPtr := oldNodes
	corePtr := oldCore
	if err := reload(configPath, &nodesPtr, &corePtr, health, runtimeState); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if nodesPtr != newNodes {
		t.Fatalf("expected nodes pointer to be replaced")
	}
	if corePtr != newCore {
		t.Fatalf("expected core pointer to be replaced")
	}
	if newCore.ReloadCh != oldReloadCh {
		t.Fatalf("expected reload channel to be preserved")
	}

	wantOrder := []string{"close-node", "close-core", "new-node", "new-core", "start-core", "start-node"}
	if !reflect.DeepEqual(callOrder, wantOrder) {
		t.Fatalf("unexpected call order: got %v want %v", callOrder, wantOrder)
	}

	snapshot := health.snapshot("")
	if snapshot.NodeCount != 1 {
		t.Fatalf("unexpected health node count: %d", snapshot.NodeCount)
	}
	if !snapshot.RealtimeEnabled {
		t.Fatalf("expected realtime to stay enabled in health snapshot")
	}
	if snapshot.HealthPort != 65530 {
		t.Fatalf("unexpected health port: %d", snapshot.HealthPort)
	}
}

func overrideReloadFactories(
	newNode func([]conf.NodeConfig, conf.RealtimeConfig) (*node.Node, error),
	newCore func(*conf.Conf) *core.V2Core,
	startCore func(*core.V2Core, *node.Node) error,
	startNode func(*node.Node, []conf.NodeConfig, *core.V2Core) error,
	closeNode func(*node.Node) error,
	closeCore func(*core.V2Core) error,
) func() {
	oldNewNode := newNodeForReload
	oldNewCore := newCoreForReload
	oldStartCore := startCoreForReload
	oldStartNode := startNodeForReload
	oldCloseNode := closeNodeForReload
	oldCloseCore := closeCoreForReload

	newNodeForReload = newNode
	newCoreForReload = newCore
	startCoreForReload = startCore
	startNodeForReload = startNode
	closeNodeForReload = closeNode
	closeCoreForReload = closeCore

	return func() {
		newNodeForReload = oldNewNode
		newCoreForReload = oldNewCore
		startCoreForReload = oldStartCore
		startNodeForReload = oldStartNode
		closeNodeForReload = oldCloseNode
		closeCoreForReload = oldCloseCore
	}
}
