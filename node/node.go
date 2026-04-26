package node

import (
	"context"
	"fmt"
	"strings"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/core"
	log "github.com/sirupsen/logrus"
)

type MachineOptions struct {
	ContinueOnError bool
}

type NodeFailure struct {
	Config conf.NodeConfig
	Err    error
}

type Node struct {
	controllers []*Controller
	NodeInfos   []*panel.NodeInfo
	configs     []conf.NodeConfig
	failures    []NodeFailure
}

func New(nodes []conf.NodeConfig, realtime conf.RealtimeConfig) (*Node, error) {
	return newWithFactory(nodes, realtime, defaultControlPlaneFactory(), MachineOptions{})
}

func NewMachine(nodes []conf.NodeConfig, realtime conf.RealtimeConfig, opts MachineOptions) (*Node, error) {
	return newWithFactory(nodes, realtime, defaultControlPlaneFactory(), opts)
}

func newWithFactory(nodes []conf.NodeConfig, realtime conf.RealtimeConfig, factory ControlPlaneFactory, opts MachineOptions) (*Node, error) {
	n := &Node{
		controllers: make([]*Controller, 0, len(nodes)),
		NodeInfos:   make([]*panel.NodeInfo, 0, len(nodes)),
		configs:     make([]conf.NodeConfig, 0, len(nodes)),
	}
	for i := range nodes {
		nodeConfig := &nodes[i]
		controlPlane, err := factory.New(nodeConfig)
		if err != nil {
			if opts.ContinueOnError {
				n.recordFailure(*nodeConfig, err)
				continue
			}
			return nil, fmt.Errorf("create control plane [%s-%d] error: %w", nodeConfig.APIHost, nodeConfig.NodeID, err)
		}
		info, err := controlPlane.GetNodeInfo(context.Background())
		if err != nil {
			if opts.ContinueOnError {
				n.recordFailure(*nodeConfig, err)
				continue
			}
			return nil, fmt.Errorf("get node info [%s-%d] error: %w", nodeConfig.APIHost, nodeConfig.NodeID, err)
		}
		if info == nil {
			err := fmt.Errorf("received empty node info")
			if opts.ContinueOnError {
				n.recordFailure(*nodeConfig, err)
				continue
			}
			return nil, fmt.Errorf("get node info [%s-%d] error: %w", nodeConfig.APIHost, nodeConfig.NodeID, err)
		}
		n.controllers = append(n.controllers, NewControllerWithControlPlane(controlPlane, nodeConfig, info, realtime))
		n.NodeInfos = append(n.NodeInfos, info)
		n.configs = append(n.configs, *nodeConfig)
	}
	if len(nodes) > 0 && len(n.controllers) == 0 {
		return nil, fmt.Errorf("no available nodes after initialization: %s", n.failureSummary())
	}
	return n, nil
}

func (n *Node) Start(nodes []conf.NodeConfig, core *core.V2Core) error {
	activeConfigs := n.activeConfigs(nodes)
	if len(activeConfigs) != len(n.controllers) {
		return fmt.Errorf("node controller/config count mismatch: configs=%d controllers=%d", len(activeConfigs), len(n.controllers))
	}
	for i, node := range activeConfigs {
		err := n.controllers[i].Start(core)
		if err != nil {
			return fmt.Errorf("start node controller [%s-%d] error: %s",
				node.APIHost,
				node.NodeID,
				err)
		}
	}
	return nil
}

func (n *Node) Close() error {
	var err error
	for _, c := range n.controllers {
		if err = c.Close(); err != nil {
			log.Errorf("close controller failed: %v", err)
			return err
		}
	}
	n.controllers = nil
	return nil
}

func (n *Node) ActiveConfigs() []conf.NodeConfig {
	if n == nil {
		return nil
	}
	out := make([]conf.NodeConfig, len(n.configs))
	copy(out, n.configs)
	return out
}

func (n *Node) Failures() []NodeFailure {
	if n == nil {
		return nil
	}
	out := make([]NodeFailure, len(n.failures))
	copy(out, n.failures)
	return out
}

func (n *Node) activeConfigs(fallback []conf.NodeConfig) []conf.NodeConfig {
	if n == nil {
		return nil
	}
	if len(n.configs) > 0 || len(n.controllers) == 0 {
		return n.configs
	}
	return fallback
}

func (n *Node) recordFailure(cfg conf.NodeConfig, err error) {
	n.failures = append(n.failures, NodeFailure{Config: cfg, Err: err})
	log.WithFields(log.Fields{
		"api_host": cfg.APIHost,
		"node_id":  cfg.NodeID,
		"err":      err,
	}).Warn("Machine mode skipped node during initialization")
}

func (n *Node) failureSummary() string {
	if n == nil || len(n.failures) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(n.failures))
	for _, failure := range n.failures {
		if failure.Err == nil {
			parts = append(parts, fmt.Sprintf("%s-%d", failure.Config.APIHost, failure.Config.NodeID))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s-%d: %s", failure.Config.APIHost, failure.Config.NodeID, failure.Err))
	}
	return strings.Join(parts, "; ")
}
