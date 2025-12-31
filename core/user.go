package core

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/counter"
	"github.com/keli-123456/kelinode/common/format"
	"github.com/keli-123456/kelinode/core/app/dispatcher"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/anytls"
	"github.com/xtls/xray-core/proxy/hysteria2"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/tuic"
	"github.com/xtls/xray-core/proxy/vless"
)

func (v *V2Core) getUserManagerLocked(tag string) (proxy.UserManager, error) {
	if v.ihm == nil {
		return nil, fmt.Errorf("core is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handler, err := v.ihm.GetHandler(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("no such inbound tag: %s", err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return nil, fmt.Errorf("handler %s is not implement proxy.GetInbound", tag)
	}
	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return nil, fmt.Errorf("handler %s is not implement proxy.UserManager", tag)
	}
	return userManager, nil
}

func (v *V2Core) GetUserManager(tag string) (proxy.UserManager, error) {
	v.access.Lock()
	defer v.access.Unlock()
	return v.getUserManagerLocked(tag)
}

func (vc *V2Core) DelUsers(ctx context.Context, users []panel.UserInfo, tag string) error {
	vc.access.Lock()
	dispatcherRef := vc.dispatcher
	if dispatcherRef == nil {
		vc.access.Unlock()
		return fmt.Errorf("core is not ready")
	}
	userManager, err := vc.getUserManagerLocked(tag)
	vc.access.Unlock()
	if err != nil {
		return fmt.Errorf("get user manager error: %s", err)
	}
	for i := range users {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		user := format.UserTag(tag, users[i].Uuid)
		reqCtx := ctx
		if reqCtx == nil {
			reqCtx = context.Background()
		}
		perUserCtx, cancel := context.WithTimeout(reqCtx, 30*time.Second)
		err = userManager.RemoveUser(perUserCtx, user)
		cancel()
		if err != nil && !isBenignRemoveUserError(err) {
			return err
		}

		vc.users.mapLock.Lock()
		delete(vc.users.uidMap, user)
		vc.users.mapLock.Unlock()

		if v, ok := dispatcherRef.Counter.Load(tag); ok {
			tc := v.(*counter.TrafficCounter)
			tc.Delete(user)
		}
		if v, ok := dispatcherRef.LinkManagers.Load(user); ok {
			lm := v.(*dispatcher.LinkManager)
			lm.CloseAll()
			dispatcherRef.LinkManagers.Delete(user)
		}
	}
	return nil
}

type trafficRollbackItem struct {
	storage *counter.TrafficStorage
	up      int64
	down    int64
}

func (vc *V2Core) GetUserTrafficSlice(tag string, mintraffic int) ([]panel.UserTraffic, func(), error) {
	vc.access.Lock()
	dispatcherRef := vc.dispatcher
	vc.access.Unlock()
	if dispatcherRef == nil {
		return nil, nil, nil
	}

	trafficSlice := make([]panel.UserTraffic, 0)
	rollbackItems := make([]trafficRollbackItem, 0)
	vc.users.mapLock.RLock()
	defer vc.users.mapLock.RUnlock()
	if v, ok := dispatcherRef.Counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		c.Counters.Range(func(key, value interface{}) bool {
			email := key.(string)
			traffic := value.(*counter.TrafficStorage)
			up := traffic.UpCounter.Load()
			down := traffic.DownCounter.Load()
			if up+down > int64(mintraffic*1000) {
				up = traffic.UpCounter.Swap(0)
				down = traffic.DownCounter.Swap(0)
				if vc.users.uidMap[email] == 0 {
					c.Delete(email)
					return true
				}
				trafficSlice = append(trafficSlice, panel.UserTraffic{
					UID:      vc.users.uidMap[email],
					Upload:   up,
					Download: down,
				})
				rollbackItems = append(rollbackItems, trafficRollbackItem{
					storage: traffic,
					up:      up,
					down:    down,
				})
			}
			return true
		})
		if len(trafficSlice) == 0 {
			return nil, nil, nil
		}
		return trafficSlice, func() {
			for i := range rollbackItems {
				item := rollbackItems[i]
				if item.up != 0 {
					item.storage.UpCounter.Add(item.up)
				}
				if item.down != 0 {
					item.storage.DownCounter.Add(item.down)
				}
			}
		}, nil
	}
	return nil, nil, nil
}

