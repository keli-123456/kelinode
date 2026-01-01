package node

import (
	"context"

	panel "github.com/keli-123456/kelinode/api/v2board"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) reportUserTrafficTask(ctx context.Context) (err error) {
	var reportmin = 0
	var devicemin = 0
	if c.info.Common.BaseConfig != nil {
		reportmin = c.info.Common.BaseConfig.NodeReportMinTraffic
		devicemin = c.info.Common.BaseConfig.DeviceOnlineMinTraffic
	}

	// Decouple thresholds:
	// - reportmin affects traffic reporting only
	// - devicemin affects online user reporting only
	var filterOnlineByTraffic = devicemin > 0
	var onlineAllowedUID map[int]struct{}
	if filterOnlineByTraffic {
		onlineTraffic, rollbackOnlineTraffic, _ := c.server.GetUserTrafficSlice(c.tag, devicemin)
		if rollbackOnlineTraffic != nil {
			rollbackOnlineTraffic()
		}
		onlineAllowedUID = make(map[int]struct{}, len(onlineTraffic))
		for _, t := range onlineTraffic {
			onlineAllowedUID[t.UID] = struct{}{}
		}
	}

	userTraffic, rollbackUserTraffic, _ := c.server.GetUserTrafficSlice(c.tag, reportmin)
	hadTraffic := len(userTraffic) > 0
	if len(userTraffic) > 0 {
		err = c.apiClient.ReportUserTraffic(ctx, userTraffic)
		if err != nil {
			if rollbackUserTraffic != nil {
				rollbackUserTraffic()
			}
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Report user traffic failed")
		} else {
			log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
			//log.WithField("tag", c.tag).Debugf("User traffic: %+v", userTraffic)
		}
	}

	onlineDevice, onlineErr := c.limiter.GetOnlineDevice()
	if onlineErr != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": onlineErr,
		}).Error("Get online device failed")
	} else if onlineDevice != nil && len(*onlineDevice) > 0 {
		var result []panel.OnlineUser
		for _, online := range *onlineDevice {
			if filterOnlineByTraffic {
				if _, ok := onlineAllowedUID[online.UID]; !ok {
					continue
				}
			}
			result = append(result, online)
		}
		data := make(map[int][]string)
		for _, onlineuser := range result {
			// json structure: { UID1:["ip1","ip2"],UID2:["ip3","ip4"] }
			data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
		}
		if err = c.apiClient.ReportNodeOnlineUsers(ctx, &data); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Report online users failed")
		} else {
			log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(*onlineDevice), len(result))
			//log.WithField("tag", c.tag).Debugf("Online users: %+v", data)
		}
	} else if !hadTraffic {
		log.WithField("tag", c.tag).Info("No traffic or online activity to report")
	}

	userTraffic = nil
	return nil
}

func compareUserList(old, new []panel.UserInfo) (deleted, added, updated []panel.UserInfo) {
	oldMap := make(map[string]panel.UserInfo, len(old))
	for _, user := range old {
		oldMap[user.Uuid] = user
	}

	for _, user := range new {
		oldUser, exists := oldMap[user.Uuid]
		if !exists {
			added = append(added, user)
			continue
		}
		if oldUser.Id != user.Id || oldUser.SpeedLimit != user.SpeedLimit || oldUser.DeviceLimit != user.DeviceLimit {
			updated = append(updated, user)
		}
		delete(oldMap, user.Uuid)
	}

	for _, user := range oldMap {
		deleted = append(deleted, user)
	}

	return deleted, added, updated
}
