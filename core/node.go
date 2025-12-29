package core

import (
	"fmt"

	panel "github.com/wyx2685/v2node/api/v2board"
)

func (v *V2Core) AddNode(tag string, info *panel.NodeInfo) error {
	v.access.Lock()
	defer v.access.Unlock()

	if v.Server == nil || v.ihm == nil {
		return fmt.Errorf("core is not ready")
	}

	inBoundConfig, err := buildInbound(info, tag)
	if err != nil {
		return fmt.Errorf("build inbound error: %s", err)
	}
	err = v.addInbound(inBoundConfig)
	if err != nil {
		return fmt.Errorf("add inbound error: %s", err)
	}
	return nil
}

func (v *V2Core) DelNode(tag string) error {
	v.access.Lock()
	defer v.access.Unlock()

	if v.ihm == nil {
		return fmt.Errorf("core is not ready")
	}

	err := v.removeInbound(tag)
	if err != nil {
		return fmt.Errorf("remove in error: %s", err)
	}
	return nil
}
