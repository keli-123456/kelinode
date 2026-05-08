package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/task"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/core"
	"github.com/keli-123456/kelinode/limiter"
	log "github.com/sirupsen/logrus"
)

type Controller struct {
	server                    *core.V2Core
	controlPlane              ControlPlane
	controlPlaneFactory       ControlPlaneFactory
	edgeTrafficBridge         EdgeTrafficBridge
	edgeSidecarBridge         EdgeSidecarBridge
	tag                       string
	limiter                   *limiter.Limiter
	userList                  []panel.UserInfo
	userRevision              int64
	userDeltaSupported        bool
	userSyncStatePath         string
	realtimeConfig            conf.RealtimeConfig
	aliveMap                  map[int]int
	aliveSnapshot             *panel.AliveMap
	conf                      *conf.NodeConfig
	info                      *panel.NodeInfo
	nodeConfigMonitorPeriodic *task.Task
	nodeUserMonitorPeriodic   *task.Task
	userReportPeriodic        *task.Task
	renewCertPeriodic         *task.Task
	userSyncMu                sync.Mutex
	configCheckMu             sync.Mutex
	realtimeClient            *RealtimeClient
	realtimeCancel            context.CancelFunc
	realtimeConfigCh          chan realtimeMessage
	realtimeUserCh            chan realtimeMessage
	executeConfigCheckFn      func(context.Context) (bool, error)
	executeUserSyncFn         func(context.Context) (userSyncSummary, error)
	updateUserIDsFn           func(string, []panel.UserInfo) error
	delUsersFn                func(context.Context, []panel.UserInfo, string) error
	addUsersFn                func(context.Context, *core.AddUsersParams) (int, error)
}

type controllerStartState struct {
	node          *panel.NodeInfo
	userList      []panel.UserInfo
	aliveMap      map[int]int
	aliveSnapshot *panel.AliveMap
}

// NewController return a Node controller with default parameters.
func NewController(api *panel.Client, conf *conf.NodeConfig, info *panel.NodeInfo, realtime conf.RealtimeConfig) *Controller {
	return NewControllerWithControlPlane(newPanelControlPlane(api, conf), conf, info, realtime)
}

