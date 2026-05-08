package core

import (
	"fmt"
	"strings"

	panel "github.com/keli-123456/kelinode/api/v2board"
)

func IsExternalSidecarNodeType(nodeType string) bool {
	return nodeType == "mieru" || nodeType == "naive"
}

func (v *V2Core) AddNode(tag string, info *panel.NodeInfo) error {
	v.access.Lock()
	defer v.access.Unlock()

	if info != nil && IsExternalSidecarNodeType(info.Type) {
		if v.externalSidecarTags == nil {
			v.externalSidecarTags = make(map[string]struct{})
		}
		v.externalSidecarTags[tag] = struct{}{}
		return nil
	}

	if v.Server == nil || v.ihm == nil {
		return fmt.Errorf("core is not ready")
	}

	inBoundConfig, err := buildInbound(info, tag)
	if err != nil {
		return fmt.Errorf("build inbound error: %s", err)
	}
	err = v.addInbound(inBoundConfig)
	if err != nil {
		if shouldFallbackNodeListenIP(info.Common.ListenIP) {
			_ = v.removeInbound(tag)
			ipv4Config, buildErr := buildInboundWithListenIP(info, tag, "0.0.0.0")
			if buildErr != nil {
				return fmt.Errorf("build ipv4 fallback inbound error: %s", buildErr)
			}
			if fallbackErr := v.addInbound(ipv4Config); fallbackErr == nil {
				return nil
			} else {
				return fmt.Errorf("add inbound error: %s; ipv4 fallback error: %s", err, fallbackErr)
			}
		}
		return fmt.Errorf("add inbound error: %s", err)
	}
	return nil
}

func (v *V2Core) DelNode(tag string) error {
	v.access.Lock()
	defer v.access.Unlock()

	if _, ok := v.externalSidecarTags[tag]; ok {
		delete(v.externalSidecarTags, tag)
		v.deleteUserIDsForTag(tag)
		return nil
	}

	if v.ihm == nil {
		return fmt.Errorf("core is not ready")
	}

	err := v.removeInbound(tag)
	if err != nil {
		return fmt.Errorf("remove in error: %s", err)
	}
	return nil
}

func (v *V2Core) deleteUserIDsForTag(tag string) {
	if v.users == nil {
		return
	}
	prefix := tag + "|"
	v.users.mapLock.Lock()
	defer v.users.mapLock.Unlock()
	for key := range v.users.uidMap {
		if strings.HasPrefix(key, prefix) {
			delete(v.users.uidMap, key)
		}
	}
}
