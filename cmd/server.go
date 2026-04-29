package cmd

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/keli-123456/kelinode/agent/subproxy"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/core"
	"github.com/keli-123456/kelinode/limiter"
	"github.com/keli-123456/kelinode/node"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	config                   string
	watch                    bool
	machineReconcileInterval = 5 * time.Minute
	newNodeForReload         = func(nodes []conf.NodeConfig, realtime conf.RealtimeConfig) (*node.Node, error) {
		return node.New(nodes, realtime)
	}
	newMachineNodeForReload  = func(nodes []conf.NodeConfig, realtime conf.RealtimeConfig, machine conf.MachineConfig) (*node.Node, error) {
		return node.NewMachine(nodes, realtime, node.MachineOptions{
			ContinueOnError: machine.ContinueOnError,
		})
	}
	prepareNodeConfigsForReload = func(ctx context.Context, cfg *conf.Conf) error {
		return node.ResolveMachineNodeConfigs(ctx, cfg)
	}
	reconcileMachineForReload = func(nodesInstance *node.Node, cfg *conf.Conf, coreInstance *core.V2Core) (*node.ReconcileResult, error) {
		nodesInstance.SetAutoHY2PortForward(cfg.RuntimeConfig.AutoHY2PortForward)
		return nodesInstance.Reconcile(context.Background(), cfg.NodeConfigs, cfg.RealtimeConfig, coreInstance, node.MachineOptions{
			ContinueOnError: cfg.MachineConfig.ContinueOnError,
		})
	}
	newCoreForReload   = func(cfg *conf.Conf) *core.V2Core { return core.New(cfg) }
	startCoreForReload = func(coreInstance *core.V2Core, nodesInstance *node.Node) error {
		return coreInstance.Start(nodesInstance.NodeInfos)
	}
	startNodeForReload = func(nodesInstance *node.Node, nodes []conf.NodeConfig, coreInstance *core.V2Core) error {
		return nodesInstance.Start(nodes, coreInstance)
	}
	closeNodeForReload = func(nodesInstance *node.Node) error {
		if nodesInstance == nil {
			return nil
		}
		return nodesInstance.Close()
	}
	closeCoreForReload = func(coreInstance *core.V2Core) error {
		if coreInstance == nil {
			return nil
		}
		return coreInstance.Close()
	}
	subscriptionProxyManager        = subproxy.NewManager()
	applySubscriptionProxyForReload = func(cfg conf.SubscriptionProxyConfig) error {
		return subscriptionProxyManager.Apply(cfg)
	}
	closeSubscriptionProxyForReload = func() error {
		return subscriptionProxyManager.Close()
	}
	machineStatusReporter       = newMachineStatusReporterState()
	applyMachineStatusForReload = func(machine conf.MachineConfig, requestReload func(), nodeFailures func() []node.NodeFailure) {
		machineStatusReporter.Apply(machine, subscriptionProxyManager.Status, requestReload, nodeFailures)
	}
	closeMachineStatusForReload = func() { machineStatusReporter.Close() }
	machineRealtimeReporter       = newMachineRealtimeState()
	applyMachineRealtimeForReload = func(machine conf.MachineConfig, realtime conf.RealtimeConfig, requestReload func()) {
		machineRealtimeReporter.Apply(machine, realtime, requestReload)
	}
	closeMachineRealtimeForReload = func() { machineRealtimeReporter.Close() }
)

var serverCommand = cobra.Command{
	Use:   "server",
	Short: "Run v2node server",
	Run:   serverHandle,
	Args:  cobra.NoArgs,
}

func init() {
	serverCommand.PersistentFlags().
		StringVarP(&config, "config", "c",
			"/etc/v2node/config.json", "config file path")
	serverCommand.PersistentFlags().
		BoolVarP(&watch, "watch", "w",
			true, "watch file path change")
	command.AddCommand(&serverCommand)
}

