package node

import (
	"context"
	"errors"
	"reflect"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	vcore "github.com/keli-123456/kelinode/core"
)

func TestNewMachineContinuesAfterNodeInfoError(t *testing.T) {
	t.Parallel()

	nodes := []conf.NodeConfig{
		{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a"},
		{APIHost: "https://panel-b.example.com", NodeID: 2, Key: "b"},
	}
	info := &panel.NodeInfo{Id: 2, Type: "vless", Tag: "node-2"}

	got, err := newWithFactory(nodes, conf.RealtimeConfig{}, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {err: errors.New("panel unavailable")},
			2: {info: info},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("newWithFactory returned error: %v", err)
	}

	if !reflect.DeepEqual(got.NodeInfos, []*panel.NodeInfo{info}) {
		t.Fatalf("unexpected node infos: %+v", got.NodeInfos)
	}
	active := got.ActiveConfigs()
	if len(active) != 1 || active[0].NodeID != 2 || active[0].APIHost != "https://panel-b.example.com" {
		t.Fatalf("unexpected active configs: %+v", active)
	}
	failures := got.Failures()
	if len(failures) != 1 || failures[0].Config.NodeID != 1 {
		t.Fatalf("unexpected failures: %+v", failures)
	}
}

func TestNewKeepsFailFastBehavior(t *testing.T) {
	t.Parallel()

	nodes := []conf.NodeConfig{
		{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a"},
		{APIHost: "https://panel-b.example.com", NodeID: 2, Key: "b"},
	}

	_, err := newWithFactory(nodes, conf.RealtimeConfig{}, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {err: errors.New("panel unavailable")},
			2: {info: &panel.NodeInfo{Id: 2, Type: "vless", Tag: "node-2"}},
		},
	}, MachineOptions{})
	if err == nil {
		t.Fatalf("expected fail-fast initialization error")
	}
}