func (v *V2Core) AddUsers(p *AddUsersParams) (added int, err error) {
	return v.AddUsersWithContext(context.Background(), p)
}

func (v *V2Core) AddUsersWithContext(ctx context.Context, p *AddUsersParams) (added int, err error) {
	v.access.Lock()
	man, err := v.getUserManagerLocked(p.Tag)
	v.access.Unlock()
	if err != nil {
		return 0, fmt.Errorf("get user manager error: %s", err)
	}

	v.users.mapLock.Lock()
	for i := range p.Users {
		v.users.uidMap[format.UserTag(p.Tag, p.Users[i].Uuid)] = p.Users[i].Id
	}
	v.users.mapLock.Unlock()

	var users []*protocol.User
	switch p.NodeInfo.Type {
	case "vmess":
		users = buildVmessUsers(p.Tag, p.Users)
	case "vless":
		users = buildVlessUsers(p.Tag, p.Users, p.Common.Flow)
	case "trojan":
		users = buildTrojanUsers(p.Tag, p.Users)
	case "shadowsocks":
		users = buildSSUsers(p.Tag,
			p.Users,
			p.Common.Cipher,
			p.Common.ServerKey)
	case "hysteria2":
		users = buildHysteria2Users(p.Tag, p.Users)
	case "tuic":
		users = buildTuicUsers(p.Tag, p.Users)
	case "anytls":
		users = buildAnyTLSUsers(p.Tag, p.Users)
	default:
		return 0, fmt.Errorf("unsupported node type: %s", p.NodeInfo.Type)
	}
	for _, u := range users {
		if ctx != nil && ctx.Err() != nil {
			return added, ctx.Err()
		}
		mUser, err := u.ToMemoryUser()
		if err != nil {
			return 0, err
		}
		reqCtx := ctx
		if reqCtx == nil {
			reqCtx = context.Background()
		}
		perUserCtx, cancel := context.WithTimeout(reqCtx, 30*time.Second)
		err = man.AddUser(perUserCtx, mUser)
		cancel()
		if err != nil && !isBenignAddUserError(err) {
			return added, err
		}
		added++
	}
	return added, nil
}

func (v *V2Core) UpdateUserIDs(tag string, users []panel.UserInfo) error {
	if v.users == nil {
		return fmt.Errorf("core is not ready")
	}
	v.users.mapLock.Lock()
	defer v.users.mapLock.Unlock()
	for i := range users {
		v.users.uidMap[format.UserTag(tag, users[i].Uuid)] = users[i].Id
	}
	return nil
}

func isBenignAddUserError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "already exists")
}

func isBenignRemoveUserError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found")
}

func buildVmessUsers(tag string, userInfo []panel.UserInfo) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i, user := range userInfo {
		users[i] = buildVmessUser(tag, &user)
	}
	return users
}

func buildVmessUser(tag string, userInfo *panel.UserInfo) (user *protocol.User) {
	vmessAccount := &conf.VMessAccount{
		ID:       userInfo.Uuid,
		Security: "auto",
	}
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(vmessAccount.Build()),
	}
}

func buildVlessUsers(tag string, userInfo []panel.UserInfo, flow string) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildVlessUser(tag, &(userInfo)[i], flow)
	}
	return users
}

func buildVlessUser(tag string, userInfo *panel.UserInfo, flow string) (user *protocol.User) {
	vlessAccount := &vless.Account{
		Id: userInfo.Uuid,
	}
	vlessAccount.Flow = flow
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(vlessAccount),
	}
}

