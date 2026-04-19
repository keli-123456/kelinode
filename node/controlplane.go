package node

import (
	"context"
	"errors"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

// ControlPlane defines the panel-facing operations used by node controllers.
// It lets us swap panel implementations without touching controller logic.
type ControlPlane interface {
	GetNodeInfo(ctx context.Context) (*panel.NodeInfo, error)
	GetUserList(ctx context.Context) ([]panel.UserInfo, error)
	GetUserDelta(ctx context.Context, since int64) (*panel.UserDeltaBody, error)
	GetUserAlive(ctx context.Context) (map[int]int, error)
	CachedAliveMap() map[int]int
	CachedAliveSnapshot() *panel.AliveMap
	ReportUserTraffic(ctx context.Context, userTraffic []panel.UserTraffic) error
	ReportNodeOnlineUsers(ctx context.Context, data *map[int][]string) error
}

// ControlPlaneRealtimeBootstrap exposes optional realtime bootstrap hints (e.g. ws_url from handshake).
type ControlPlaneRealtimeBootstrap interface {
	GetRealtimeBootstrap(ctx context.Context) (*RealtimeBootstrap, error)
}

// ControlPlaneUnifiedReporter exposes optional unified reporting capability.
type ControlPlaneUnifiedReporter interface {
	ReportSnapshot(ctx context.Context, userTraffic []panel.UserTraffic, online map[int][]string) error
}

type RealtimeBootstrap struct {
	Enabled bool
	URL     string
}

var ErrControlPlaneUnifiedReportUnsupported = errors.New("control plane unified report unsupported")

type ControlPlaneFactory interface {
	New(nodeConfig *conf.NodeConfig) (ControlPlane, error)
}

type panelControlPlaneFactory struct{}

func (panelControlPlaneFactory) New(nodeConfig *conf.NodeConfig) (ControlPlane, error) {
	client, err := panel.New(nodeConfig)
	if err != nil {
		return nil, err
	}
	return newPanelControlPlane(client, nodeConfig), nil
}

func defaultControlPlaneFactory() ControlPlaneFactory {
	return panelControlPlaneFactory{}
}