func TestMachineStartSkipsFailedControllersWhenContinueOnError(t *testing.T) {
	oldStart := startControllerForMachine
	oldClose := closeControllerForMachine
	defer func() {
		startControllerForMachine = oldStart
		closeControllerForMachine = oldClose
	}()

	startControllerForMachine = func(controller *Controller, _ *vcore.V2Core) error {
		if controller.conf != nil && controller.conf.NodeID == 1 {
			return errors.New("user_delta request failed: 403 Forbidden")
		}
		return nil
	}
	closeControllerForMachine = func(*Controller) error { return nil }

	cfg1 := conf.NodeConfig{APIHost: "https://blocked.example.com", NodeID: 1, Key: "a", MachineID: 10}
	cfg2 := conf.NodeConfig{APIHost: "https://healthy.example.com", NodeID: 2, Key: "b", MachineID: 10}
	manager := &Node{
		controllers:     []*Controller{{conf: &cfg1}, {conf: &cfg2}},
		NodeInfos:       []*panel.NodeInfo{testNodeInfo(1, "node-1"), testNodeInfo(2, "node-2")},
		configs:         []conf.NodeConfig{cfg1, cfg2},
		continueOnError: true,
	}

	if err := manager.Start([]conf.NodeConfig{cfg1, cfg2}, &vcore.V2Core{}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	active := manager.ActiveConfigs()
	if len(active) != 1 || active[0].NodeID != 2 {
		t.Fatalf("unexpected active configs: %+v", active)
	}
	failures := manager.Failures()
	if len(failures) != 1 || failures[0].Config.NodeID != 1 {
		t.Fatalf("unexpected failures: %+v", failures)
	}
}

func TestMachineReconcileAddsAndRemovesNodes(t *testing.T) {
	oldStart := startControllerForMachine
	oldClose := closeControllerForMachine
	defer func() {
		startControllerForMachine = oldStart
		closeControllerForMachine = oldClose
	}()

	var started []int
	var closed int
	startControllerForMachine = func(controller *Controller, _ *vcore.V2Core) error {
		started = append(started, controller.conf.NodeID)
		return nil
	}
	closeControllerForMachine = func(*Controller) error {
		closed++
		return nil
	}

	cfg1 := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a"}
	cfg2 := conf.NodeConfig{APIHost: "https://panel-b.example.com", NodeID: 2, Key: "b"}
	cfg3 := conf.NodeConfig{APIHost: "https://panel-c.example.com", NodeID: 3, Key: "c"}
	info1 := testNodeInfo(1, "node-1")
	info2 := testNodeInfo(2, "node-2")
	info3 := testNodeInfo(3, "node-3")
	manager := &Node{
		controllers: []*Controller{{conf: &cfg1}, {conf: &cfg2}},
		NodeInfos:   []*panel.NodeInfo{info1, info2},
		configs:     []conf.NodeConfig{cfg1, cfg2},
	}

	result, err := manager.reconcileWithFactory(context.Background(), []conf.NodeConfig{cfg2, cfg3}, conf.RealtimeConfig{}, nil, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			2: {info: info2},
			3: {info: info3},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("reconcileWithFactory returned error: %v", err)
	}

	if result.Added != 1 || result.Removed != 1 || result.Unchanged != 1 || result.Restarted != 0 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if closed != 1 {
		t.Fatalf("unexpected closed count: %d", closed)
	}
	if !reflect.DeepEqual(started, []int{3}) {
		t.Fatalf("unexpected started nodes: %+v", started)
	}
	active := manager.ActiveConfigs()
	if len(active) != 2 || active[0].NodeID != 2 || active[1].NodeID != 3 {
		t.Fatalf("unexpected active configs: %+v", active)
	}
}

func TestMachineReconcileReplacesChangedNodeWithPreparedRestart(t *testing.T) {
	oldStart := startControllerForMachine
	oldClose := closeControllerForMachine
	oldReplace := replaceControllerForMachine
	defer func() {
		startControllerForMachine = oldStart
		closeControllerForMachine = oldClose
		replaceControllerForMachine = oldReplace
	}()

	startControllerForMachine = func(*Controller, *vcore.V2Core) error {
		t.Fatalf("changed node should use replacement hook")
		return nil
	}
	closeControllerForMachine = func(*Controller) error {
		t.Fatalf("changed node should not be closed before replacement hook")
		return nil
	}
	replaced := false
	replaceControllerForMachine = func(oldController *Controller, newController *Controller, _ *vcore.V2Core) (bool, error) {
		replaced = true
		if oldController == nil || oldController.conf == nil || oldController.conf.Timeout != 1 {
			t.Fatalf("unexpected old controller: %+v", oldController)
		}
		if newController == nil || newController.conf == nil || newController.conf.Timeout != 2 {
			t.Fatalf("unexpected new controller: %+v", newController)
		}
		return false, nil
	}

	oldCfg := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a", Timeout: 1}
	newCfg := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a", Timeout: 2}
	oldInfo := testNodeInfo(1, "node-1")
	newInfo := testNodeInfo(1, "node-1")
	newInfo.Common.ServerPort = 10088
	manager := &Node{
		controllers: []*Controller{{conf: &oldCfg}},
		NodeInfos:   []*panel.NodeInfo{oldInfo},
		configs:     []conf.NodeConfig{oldCfg},
	}

	result, err := manager.reconcileWithFactory(context.Background(), []conf.NodeConfig{newCfg}, conf.RealtimeConfig{}, nil, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {info: newInfo},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("reconcileWithFactory returned error: %v", err)
	}
	if !replaced {
		t.Fatalf("expected replacement hook")
	}
	if result.Restarted != 1 || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	active := manager.ActiveConfigs()
	if len(active) != 1 || active[0].Timeout != 2 {
		t.Fatalf("unexpected active config: %+v", active)
	}
}

func TestMachineReconcileKeepsOldControllerWhenReplacementPreparationFails(t *testing.T) {
	oldStart := startControllerForMachine
	oldClose := closeControllerForMachine
	oldReplace := replaceControllerForMachine
	defer func() {
		startControllerForMachine = oldStart
		closeControllerForMachine = oldClose
		replaceControllerForMachine = oldReplace
	}()

	startControllerForMachine = func(*Controller, *vcore.V2Core) error {
		t.Fatalf("changed node should use replacement hook")
		return nil
	}
	closeControllerForMachine = func(*Controller) error {
		t.Fatalf("old controller should stay active when preparation fails")
		return nil
	}
	replaceControllerForMachine = func(*Controller, *Controller, *vcore.V2Core) (bool, error) {
		return true, errors.New("prepare failed")
	}

	oldCfg := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a", Timeout: 1}
	newCfg := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a", Timeout: 2}
	oldController := &Controller{conf: &oldCfg}
	oldInfo := testNodeInfo(1, "node-1")
	newInfo := testNodeInfo(1, "node-1")
	newInfo.Common.ServerPort = 10088
	manager := &Node{
		controllers: []*Controller{oldController},
		NodeInfos:   []*panel.NodeInfo{oldInfo},
		configs:     []conf.NodeConfig{oldCfg},
	}

	result, err := manager.reconcileWithFactory(context.Background(), []conf.NodeConfig{newCfg}, conf.RealtimeConfig{}, nil, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {info: newInfo},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("reconcileWithFactory returned error: %v", err)
	}
	if result.Skipped != 1 || len(result.Failures) != 1 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if len(manager.controllers) != 1 || manager.controllers[0] != oldController {
		t.Fatalf("old controller was not kept: %+v", manager.controllers)
	}
	active := manager.ActiveConfigs()
	if len(active) != 1 || active[0].Timeout != 1 {
		t.Fatalf("unexpected active config: %+v", active)
	}
}

func TestMachineReconcileRequiresFullReloadForRouteChanges(t *testing.T) {
	cfg1 := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a"}
	cfg2 := conf.NodeConfig{APIHost: "https://panel-b.example.com", NodeID: 2, Key: "b"}
	info1 := testNodeInfo(1, "node-1")
	info2 := testNodeInfo(2, "node-2")
	info2.Common.Routes = []panel.Route{{Id: 1, Action: "block", Match: []string{"example.com"}}}
	manager := &Node{
		controllers: []*Controller{{conf: &cfg1}},
		NodeInfos:   []*panel.NodeInfo{info1},
		configs:     []conf.NodeConfig{cfg1},
	}

	result, err := manager.reconcileWithFactory(context.Background(), []conf.NodeConfig{cfg1, cfg2}, conf.RealtimeConfig{}, nil, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {info: info1},
			2: {info: info2},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("reconcileWithFactory returned error: %v", err)
	}
	if !result.FullReloadRequired {
		t.Fatalf("expected full reload requirement, got %+v", result)
	}
	if len(manager.ActiveConfigs()) != 1 || manager.ActiveConfigs()[0].NodeID != 1 {
		t.Fatalf("manager should not be mutated before full reload: %+v", manager.ActiveConfigs())
	}
}

func TestMachineReconcileKeepsExistingWhenRefreshFails(t *testing.T) {
	cfg1 := conf.NodeConfig{APIHost: "https://panel-a.example.com", NodeID: 1, Key: "a"}
	info1 := testNodeInfo(1, "node-1")
	manager := &Node{
		controllers: []*Controller{{conf: &cfg1}},
		NodeInfos:   []*panel.NodeInfo{info1},
		configs:     []conf.NodeConfig{cfg1},
	}

	result, err := manager.reconcileWithFactory(context.Background(), []conf.NodeConfig{cfg1}, conf.RealtimeConfig{}, nil, fakeControlPlaneFactory{
		results: map[int]fakeControlPlaneResult{
			1: {err: errors.New("temporary panel error")},
		},
	}, MachineOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("reconcileWithFactory returned error: %v", err)
	}
	if result.Skipped != 1 || len(result.Failures) != 1 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if len(manager.ActiveConfigs()) != 1 || manager.ActiveConfigs()[0].NodeID != 1 {
		t.Fatalf("expected existing node to stay active: %+v", manager.ActiveConfigs())
	}
}

func testNodeInfo(id int, tag string) *panel.NodeInfo {
	return &panel.NodeInfo{
		Id:   id,
		Type: "vless",
		Tag:  tag,
		Common: &panel.CommonNode{
			ServerPort: 10000 + id,
		},
	}
}

type fakeControlPlaneFactory struct {
	results map[int]fakeControlPlaneResult
}

func (f fakeControlPlaneFactory) New(cfg *conf.NodeConfig) (ControlPlane, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	result, ok := f.results[cfg.NodeID]
	if !ok {
		return nil, errors.New("missing fake control plane")
	}
	if result.factoryErr != nil {
		return nil, result.factoryErr
	}
	return &fakeControlPlane{result: result}, nil
}

type fakeControlPlaneResult struct {
	info       *panel.NodeInfo
	err        error
	factoryErr error
}

type fakeControlPlane struct {
	result fakeControlPlaneResult
}

func (f *fakeControlPlane) GetNodeInfo(context.Context) (*panel.NodeInfo, error) {
	return f.result.info, f.result.err
}

func (f *fakeControlPlane) GetUserList(context.Context) ([]panel.UserInfo, error) {
	return nil, nil
}

func (f *fakeControlPlane) GetUserDelta(context.Context, int64) (*panel.UserDeltaBody, error) {
	return nil, panel.ErrUserDeltaNotSupported
}

func (f *fakeControlPlane) GetUserAlive(context.Context) (map[int]int, error) {
	return nil, nil
}

func (f *fakeControlPlane) CachedAliveMap() map[int]int {
	return nil
}

func (f *fakeControlPlane) CachedAliveSnapshot() *panel.AliveMap {
	return nil
}

func (f *fakeControlPlane) ReportUserTraffic(context.Context, []panel.UserTraffic) error {
	return nil
}

func (f *fakeControlPlane) ReportNodeOnlineUsers(context.Context, *map[int][]string) error {
	return nil
}
