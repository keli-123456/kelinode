package node

import (
	"context"
	"errors"
	"fmt"
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
	apiClient                 *panel.Client
	tag                       string
	limiter                   *limiter.Limiter
	userList                  []panel.UserInfo
	userRevision              int64
	userDeltaSupported        bool
	userSyncStatePath         string
	realtimeConfig            conf.RealtimeConfig
	aliveMap                  map[int]int
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
	realtimeConfigCh          chan struct{}
	realtimeUserCh            chan struct{}
}

// NewController return a Node controller with default parameters.
func NewController(api *panel.Client, conf *conf.NodeConfig, info *panel.NodeInfo, realtime conf.RealtimeConfig) *Controller {
	controller := &Controller{
		apiClient:          api,
		info:               info,
		conf:               conf,
		realtimeConfig:     realtime,
		userDeltaSupported: true,
	}
	if conf != nil {
		controller.userSyncStatePath = userSyncStatePath(conf.APIHost, conf.NodeID)
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start(x *core.V2Core) error {
	// Init Core
	c.server = x
	var err error
	// First fetch Node Info
	node := c.info
	if node == nil {
		c.info, err = c.apiClient.GetNodeInfo(context.Background())
		if err != nil {
			return fmt.Errorf("get node info error: %s", err)
		}
		node = c.info
	}
	// Update user
	c.userList, err = c.loadAndSyncUsers(context.Background())
	if err != nil {
		return fmt.Errorf("get user list error: %s", err)
	}
	if len(c.userList) == 0 {
		return errors.New("add users error: not have any user")
	}
	c.aliveMap, err = c.apiClient.GetUserAlive(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get user alive list: %s", err)
	}
	c.tag = node.Tag

	// add limiter
	l := limiter.AddLimiter(c.tag, c.userList, c.aliveMap)
	c.limiter = l
	if node.Security == panel.Tls {
		err = c.requestCert()
		if err != nil {
			return fmt.Errorf("request cert error: %s", err)
		}
	}
	// Add new tag
	err = c.server.AddNode(c.tag, node)
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
	c.info = node
	c.startTasks(node)
	c.startRealtime()
	return nil
}

func (c *Controller) loadAndSyncUsers(ctx context.Context) ([]panel.UserInfo, error) {
	// Try to warm start from local cache (revision + user list), then catch up via delta.
	if c.userDeltaSupported && c.userSyncStatePath != "" {
		if state, err := loadUserSyncState(c.userSyncStatePath); err == nil && state != nil && len(state.Users) > 0 && state.Revision > 0 {
			if delta, err := c.apiClient.GetUserDelta(ctx, state.Revision); err == nil && delta != nil {
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
		delta, err := c.apiClient.GetUserDelta(ctx, 0)
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

	users, err := c.apiClient.GetUserList(ctx)
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
