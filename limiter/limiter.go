package limiter

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juju/ratelimit"
	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
)

var limitLock sync.RWMutex
var limiter map[string]*Limiter

func Init() {
	limiter = map[string]*Limiter{}
}

type Limiter struct {
	Nodetype      string
	SpeedLimit    int
	UserOnlineIP  *sync.Map      // Key: TagUUID, value: {Key: Ip, value: Uid}
	OldUserOnline *sync.Map      // Key: Ip, value: Uid
	UUIDtoUID     map[string]int // Key: UUID, value: Uid
	UserLimitInfo *sync.Map      // Key: TagUUID value: UserLimitInfo
	SpeedLimiter  *sync.Map      // key: TagUUID, value: *ratelimit.Bucket
	aliveList     atomic.Value   // map[int]int, Key: Uid, value: alive_ip (immutable snapshot)
	aliveIPList   atomic.Value   // map[int]map[string]struct{}, Key: Uid, value: alive ip set
	aliveMode     atomic.Int32
}

type UserLimitInfo struct {
	UID               int
	SpeedLimit        int
	DeviceLimit       int
	DynamicSpeedLimit int
	ExpireTime        int64
	OverLimit         bool
}

func AddLimiter(nodetype string, tag string, users []panel.UserInfo, aliveList map[int]int) *Limiter {
	info := &Limiter{
		Nodetype:      nodetype,
		UserOnlineIP:  new(sync.Map),
		UserLimitInfo: new(sync.Map),
		SpeedLimiter:  new(sync.Map),
		OldUserOnline: new(sync.Map),
	}
	info.SetAliveList(aliveList)
	uuidmap := make(map[string]int)
	for i := range users {
		uuidmap[users[i].Uuid] = users[i].Id
		userLimit := &UserLimitInfo{}
		userLimit.UID = users[i].Id
		if users[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = users[i].SpeedLimit
		}
		if users[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = users[i].DeviceLimit
		}
		userLimit.OverLimit = false
		info.UserLimitInfo.Store(format.UserTag(tag, users[i].Uuid), userLimit)
	}
	info.UUIDtoUID = uuidmap
	limitLock.Lock()
	limiter[tag] = info
	limitLock.Unlock()
	return info
}

func (l *Limiter) SetAliveList(alive map[int]int) {
	if alive == nil {
		alive = make(map[int]int)
	}
	next := make(map[int]int, len(alive))
	for k, v := range alive {
		next[k] = v
	}
	l.aliveList.Store(next)
}

func (l *Limiter) SetAliveSnapshot(snapshot *panel.AliveMap) {
	if snapshot == nil {
		l.SetAliveList(nil)
		l.aliveIPList.Store(map[int]map[string]struct{}{})
		l.aliveMode.Store(0)
		return
	}

	l.SetAliveList(snapshot.Alive)

	nextIPs := make(map[int]map[string]struct{}, len(snapshot.AliveIPs))
	for uid, ips := range snapshot.AliveIPs {
		normalizedUID := uid
		if normalizedUID <= 0 {
			continue
		}

		ipSet := make(map[string]struct{}, len(ips))
		for _, ip := range ips {
			trimmed := strings.TrimPrefix(strings.TrimSpace(ip), "::ffff:")
			if trimmed == "" {
				continue
			}
			ipSet[trimmed] = struct{}{}
		}
		if len(ipSet) == 0 {
			continue
		}
		nextIPs[normalizedUID] = ipSet
	}

	l.aliveIPList.Store(nextIPs)
	l.aliveMode.Store(int32(snapshot.Mode))
}

func (l *Limiter) getAliveIP(uid int) int {
	if l == nil {
		return 0
	}
	v := l.aliveList.Load()
	if v == nil {
		return 0
	}
	return v.(map[int]int)[uid]
}

func (l *Limiter) getAliveMode() int {
	if l == nil {
		return 0
	}
	return int(l.aliveMode.Load())
}

func (l *Limiter) hasGlobalAliveIP(uid int, ip string) bool {
	if l == nil || l.getAliveMode() != 1 {
		return false
	}

	v := l.aliveIPList.Load()
	if v == nil {
		return false
	}

	ipSets := v.(map[int]map[string]struct{})
	userIPs, ok := ipSets[uid]
	if !ok {
		return false
	}

	_, exists := userIPs[ip]
	return exists
}