func buildTrojanUsers(tag string, userInfo []panel.UserInfo) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildTrojanUser(tag, &(userInfo)[i])
	}
	return users
}

func buildTrojanUser(tag string, userInfo *panel.UserInfo) (user *protocol.User) {
	trojanAccount := &trojan.Account{
		Password: userInfo.Uuid,
	}
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(trojanAccount),
	}
}

func buildSSUsers(tag string, userInfo []panel.UserInfo, cypher string, serverKey string) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildSSUser(tag, &userInfo[i], cypher, serverKey)
	}
	return users
}

func buildSSUser(tag string, userInfo *panel.UserInfo, cypher string, serverKey string) (user *protocol.User) {
	if serverKey == "" {
		ssAccount := &shadowsocks.Account{
			Password:   userInfo.Uuid,
			CipherType: getCipherFromString(cypher),
		}
		return &protocol.User{
			Level:   0,
			Email:   format.UserTag(tag, userInfo.Uuid),
			Account: serial.ToTypedMessage(ssAccount),
		}
	} else {
		var keyLength int
		switch cypher {
		case "2022-blake3-aes-128-gcm":
			keyLength = 16
		case "2022-blake3-aes-256-gcm":
			keyLength = 32
		case "2022-blake3-chacha20-poly1305":
			keyLength = 32
		}
		ssAccount := &shadowsocks_2022.Account{
			Key: base64.StdEncoding.EncodeToString([]byte(userInfo.Uuid[:keyLength])),
		}
		return &protocol.User{
			Level:   0,
			Email:   format.UserTag(tag, userInfo.Uuid),
			Account: serial.ToTypedMessage(ssAccount),
		}
	}
}

func getCipherFromString(c string) shadowsocks.CipherType {
	switch strings.ToLower(c) {
	case "aes-128-gcm", "aead_aes_128_gcm":
		return shadowsocks.CipherType_AES_128_GCM
	case "aes-256-gcm", "aead_aes_256_gcm":
		return shadowsocks.CipherType_AES_256_GCM
	case "chacha20-poly1305", "aead_chacha20_poly1305", "chacha20-ietf-poly1305":
		return shadowsocks.CipherType_CHACHA20_POLY1305
	case "none", "plain":
		return shadowsocks.CipherType_NONE
	default:
		return shadowsocks.CipherType_UNKNOWN
	}
}

func buildHysteria2Users(tag string, userInfo []panel.UserInfo) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildHysteria2User(tag, &userInfo[i])
	}
	return users
}

func buildHysteria2User(tag string, userInfo *panel.UserInfo) (user *protocol.User) {
	hysteria2Account := &hysteria2.Account{
		Password: userInfo.Uuid,
	}
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(hysteria2Account),
	}
}

func buildTuicUsers(tag string, userInfo []panel.UserInfo) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildTuicUser(tag, &userInfo[i])
	}
	return users
}

func buildTuicUser(tag string, userInfo *panel.UserInfo) (user *protocol.User) {
	tuicAccount := &tuic.Account{
		Uuid:     userInfo.Uuid,
		Password: userInfo.Uuid,
	}
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(tuicAccount),
	}
}

func buildAnyTLSUsers(tag string, userInfo []panel.UserInfo) (users []*protocol.User) {
	users = make([]*protocol.User, len(userInfo))
	for i := range userInfo {
		users[i] = buildAnyTLSUser(tag, &userInfo[i])
	}
	return users
}

func buildAnyTLSUser(tag string, userInfo *panel.UserInfo) (user *protocol.User) {
	anyTLSAccount := &anytls.Account{
		Password: userInfo.Uuid,
	}
	return &protocol.User{
		Level:   0,
		Email:   format.UserTag(tag, userInfo.Uuid),
		Account: serial.ToTypedMessage(anyTLSAccount),
	}
}