func serverHandle(_ *cobra.Command, _ []string) {
	showVersion()
	configPath := conf.ResolveConfigPath(config)
	c := conf.New()
	err := c.LoadFromPath(configPath)
	health := newHealthState(configPath)
	runtimeState := &runtimeTuningState{}
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
		PadLevelText:    false,
	})
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		return
	}
	switch c.LogConfig.Level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn", "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	}
	if c.LogConfig.Output != "" {
		f, err := os.OpenFile(c.LogConfig.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.WithField("err", err).Error("Open log file failed, using stdout instead")
		}
		log.SetOutput(f)
	}
	reloadCh := make(chan struct{}, 1)
	requestReload := func() { queueReload(reloadCh) }
	var nodes *node.Node
	nodeFailures := func() []node.NodeFailure {
		if nodes == nil {
			return nil
		}
		return nodes.Failures()
	}
	if err := prepareNodeConfigsForReload(context.Background(), c); err != nil {
		log.WithField("err", err).Error("Resolve machine profiles failed")
		return
	}
	applySubscriptionProxy(c.AgentConfig.SubscriptionProxy)
	applyMachineStatusForReload(c.MachineConfig, requestReload, nodeFailures)
	applyMachineRealtimeForReload(c.MachineConfig, c.RealtimeConfig, requestReload)
	defer func() {
		closeMachineRealtimeForReload()
		closeMachineStatusForReload()
		if err := closeSubscriptionProxyForReload(); err != nil {
			log.WithField("err", err).Warn("Close subscription proxy failed")
		}
	}()
	appliedRuntime := applyRuntimeSettings(c.RuntimeConfig, runtimeState)
	health.UpdateConfig(c, appliedRuntime)
	// Enable pprof if configured
	if c.PprofPort != 0 {
		go func() {
			log.Infof("Starting pprof server on :%d", c.PprofPort)
			if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", c.PprofPort), nil); err != nil {
				log.WithField("err", err).Error("pprof server failed")
			}
		}()
	}
	startHealthServer(c.HealthPort, health)
	//init limiter
	limiter.Init()
	//get node info
	nodes, err = newNodeForConfig(c)
	if err != nil {
		log.WithField("err", err).Error("Get node info failed")
		return
	}
	logMachineNodeFailures(nodes)
	if len(nodes.NodeInfos) == 0 {
		log.Info("No nodes configured; running agent services only")
	} else {
		log.Info("Got nodes info from server")
	}
	//core
	v2core := core.New(c)
	v2core.ReloadCh = reloadCh
	err = v2core.Start(nodes.NodeInfos)
	if err != nil {
		log.WithField("err", err).Error("Start core failed")
		return
	}
	defer func() {
		if v2core != nil {
			_ = v2core.Close()
		}
	}()
	//node
	err = nodes.Start(c.NodeConfigs, v2core)
	if err != nil {
		log.WithField("err", err).Error("Run nodes failed")
		return
	}
	defer func() {
		if nodes != nil {
			_ = nodes.Close()
		}
	}()
	health.MarkReady(true)
	if len(nodes.NodeInfos) == 0 {
		log.Info("Agent services started")
	} else {
		log.Info("Nodes started")
	}
	if watch {
		// On file change, just signal reload; do not run reload concurrently here
		err = c.Watch(configPath, func() {
			queueReload(reloadCh)
		})
		if err != nil {
			log.WithField("err", err).Error("start watch failed")
			return
		}
	}
	// clear memory
	runtime.GC()

	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, syscall.SIGINT, syscall.SIGTERM)
	machineFailureRetryTicker := time.NewTicker(time.Minute)
	defer machineFailureRetryTicker.Stop()
	machineReconcileTicker := time.NewTicker(machineReconcileInterval)
	defer machineReconcileTicker.Stop()

	for {
		select {
		case <-osSignals:
			log.Info("收到退出信号，正在关闭程序...")
			return
		case <-machineFailureRetryTicker.C:
			if nodes != nil && len(nodes.Failures()) > 0 {
				log.WithField("failures", len(nodes.Failures())).Info("Retrying failed machine nodes")
				queueReload(reloadCh)
			}
		case <-machineReconcileTicker.C:
			queueMachineReconcileIfEnabled(configPath, reloadCh)
		case <-reloadCh:
			log.Info("收到重启信号，正在重新加载配置...")
			health.MarkReady(false)
			if err := reload(configPath, &nodes, &v2core, health, runtimeState); err != nil {
				log.WithField("err", err).Panic("重启失败")
			}
			health.MarkReady(true)
			log.Info("重启成功")
		}
	}
}

func queueMachineReconcileIfEnabled(configPath string, reloadCh chan struct{}) {
	cfg := conf.New()
	if err := cfg.LoadFromPath(configPath); err != nil {
		log.WithField("err", err).Warn("Load config for periodic machine reconcile failed")
		return
	}
	if !cfg.MachineConfig.Enabled {
		return
	}
	log.Debug("Queueing periodic machine reconcile")
	queueReload(reloadCh)
}