func (l *Limiter) deleteAliveIDs(ids []int) {
	if l == nil || len(ids) == 0 {
		return
	}
	v := l.aliveList.Load()
	if v == nil {
		return
	}
	cur := v.(map[int]int)
	if len(cur) == 0 {
		return
	}
	next := make(map[int]int, len(cur))
	for k, val := range cur {
		next[k] = val
	}
	for _, id := range ids {
		delete(next, id)
	}
	l.aliveList.Store(next)
}

func GetLimiter(tag string) (info *Limiter, err error) {
	limitLock.RLock()
	info, ok := limiter[tag]
	limitLock.RUnlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return info, nil
}

func DeleteLimiter(tag string) {
	limitLock.Lock()
	delete(limiter, tag)
	limitLock.Unlock()
}

func (l *Limiter) UpdateUser(tag string, added []panel.UserInfo, deleted []panel.UserInfo) {
	deletedIDs := make([]int, 0, len(deleted))
	for i := range deleted {
		l.UserLimitInfo.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.UserOnlineIP.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.SpeedLimiter.Delete(format.UserTag(tag, deleted[i].Uuid))
		delete(l.UUIDtoUID, deleted[i].Uuid)
		deletedIDs = append(deletedIDs, deleted[i].Id)
	}
	l.deleteAliveIDs(deletedIDs)
	for i := range added {
		userLimit := &UserLimitInfo{
			UID: added[i].Id,
		}
		if added[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = added[i].SpeedLimit
			userLimit.ExpireTime = 0
		}
		if added[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = added[i].DeviceLimit
		}
		userLimit.OverLimit = false
		l.UserLimitInfo.Store(format.UserTag(tag, added[i].Uuid), userLimit)
		l.UUIDtoUID[added[i].Uuid] = added[i].Id
	}
}

func (l *Limiter) UpdateUserInfo(tag string, updated []panel.UserInfo) {
	for i := range updated {
		key := format.UserTag(tag, updated[i].Uuid)
		if v, ok := l.UserLimitInfo.Load(key); ok {
			info := v.(*UserLimitInfo)
			info.UID = updated[i].Id
			info.SpeedLimit = updated[i].SpeedLimit
			info.DeviceLimit = updated[i].DeviceLimit
			info.OverLimit = false
		} else {
			userLimit := &UserLimitInfo{
				UID:         updated[i].Id,
				SpeedLimit:  updated[i].SpeedLimit,
				DeviceLimit: updated[i].DeviceLimit,
				OverLimit:   false,
			}
			l.UserLimitInfo.Store(key, userLimit)
		}
		l.UUIDtoUID[updated[i].Uuid] = updated[i].Id
		// Ensure new speed limits take effect on next connections.
		l.SpeedLimiter.Delete(key)
	}
}

func (l *Limiter) UpdateDynamicSpeedLimit(tag, uuid string, limit int, expire time.Time) error {
	if v, ok := l.UserLimitInfo.Load(format.UserTag(tag, uuid)); ok {
		info := v.(*UserLimitInfo)
		info.DynamicSpeedLimit = limit
		info.ExpireTime = expire.Unix()
	} else {
		return errors.New("not found")
	}
	return nil
}

func (l *Limiter) tracksUDPSource() bool {
	switch l.Nodetype {
	case "hysteria2", "tuic":
		return true
	default:
		return false
	}
}

func (l *Limiter) getOrCreateUserOnlineIPMap(taguuid string) (*sync.Map, bool) {
	if v, ok := l.UserOnlineIP.Load(taguuid); ok {
		return v.(*sync.Map), false
	}

	newipMap := new(sync.Map)
	if v, loaded := l.UserOnlineIP.LoadOrStore(taguuid, newipMap); loaded {
		return v.(*sync.Map), false
	}

	return newipMap, true
}

func (l *Limiter) ownsOldIP(uid int, ip string) bool {
	if v, ok := l.OldUserOnline.Load(ip); ok {
		return v.(int) == uid
	}
	return false
}

func (l *Limiter) isKnownAliveIP(uid int, ip string) bool {
	return l.ownsOldIP(uid, ip) || l.hasGlobalAliveIP(uid, ip)
}