// NewControllerWithControlPlane return a Node controller with a custom control plane.
func NewControllerWithControlPlane(controlPlane ControlPlane, conf *conf.NodeConfig, info *panel.NodeInfo, realtime conf.RealtimeConfig) *Controller {
	controller := &Controller{
		controlPlane:        controlPlane,
		controlPlaneFactory: defaultControlPlaneFactory(),
		info:                info,
		conf:                conf,
		realtimeConfig:      realtime,
		userDeltaSupported:  true,
	}
	if conf != nil {
		controller.userSyncStatePath = userSyncStatePath(conf.ConfigDir, conf.APIHost, conf.NodeID)
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start(x *core.V2Core) error {
	state, err := c.prepareStart(x)
	if err != nil {
		return err
	}
	return c.activatePreparedStart(state)
}

func (c *Controller) StartReplacing(x *core.V2Core, old *Controller) (bool, error) {
	state, err := c.prepareStart(x)
	if err != nil {
		return true, err
	}
	if old != nil {
		if err := old.Close(); err != nil {
			return false, err
		}
	}
	if err := c.activatePreparedStart(state); err != nil {
		_ = c.Close()
		return false, err
	}
	return false, nil
}

func (c *Controller) prepareStart(x *core.V2Core) (*controllerStartState, error) {
	c.server = x
	node := c.info
	if node == nil {
		var err error
		c.info, err = c.controlPlane.GetNodeInfo(context.Background())
		if err != nil {
			return nil, fmt.Errorf("get node info error: %s", err)
		}
		node = c.info
	}
	c.info = node
	c.tag = node.Tag
	if bootstrapProvider, ok := c.controlPlane.(ControlPlaneRealtimeBootstrap); ok {
		bootstrap, err := bootstrapProvider.GetRealtimeBootstrap(context.Background())
		if err != nil {
			log.WithField("err", err).Debug("control plane handshake bootstrap unavailable")
		} else if bootstrap != nil && bootstrap.Enabled && strings.TrimSpace(bootstrap.URL) != "" && strings.TrimSpace(c.realtimeConfig.URL) == "" {
			c.realtimeConfig.Enabled = true
			c.realtimeConfig.URL = strings.TrimSpace(bootstrap.URL)
		}
	}
	userList, err := c.loadAndSyncUsers(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get user list error: %s", err)
	}
	if len(userList) == 0 {
		return nil, errors.New("add users error: not have any user")
	}
	aliveMap, err := c.controlPlane.GetUserAlive(context.Background())
	if err != nil {
		log.WithFields(log.Fields{
			"tag": node.Tag,
			"err": err,
		}).Warn("Get user alive list failed, starting with cached snapshot")
		aliveMap = c.controlPlane.CachedAliveMap()
	}
	aliveSnapshot := c.controlPlane.CachedAliveSnapshot()
	if aliveMap == nil {
		aliveMap = make(map[int]int)
	}

	if node.Security == panel.Tls {
		if err := c.requestCert(); err != nil {
			return nil, fmt.Errorf("request cert error: %s", err)
		}
	}

	return &controllerStartState{
		node:          node,
		userList:      userList,
		aliveMap:      aliveMap,
		aliveSnapshot: aliveSnapshot,
	}, nil
}

func (c *Controller) activatePreparedStart(state *controllerStartState) error {
	if state == nil || state.node == nil {
		return errors.New("prepared node state is empty")
	}
	node := state.node
	c.info = node
	c.userList = state.userList
	c.aliveMap = state.aliveMap
	c.aliveSnapshot = state.aliveSnapshot
	c.tag = node.Tag

	l := limiter.AddLimiter(c.info.Type, c.tag, c.userList, c.aliveMap)
	l.SetAliveSnapshot(c.aliveSnapshot)
	l.SetDefaultDeviceLimit(defaultDeviceLimitFromNode(node))
	c.limiter = l

	err := c.server.AddNode(c.tag, node)
	if err != nil {
		return fmt.Errorf("add new node error: %s", err)
	}
	added, err := c.server.AddUsers(&core.AddUsersParams{
		Tag:      c.tag,
		Users:    c.userList,
		NodeInfo: node,
	})
	if err != nil {
		return fmt.Errorf("add users error: %s", err)
	}
	log.WithField("tag", c.tag).Infof("Added %d new users", added)
	if err := c.applyEdgeSidecar(context.Background()); err != nil {
		return fmt.Errorf("apply edge sidecar error: %s", err)
	}
	c.info = node
	c.startTasks(node)
	c.startRealtime()
	return nil
}

func (c *Controller) loadAndSyncUsers(ctx context.Context) ([]panel.UserInfo, error) {
	// Try to warm start from local cache (revision + user list), then catch up via delta.
	if c.userDeltaSupported && c.userSyncStatePath != "" {
		if state, err := loadUserSyncState(c.userSyncStatePath); err == nil && state != nil && len(state.Users) > 0 && state.Revision > 0 {
			if delta, err := c.controlPlane.GetUserDelta(ctx, state.Revision); err == nil && delta != nil {
				if delta.Full {
					c.userRevision = delta.Revision
					_ = saveUserSyncState(c.userSyncStatePath, &userSyncStateFile{Revision: c.userRevision, Users: delta.Users})
					return delta.Users, nil
				}
				next, _, _, _ := applyUserDelta(state.Users, delta.Deleted, delta.Upsert)
				c.userRevision = delta.Revision
				_ = saveUserSyncState(c.userSyncStatePath, &userSyncStateFile{Revision: c.userRevision, Users: next})
				return next, nil
			} else if errors.Is(err, panel.ErrUserDeltaNotSupported) {
				c.userDeltaSupported = false
			}
		}
	}

	// Fallback: fetch from panel.
	if c.userDeltaSupported {
		delta, err := c.controlPlane.GetUserDelta(ctx, 0)
		if err != nil {
			if errors.Is(err, panel.ErrUserDeltaNotSupported) {
				c.userDeltaSupported = false
			} else {
				return nil, err
			}
		} else if delta != nil {
			users := delta.Users
			if !delta.Full {
				users = delta.Upsert
			}
			c.userRevision = delta.Revision
			if c.userSyncStatePath != "" {
				_ = saveUserSyncState(c.userSyncStatePath, &userSyncStateFile{Revision: c.userRevision, Users: users})
			}
			return users, nil
		}
	}

	users, err := c.controlPlane.GetUserList(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	if c.realtimeCancel != nil {
		c.realtimeCancel()
		c.realtimeCancel = nil
	}
	if c.realtimeClient != nil {
		c.realtimeClient.Close()
		c.realtimeClient = nil
	}
	limiter.DeleteLimiter(c.tag)
	if c.nodeConfigMonitorPeriodic != nil {
		c.nodeConfigMonitorPeriodic.Close()
	}
	if c.nodeUserMonitorPeriodic != nil {
		c.nodeUserMonitorPeriodic.Close()
	}
	if c.userReportPeriodic != nil {
		c.userReportPeriodic.Close()
	}
	if c.renewCertPeriodic != nil {
		c.renewCertPeriodic.Close()
	}
	err := c.server.DelNode(c.tag)
	if err != nil {
		return fmt.Errorf("del node error: %s", err)
	}
	return nil
}

func defaultDeviceLimitFromNode(node *panel.NodeInfo) int {
	const maxDeviceLimitFallback = int(^uint32(0) >> 1)
	if node == nil || node.Common == nil || node.Common.BaseConfig == nil {
		return 0
	}
	if node.Common.BaseConfig.DeviceLimitFallback < 0 {
		return 0
	}
	if node.Common.BaseConfig.DeviceLimitFallback > maxDeviceLimitFallback {
		return maxDeviceLimitFallback
	}
	return node.Common.BaseConfig.DeviceLimitFallback
}
