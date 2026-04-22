package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

const (
	featureUnknown int32 = iota
	featureSupported
	featureUnsupported
)

type panelControlPlane struct {
	client *panel.Client

	apiHost string
	token   string
	nodeID  int

	httpClient *http.Client

	handshakeState int32
	reportState    int32

	bootstrapMu sync.RWMutex
	bootstrap   *RealtimeBootstrap
}

func newPanelControlPlane(client *panel.Client, nodeConfig *conf.NodeConfig) *panelControlPlane {
	p := &panelControlPlane{
		client:     client,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	if nodeConfig != nil {
		p.apiHost = strings.TrimSpace(nodeConfig.APIHost)
		p.token = strings.TrimSpace(nodeConfig.Key)
		p.nodeID = nodeConfig.NodeID
	}
	if client != nil {
		if p.apiHost == "" {
			p.apiHost = strings.TrimSpace(client.APIHost)
		}
		if p.token == "" {
			p.token = strings.TrimSpace(client.Token)
		}
		if p.nodeID == 0 {
			p.nodeID = client.NodeId
		}
	}

	return p
}

func (p *panelControlPlane) GetNodeInfo(ctx context.Context) (*panel.NodeInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("panel client not initialized")
	}
	return p.client.GetNodeInfo(ctx)
}

func (p *panelControlPlane) GetUserList(ctx context.Context) ([]panel.UserInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("panel client not initialized")
	}
	return p.client.GetUserList(ctx)
}

func (p *panelControlPlane) GetUserDelta(ctx context.Context, since int64) (*panel.UserDeltaBody, error) {
	if p.client == nil {
		return nil, fmt.Errorf("panel client not initialized")
	}
	return p.client.GetUserDelta(ctx, since)
}

func (p *panelControlPlane) GetUserAlive(ctx context.Context) (map[int]int, error) {
	if p.client == nil {
		return nil, fmt.Errorf("panel client not initialized")
	}
	return p.client.GetUserAlive(ctx)
}

func (p *panelControlPlane) CachedAliveMap() map[int]int {
	if p.client == nil {
		return map[int]int{}
	}
	return p.client.CachedAliveMap()
}

func (p *panelControlPlane) CachedAliveSnapshot() *panel.AliveMap {
	if p.client == nil {
		return &panel.AliveMap{
			Alive:    map[int]int{},
			AliveIPs: map[int][]string{},
		}
	}
	return p.client.CachedAliveSnapshot()
}

func (p *panelControlPlane) ReportUserTraffic(ctx context.Context, userTraffic []panel.UserTraffic) error {
	if p.client == nil {
		return fmt.Errorf("panel client not initialized")
	}
	return p.client.ReportUserTraffic(ctx, userTraffic)
}

func (p *panelControlPlane) ReportNodeOnlineUsers(ctx context.Context, data *map[int][]string) error {
	if p.client == nil {
		return fmt.Errorf("panel client not initialized")
	}
	return p.client.ReportNodeOnlineUsers(ctx, data)
}

func (p *panelControlPlane) GetRealtimeBootstrap(ctx context.Context) (*RealtimeBootstrap, error) {
	state := atomic.LoadInt32(&p.handshakeState)
	if state == featureUnsupported {
		return nil, nil
	}
	if state == featureSupported {
		p.bootstrapMu.RLock()
		cached := p.bootstrap
		p.bootstrapMu.RUnlock()
		if cached != nil {
			cp := *cached
			return &cp, nil
		}
	}

	resp, err := p.doJSONRequest(ctx, http.MethodPost, "/api/v2/server/handshake", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		atomic.StoreInt32(&p.handshakeState, featureUnsupported)
		return nil, nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("handshake failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var payload struct {
		Websocket struct {
			Enabled bool   `json:"enabled"`
			WSURL   string `json:"ws_url"`
		} `json:"websocket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode handshake response failed: %w", err)
	}

	bootstrap := &RealtimeBootstrap{
		Enabled: payload.Websocket.Enabled,
		URL:     strings.TrimSpace(payload.Websocket.WSURL),
	}
	atomic.StoreInt32(&p.handshakeState, featureSupported)
	p.bootstrapMu.Lock()
	p.bootstrap = bootstrap
	p.bootstrapMu.Unlock()
	return bootstrap, nil
}

func (p *panelControlPlane) ReportSnapshot(ctx context.Context, userTraffic []panel.UserTraffic, online map[int][]string) error {
	state := atomic.LoadInt32(&p.reportState)
	if state == featureUnsupported {
		return ErrControlPlaneUnifiedReportUnsupported
	}

	payload := make(map[string]any)
	if len(userTraffic) > 0 {
		traffic := make(map[string][2]int64, len(userTraffic))
		for _, t := range userTraffic {
			traffic[strconv.Itoa(t.UID)] = [2]int64{t.Upload, t.Download}
		}
		payload["traffic"] = traffic
	}
	if len(online) > 0 {
		alive := make(map[string][]string, len(online))
		onlineCount := make(map[string]int, len(online))
		for uid, ips := range online {
			k := strconv.Itoa(uid)
			alive[k] = ips
			onlineCount[k] = len(ips)
		}
		payload["alive"] = alive
		payload["online"] = onlineCount
	}

	resp, err := p.doJSONRequest(ctx, http.MethodPost, "/api/v2/server/report", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		atomic.StoreInt32(&p.reportState, featureUnsupported)
		return ErrControlPlaneUnifiedReportUnsupported
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("report snapshot failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	atomic.StoreInt32(&p.reportState, featureSupported)
	return nil
}

func (p *panelControlPlane) doJSONRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if p.apiHost == "" || p.token == "" || p.nodeID <= 0 {
		return nil, fmt.Errorf("panel auth config invalid")
	}

	endpoint, err := p.buildEndpoint(path)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body failed: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return p.httpClient.Do(req)
}

func (p *panelControlPlane) buildEndpoint(path string) (string, error) {
	u, err := url.Parse(strings.TrimRight(p.apiHost, "/") + path)
	if err != nil {
		return "", fmt.Errorf("parse endpoint failed: %w", err)
	}
	q := u.Query()
	q.Set("token", p.token)
	q.Set("node_id", strconv.Itoa(p.nodeID))
	q.Set("node_type", "v2node")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