func (l *Limiter) pendingNewIPCount(taguuid string, uid int) int {
	v, ok := l.UserOnlineIP.Load(taguuid)
	if !ok {
		return 0
	}

	count := 0
	ipMap := v.(*sync.Map)
	ipMap.Range(func(key, value interface{}) bool {
		currentUID := value.(int)
		if currentUID != uid {
			return true
		}

		ip := key.(string)
		if l.isKnownAliveIP(uid, ip) {
			return true
		}

		count++
		return true
	})

	return count
}

func (l *Limiter) CheckLimit(taguuid string, ip string, noUDPsource bool) (Bucket *ratelimit.Bucket, Reject bool) {
	// check if ipv4 mapped ipv6
	ip = strings.TrimPrefix(ip, "::ffff:")

	// check and gen speed limit Bucket
	nodeLimit := l.SpeedLimit
	userLimit := 0
	deviceLimit := 0
	var uid int
	if v, ok := l.UserLimitInfo.Load(taguuid); ok {
		u := v.(*UserLimitInfo)
		deviceLimit = u.DeviceLimit
		uid = u.UID
		if u.ExpireTime < time.Now().Unix() && u.ExpireTime != 0 {
			if u.SpeedLimit != 0 {
				userLimit = u.SpeedLimit
				u.DynamicSpeedLimit = 0
				u.ExpireTime = 0
			} else {
				l.UserLimitInfo.Delete(taguuid)
			}
		} else {
			userLimit = determineSpeedLimit(u.SpeedLimit, u.DynamicSpeedLimit)
		}
	} else {
		return nil, true
	}
		if noUDPsource || l.tracksUDPSource() {
			aliveIp := l.getAliveIP(uid)
			ipMap, created := l.getOrCreateUserOnlineIPMap(taguuid)
			if _, loaded := ipMap.Load(ip); !loaded {
				if !l.isKnownAliveIP(uid, ip) && deviceLimit > 0 {
					if deviceLimit <= aliveIp+l.pendingNewIPCount(taguuid, uid) {
						if created {
							l.UserOnlineIP.Delete(taguuid)
						}
					return nil, true
				}
			}
			ipMap.Store(ip, uid)
		}
	}

	limit := int64(determineSpeedLimit(nodeLimit, userLimit)) * 1000000 / 8 // If you need the Speed limit
	if limit <= 0 {
		return nil, false
	}

	// Avoid allocating a new bucket on every connection.
	if v, ok := l.SpeedLimiter.Load(taguuid); ok {
		return v.(*ratelimit.Bucket), false
	}

	Bucket = ratelimit.NewBucketWithQuantum(time.Second, limit, limit) // Byte/s
	if v, loaded := l.SpeedLimiter.LoadOrStore(taguuid, Bucket); loaded {
		return v.(*ratelimit.Bucket), false
	}
	return Bucket, false
}

func (l *Limiter) GetOnlineDevice() (*[]panel.OnlineUser, error) {
	var onlineUser []panel.OnlineUser
	l.UserOnlineIP.Range(func(key, value interface{}) bool {
		taguuid := key.(string)
		ipMap := value.(*sync.Map)
		ipMap.Range(func(key, value interface{}) bool {
			uid := value.(int)
			ip := key.(string)
			onlineUser = append(onlineUser, panel.OnlineUser{UID: uid, IP: ip, TagUUID: taguuid})
			return true
		})
		return true
	})

	return &onlineUser, nil
}

func (l *Limiter) CommitOnlineDeviceReport(reported []panel.OnlineUser) {
	nextOld := new(sync.Map)

	for i := range reported {
		online := reported[i]
		if online.UID <= 0 || online.IP == "" {
			continue
		}

		nextOld.Store(online.IP, online.UID)

		if online.TagUUID == "" {
			continue
		}
		v, ok := l.UserOnlineIP.Load(online.TagUUID)
		if !ok {
			continue
		}
		ipMap := v.(*sync.Map)
		if currentUID, loaded := ipMap.Load(online.IP); loaded {
			if currentUID.(int) == online.UID {
				ipMap.Delete(online.IP)
			}
		}
		empty := true
		ipMap.Range(func(_, _ interface{}) bool {
			empty = false
			return false
		})
		if empty {
			l.UserOnlineIP.Delete(online.TagUUID)
		}
	}

	l.OldUserOnline = nextOld
}

type UserIpList struct {
	Uid    int      `json:"Uid"`
	IpList []string `json:"Ips"`
}
