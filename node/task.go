package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/task"
	vCore "github.com/keli-123456/kelinode/core"
	log "github.com/sirupsen/logrus"
)

type userSyncSummary struct {
	Deleted int
	Added   int
	Updated int
}

func (c *Controller) startTasks(node *panel.NodeInfo) {
	// fetch node config task (lightweight, for reload detection)
	c.nodeConfigMonitorPeriodic = &task.Task{
		Name:     "nodeConfigMonitor",
		Interval: node.PullInterval,
		Timeout:  2 * time.Minute,
		Execute:  c.nodeConfigMonitor,
	}
	// sync users/alive list task (may be heavier)
	c.nodeUserMonitorPeriodic = &task.Task{
		Name:     "nodeUserMonitor",
		Interval: node.PullInterval,
		Timeout:  10 * time.Minute,
		Execute:  c.nodeUserMonitor,
	}
	// fetch user list task
	c.userReportPeriodic = &task.Task{
		Name:     "reportUserTrafficTask",
		Interval: node.PushInterval,
		Timeout:  10 * time.Minute,
		Execute:  c.reportUserTrafficTask,
	}
	log.WithField("tag", c.tag).Info("Start monitor node status")
	_ = c.nodeConfigMonitorPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start sync users status")
	_ = c.nodeUserMonitorPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report node status")
	_ = c.userReportPeriodic.Start(false)
	if node.Security == panel.Tls {
		switch c.info.Common.CertInfo.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Name:     "renewCertTask",
				Interval: time.Hour * 24,
				Timeout:  30 * time.Minute,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}
}

func (c *Controller) reloadTask() {
	newClient, err := panel.New(c.conf)
	if err != nil {
		log.Panic("Tasks reload failed")
	}
	c.apiClient = newClient
	c.nodeConfigMonitorPeriodic.Close()
	c.nodeUserMonitorPeriodic.Close()
	c.userReportPeriodic.Close()
	if c.renewCertPeriodic != nil {
		c.renewCertPeriodic.Close()
	}
	c.startTasks(c.info)
}

func (c *Controller) nodeConfigMonitor(ctx context.Context) (err error) {
	changed, err := c.executeNodeConfigCheck(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	if changed {
		log.WithFields(log.Fields{
			"tag": c.tag,
		}).Info("Got new node info, reload")
		return nil
	}
	log.WithField("tag", c.tag).Debug("Node info no change")
	return nil
}

func (c *Controller) executeNodeConfigCheck(ctx context.Context) (bool, error) {
	c.configCheckMu.Lock()
	defer c.configCheckMu.Unlock()

	newN, err := c.apiClient.GetNodeInfo(ctx)
	if err != nil {
		return false, err
	}
	if newN != nil {
		// Non-blocking signal to avoid goroutine stuck when channel is full or nil
		if c.server.ReloadCh != nil {
			select {
			case c.server.ReloadCh <- struct{}{}:
			default:
			}
		} else {
			return false, fmt.Errorf("reload channel is nil")
		}
		// Stop current task execution. Core/nodes may be closing/reloading now.
		return true, nil
	}
	return false, nil
}

func (c *Controller) nodeUserMonitor(ctx context.Context) (err error) {
	summary, err := c.executeNodeUserSync(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Sync users failed")
		return nil
	}
	if summary.Added+summary.Deleted+summary.Updated != 0 {
		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added, %d user updated", summary.Deleted, summary.Added, summary.Updated)
	}
	return nil
}

func (c *Controller) executeNodeUserSync(ctx context.Context) (userSyncSummary, error) {
	c.userSyncMu.Lock()
	defer c.userSyncMu.Unlock()

	var (
		deleted []panel.UserInfo
		added   []panel.UserInfo
		updated []panel.UserInfo
		summary userSyncSummary
	)

	// get user info (prefer delta)
	if c.userDeltaSupported {
		delta, derr := c.apiClient.GetUserDelta(ctx, c.userRevision)
		if derr != nil {
			if errors.Is(derr, panel.ErrUserDeltaNotSupported) {
				c.userDeltaSupported = false
			} else {
				return summary, derr
			}
		} else if delta != nil {
			c.userRevision = delta.Revision
			if delta.Full {
				newU := delta.Users
				if len(newU) == 0 {
					log.WithField("tag", c.tag).Debug("User list no change")
				} else {
					deleted, added, updated = compareUserList(c.userList, newU)
					c.userList = newU
				}
			} else {
				if len(delta.Deleted) == 0 && len(delta.Upsert) == 0 {
					log.WithField("tag", c.tag).Debug("User list no change")
				} else {
					next, deletedApplied, addedApplied, updatedApplied := applyUserDelta(c.userList, delta.Deleted, delta.Upsert)
					deleted, added, updated = deletedApplied, addedApplied, updatedApplied
					c.userList = next
				}
			}
			if c.userSyncStatePath != "" {
				_ = saveUserSyncState(c.userSyncStatePath, &userSyncStateFile{Revision: c.userRevision, Users: c.userList})
			}
		}
	}

	// fallback to full user list API
	if !c.userDeltaSupported {
		newU, err := c.apiClient.GetUserList(ctx)
		if err != nil {
			return summary, err
		}
		// node no changed, check users
		if len(newU) == 0 {
			log.WithField("tag", c.tag).Debug("User list no change")
		} else {
			deleted, added, updated = compareUserList(c.userList, newU)
			c.userList = newU
		}
	}

	// get user alive
	newA, aliveErr := c.apiClient.GetUserAlive(ctx)
	if aliveErr != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": aliveErr,
		}).Warn("Get user alive list failed, keeping previous snapshot")
	} else if newA != nil {
		c.aliveMap = newA
		c.limiter.SetAliveList(newA)
	}
	if len(updated) > 0 {
		if err := c.applyUpdatedUsers(updated); err != nil {
			return summary, err
		}
	}
	if len(deleted) > 0 {
		// have deleted users
		if err := c.applyDeletedUsers(ctx, deleted); err != nil {
			if ctx.Err() != nil {
				return summary, nil
			}
			return summary, err
		}
	}
	if len(added) > 0 {
		// have added users
		if err := c.applyAddedUsers(ctx, added); err != nil {
			if ctx.Err() != nil {
				return summary, nil
			}
			return summary, err
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted)
	}
	summary = userSyncSummary{
		Deleted: len(deleted),
		Added:   len(added),
		Updated: len(updated),
	}
	return summary, nil
}

func (c *Controller) applyUpdatedUsers(updated []panel.UserInfo) error {
	c.limiter.UpdateUserInfo(c.tag, updated)
	if c.updateUserIDsFn != nil {
		return c.updateUserIDsFn(c.tag, updated)
	}
	return c.server.UpdateUserIDs(c.tag, updated)
}

func (c *Controller) applyDeletedUsers(ctx context.Context, deleted []panel.UserInfo) error {
	if c.delUsersFn != nil {
		return c.delUsersFn(ctx, deleted, c.tag)
	}
	return c.server.DelUsers(ctx, deleted, c.tag)
}

func (c *Controller) applyAddedUsers(ctx context.Context, added []panel.UserInfo) error {
	params := &vCore.AddUsersParams{
		Tag:      c.tag,
		NodeInfo: c.info,
		Users:    added,
	}
	if c.addUsersFn != nil {
		_, err := c.addUsersFn(ctx, params)
		return err
	}
	_, err := c.server.AddUsersWithContext(ctx, params)
	return err
}
