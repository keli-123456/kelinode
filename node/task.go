package node

import (
	"context"
	"errors"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/task"
	vCore "github.com/keli-123456/kelinode/core"
	log "github.com/sirupsen/logrus"
)

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
	newN, err := c.apiClient.GetNodeInfo(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	if newN != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
		}).Info("Got new node info, reload")
		// Non-blocking signal to avoid goroutine stuck when channel is full or nil
		if c.server.ReloadCh != nil {
			select {
			case c.server.ReloadCh <- struct{}{}:
			default:
			}
		} else {
			log.Panic("Reload failed")
		}
		// Stop current task execution. Core/nodes may be closing/reloading now.
		return nil
	}
	log.WithField("tag", c.tag).Debug("Node info no change")
	return nil
}

func (c *Controller) nodeUserMonitor(ctx context.Context) (err error) {
	var (
		deleted []panel.UserInfo
		added   []panel.UserInfo
		updated []panel.UserInfo
	)

	// get user info (prefer delta)
	if c.userDeltaSupported {
		delta, derr := c.apiClient.GetUserDelta(ctx, c.userRevision)
		if derr != nil {
			if errors.Is(derr, panel.ErrUserDeltaNotSupported) {
				c.userDeltaSupported = false
			} else {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": derr,
				}).Error("Get user delta failed")
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
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Get user list failed")
			return nil
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
	newA, err := c.apiClient.GetUserAlive(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get alive list failed")
		return nil
	}

	// update alive list
	if newA != nil {
		c.limiter.SetAliveList(newA)
	}
	if len(updated) > 0 {
		c.limiter.UpdateUserInfo(c.tag, updated)
		if err := c.server.UpdateUserIDs(c.tag, updated); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update users failed")
			return nil
		}
	}
	if len(deleted) > 0 {
		// have deleted users
		err = c.server.DelUsers(ctx, deleted, c.tag)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}
	if len(added) > 0 {
		// have added users
		_, err = c.server.AddUsersWithContext(ctx, &vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("limiter users failed")
			return nil
		}
	}
	if len(added)+len(deleted)+len(updated) != 0 {
		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added, %d user updated", len(deleted), len(added), len(updated))
	}
	return nil
}
