package node

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/core"
	log "github.com/sirupsen/logrus"
)

var startControllerForMachine = func(controller *Controller, coreInstance *core.V2Core) error {
	return controller.Start(coreInstance)
}

var closeControllerForMachine = func(controller *Controller) error {
	return controller.Close()
}

var replaceControllerForMachine = func(oldController *Controller, newController *Controller, coreInstance *core.V2Core) (bool, error) {
	return newController.StartReplacing(coreInstance, oldController)
}

type ReconcileResult struct {
	Added              int
	Removed            int
	Restarted          int
	Unchanged          int
	Skipped            int
	FullReloadRequired bool
	Failures           []NodeFailure
}

type machineSlot struct {
	Config     conf.NodeConfig
	Info       *panel.NodeInfo
	Controller *Controller
}

type machineCandidate struct {
	Config       conf.NodeConfig
	Info         *panel.NodeInfo
	ControlPlane ControlPlane
}

func (n *Node) Reconcile(ctx context.Context, desired []conf.NodeConfig, realtime conf.RealtimeConfig, coreInstance *core.V2Core, opts MachineOptions) (*ReconcileResult, error) {
	return n.reconcileWithFactory(ctx, desired, realtime, coreInstance, defaultControlPlaneFactory(), opts)
}

func (n *Node) reconcileWithFactory(ctx context.Context, desired []conf.NodeConfig, realtime conf.RealtimeConfig, coreInstance *core.V2Core, factory ControlPlaneFactory, opts MachineOptions) (*ReconcileResult, error) {
	if n == nil {
		return nil, fmt.Errorf("node manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if factory == nil {
		factory = defaultControlPlaneFactory()
	}

	result := &ReconcileResult{}
	current, err := n.currentMachineSlots()
	if err != nil {
		return nil, err
	}

	desiredKeys := make(map[string]struct{}, len(desired))
	candidates := make(map[string]machineCandidate, len(desired))
	orderedKeys := make([]string, 0, len(desired))
	for i := range desired {
		cfg := desired[i]
		key := machineNodeKey(cfg)
		if key == "" {
			if err := handleMachineFailure(result, cfg, fmt.Errorf("node config requires api host and node id"), opts); err != nil {
				return nil, err
			}
			continue
		}
		if _, exists := desiredKeys[key]; exists {
			if err := handleMachineFailure(result, cfg, fmt.Errorf("duplicate node config: %s", key), opts); err != nil {
				return nil, err
			}
			continue
		}
		desiredKeys[key] = struct{}{}
		orderedKeys = append(orderedKeys, key)

		controlPlane, err := factory.New(&desired[i])
		if err != nil {
			if err := handleMachineFailure(result, cfg, err, opts); err != nil {
				return nil, err
			}
			continue
		}
		info, err := controlPlane.GetNodeInfo(ctx)
		if err != nil {
			if err := handleMachineFailure(result, cfg, err, opts); err != nil {
				return nil, err
			}
			continue
		}
		if info == nil {
			if _, exists := current[key]; exists {
				result.Skipped++
				continue
			}
			if err := handleMachineFailure(result, cfg, fmt.Errorf("received empty node info"), opts); err != nil {
				return nil, err
			}
			continue
		}
		candidates[key] = machineCandidate{
			Config:       cfg,
			Info:         info,
			ControlPlane: controlPlane,
		}
	}

	if len(candidates) == 0 && len(result.Failures) > 0 && opts.ContinueOnError {
		log.WithField("failures", len(result.Failures)).Warn("Machine reconcile kept existing nodes because every desired node failed to refresh")
		n.failures = result.Failures
		return result, nil
	}

	if machineReconcileNeedsFullReload(current, desiredKeys, candidates) {
		result.FullReloadRequired = true
		return result, nil
	}

	for key, slot := range current {
		if _, desired := desiredKeys[key]; desired {
			continue
		}
		if err := closeControllerForMachine(slot.Controller); err != nil {
			return nil, fmt.Errorf("close removed node [%s-%d] error: %w", slot.Config.APIHost, slot.Config.NodeID, err)
		}
		result.Removed++
	}

	nextControllers := make([]*Controller, 0, len(desired))
	nextInfos := make([]*panel.NodeInfo, 0, len(desired))
	nextConfigs := make([]conf.NodeConfig, 0, len(desired))
	for _, key := range orderedKeys {
		candidate, hasCandidate := candidates[key]
		slot, hasCurrent := current[key]
		if !hasCandidate {
			if hasCurrent {
				nextControllers = append(nextControllers, slot.Controller)
				nextInfos = append(nextInfos, slot.Info)
				nextConfigs = append(nextConfigs, slot.Config)
				result.Unchanged++
			}
			continue
		}

		changed := !hasCurrent || !sameMachineNode(slot.Config, slot.Info, candidate.Config, candidate.Info)
		if hasCurrent && !changed {
			nextControllers = append(nextControllers, slot.Controller)
			nextInfos = append(nextInfos, slot.Info)
			nextConfigs = append(nextConfigs, slot.Config)
			result.Unchanged++
			continue
		}

		cfg := candidate.Config
		controller := NewControllerWithControlPlane(candidate.ControlPlane, &cfg, candidate.Info, realtime)
		controller.edgeTrafficBridge = n.edgeTrafficBridge
		controller.edgeSidecarBridge = n.edgeSidecarBridge
		var err error
		oldStillActive := false
		if hasCurrent {
			oldStillActive, err = replaceControllerForMachine(slot.Controller, controller, coreInstance)
		} else {
			err = startControllerForMachine(controller, coreInstance)
		}
		if err != nil {
			if !hasCurrent {
				_ = closeControllerForMachine(controller)
			} else if oldStillActive {
				nextControllers = append(nextControllers, slot.Controller)
				nextInfos = append(nextInfos, slot.Info)
				nextConfigs = append(nextConfigs, slot.Config)
			}
			if err := handleMachineFailure(result, candidate.Config, err, opts); err != nil {
				return nil, err
			}
			continue
		}

		nextControllers = append(nextControllers, controller)
		nextInfos = append(nextInfos, candidate.Info)
		nextConfigs = append(nextConfigs, candidate.Config)
		if hasCurrent {
			result.Restarted++
		} else {
			result.Added++
		}
	}

	n.controllers = nextControllers
	n.NodeInfos = nextInfos
	n.configs = nextConfigs
	n.failures = result.Failures
	n.reconcileAutoHY2PortForward()
	return result, nil
}

func (n *Node) currentMachineSlots() (map[string]machineSlot, error) {
	activeConfigs := n.activeConfigs(nil)
	if len(activeConfigs) != len(n.controllers) || len(n.NodeInfos) != len(n.controllers) {
		return nil, fmt.Errorf("node controller/config/info count mismatch: configs=%d infos=%d controllers=%d", len(activeConfigs), len(n.NodeInfos), len(n.controllers))
	}

	current := make(map[string]machineSlot, len(n.controllers))
	for i := range n.controllers {
		key := machineNodeKey(activeConfigs[i])
		if key == "" {
			return nil, fmt.Errorf("current node config requires api host and node id: %+v", activeConfigs[i])
		}
		current[key] = machineSlot{
			Config:     activeConfigs[i],
			Info:       n.NodeInfos[i],
			Controller: n.controllers[i],
		}
	}
	return current, nil
}

func handleMachineFailure(result *ReconcileResult, cfg conf.NodeConfig, err error, opts MachineOptions) error {
	if result != nil {
		result.Failures = append(result.Failures, NodeFailure{Config: cfg, Err: err})
		result.Skipped++
	}
	if opts.ContinueOnError {
		log.WithFields(log.Fields{
			"api_host": cfg.APIHost,
			"node_id":  cfg.NodeID,
			"err":      err,
		}).Warn("Machine reconcile skipped node")
		return nil
	}
	return err
}

func machineReconcileNeedsFullReload(current map[string]machineSlot, desiredKeys map[string]struct{}, candidates map[string]machineCandidate) bool {
	for key, slot := range current {
		if _, desired := desiredKeys[key]; !desired && nodeHasCustomRoutes(slot.Info) {
			return true
		}
	}
	for key, candidate := range candidates {
		slot, exists := current[key]
		if !exists {
			if nodeHasCustomRoutes(candidate.Info) {
				return true
			}
			continue
		}
		if !sameMachineNode(slot.Config, slot.Info, candidate.Config, candidate.Info) &&
			(nodeHasCustomRoutes(slot.Info) || nodeHasCustomRoutes(candidate.Info)) {
			return true
		}
	}
	return false
}

func sameMachineNode(oldConfig conf.NodeConfig, oldInfo *panel.NodeInfo, newConfig conf.NodeConfig, newInfo *panel.NodeInfo) bool {
	return reflect.DeepEqual(oldConfig, newConfig) && reflect.DeepEqual(oldInfo, newInfo)
}

func nodeHasCustomRoutes(info *panel.NodeInfo) bool {
	return info != nil && info.Common != nil && len(info.Common.Routes) > 0
}

func machineNodeKey(cfg conf.NodeConfig) string {
	apiHost := strings.TrimRight(strings.TrimSpace(cfg.APIHost), "/")
	if apiHost == "" || cfg.NodeID <= 0 {
		return ""
	}
	return apiHost + "#" + strconv.Itoa(cfg.NodeID)
}