func reload(config string, nodes **node.Node, v2core **core.V2Core, health *healthState, runtimeState *runtimeTuningState) error {
	// Preserve old reload channel so new core continues to receive signals
	var oldReloadCh chan struct{}
	if *v2core != nil {
		oldReloadCh = (*v2core).ReloadCh
	}

	newConf := conf.New()
	if err := newConf.LoadFromPath(config); err != nil {
		return err
	}

	switch newConf.LogConfig.Level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn", "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	}
	if newConf.LogConfig.Output != "" {
		f, err := os.OpenFile(newConf.LogConfig.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.WithField("err", err).Error("Open log file failed, using stdout instead")
		} else {
			// 关闭旧的日志文件（如果是文件）
			if oldWriter, ok := log.StandardLogger().Out.(*os.File); ok && oldWriter != os.Stdout && oldWriter != os.Stderr {
				oldWriter.Close()
			}
			log.SetOutput(f)
		}
	}
	if err := prepareNodeConfigsForReload(context.Background(), newConf); err != nil {
		return err
	}
	applySubscriptionProxy(newConf.AgentConfig.SubscriptionProxy)
	applyMachineStatusForReload(newConf.MachineConfig, func() { queueReload(oldReloadCh) }, func() []node.NodeFailure {
		if nodes == nil || *nodes == nil {
			return nil
		}
		return (*nodes).Failures()
	})
	applyMachineRealtimeForReload(newConf.MachineConfig, newConf.RealtimeConfig, func() { queueReload(oldReloadCh) })
	appliedRuntime := applyRuntimeSettings(newConf.RuntimeConfig, runtimeState)

	if newConf.MachineConfig.Enabled && *nodes != nil && *v2core != nil {
		result, err := reconcileMachineForReload(*nodes, newConf, *v2core)
		if err != nil {
			return err
		}
		if result != nil && !result.FullReloadRequired {
			log.WithFields(log.Fields{
				"added":     result.Added,
				"removed":   result.Removed,
				"restarted": result.Restarted,
				"unchanged": result.Unchanged,
				"skipped":   result.Skipped,
			}).Info("Machine mode reconcile completed")
			if health != nil {
				health.UpdateConfig(newConf, appliedRuntime)
			}
			runtime.GC()
			return nil
		}
		log.Info("Machine mode reconcile requires full reload")
	}

	if err := closeNodeForReload(*nodes); err != nil {
		return err
	}

	if err := closeCoreForReload(*v2core); err != nil {
		return err
	}

	newNodes, err := newNodeForConfig(newConf)
	if err != nil {
		return err
	}
	logMachineNodeFailures(newNodes)

	newCore := newCoreForReload(newConf)
	// Reattach reload channel
	newCore.ReloadCh = oldReloadCh
	if err := startCoreForReload(newCore, newNodes); err != nil {
		return err
	}

	if err := startNodeForReload(newNodes, newConf.NodeConfigs, newCore); err != nil {
		return err
	}

	*nodes = newNodes
	*v2core = newCore
	if health != nil {
		health.UpdateConfig(newConf, appliedRuntime)
	}

	runtime.GC()
	return nil
}

func applySubscriptionProxy(cfg conf.SubscriptionProxyConfig) {
	if err := applySubscriptionProxyForReload(cfg); err != nil {
		log.WithField("err", err).Warn("Subscription proxy apply failed")
	}
}

func queueReload(reloadCh chan struct{}) {
	if reloadCh == nil {
		return
	}
	select {
	case reloadCh <- struct{}{}:
	default:
	}
}

func newNodeForConfig(cfg *conf.Conf) (*node.Node, error) {
	var (
		nodesInstance *node.Node
		err           error
	)
	if cfg != nil && cfg.MachineConfig.Enabled {
		log.WithFields(log.Fields{
			"node_count":        len(cfg.NodeConfigs),
			"continue_on_error": cfg.MachineConfig.ContinueOnError,
		}).Info("Machine mode enabled")
		nodesInstance, err = newMachineNodeForReload(cfg.NodeConfigs, cfg.RealtimeConfig, cfg.MachineConfig)
	} else {
		nodesInstance, err = newNodeForReload(cfg.NodeConfigs, cfg.RealtimeConfig)
	}
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		nodesInstance.SetAutoHY2PortForward(cfg.RuntimeConfig.AutoHY2PortForward)
	}
	return nodesInstance, nil
}

func logMachineNodeFailures(nodesInstance *node.Node) {
	if nodesInstance == nil {
		return
	}
	for _, failure := range nodesInstance.Failures() {
		log.WithFields(log.Fields{
			"api_host": failure.Config.APIHost,
			"node_id":  failure.Config.NodeID,
			"err":      failure.Err,
		}).Warn("Node skipped in machine mode")
	}
}
