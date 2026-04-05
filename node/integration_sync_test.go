package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	v2core "github.com/keli-123456/kelinode/core"
	"github.com/keli-123456/kelinode/limiter"
)

func TestExecuteNodeConfigCheckSignalsReloadOnConfigChange(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.RWMutex
		serverPort = 443
	)

	client := newTestPanelClient(t, 32, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v2/server/config" {
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}

		mu.RLock()
		currentPort := serverPort
		mu.RUnlock()

		return jsonResponse(t, req, http.StatusOK, map[string]any{
			"protocol":    "hysteria2",
			"listen_ip":   "0.0.0.0",
			"server_port": currentPort,
			"tls":         1,
			"tls_settings": map[string]any{
				"server_name": "node.example.com",
				"cert_mode":   "none",
			},
			"base_config": map[string]any{
				"push_interval": 60,
				"pull_interval": 60,
			},
		}), nil
	}))
	if _, err := client.GetNodeInfo(context.Background()); err != nil {
		t.Fatalf("warm get node info failed: %v", err)
	}

	controller := &Controller{
		apiClient: client,
		server:    &v2core.V2Core{ReloadCh: make(chan struct{}, 1)},
	}

	mu.Lock()
	serverPort = 8443
	mu.Unlock()

	changed, err := controller.executeNodeConfigCheck(context.Background())
	if err != nil {
		t.Fatalf("execute node config check failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed to be true")
	}

	select {
	case <-controller.server.ReloadCh:
	default:
		t.Fatalf("expected reload signal to be queued")
	}
}

func TestExecuteNodeConfigCheckReturnsNoChange(t *testing.T) {
	t.Parallel()

	client := newTestPanelClient(t, 33, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v2/server/config" {
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}

		return jsonResponse(t, req, http.StatusOK, map[string]any{
			"protocol":    "hysteria2",
			"listen_ip":   "0.0.0.0",
			"server_port": 443,
			"tls":         1,
			"tls_settings": map[string]any{
				"server_name": "node.example.com",
				"cert_mode":   "none",
			},
			"base_config": map[string]any{
				"push_interval": 60,
				"pull_interval": 60,
			},
		}), nil
	}))
	if _, err := client.GetNodeInfo(context.Background()); err != nil {
		t.Fatalf("warm get node info failed: %v", err)
	}

	controller := &Controller{
		apiClient: client,
		server:    &v2core.V2Core{ReloadCh: make(chan struct{}, 1)},
	}

	changed, err := controller.executeNodeConfigCheck(context.Background())
	if err != nil {
		t.Fatalf("execute node config check failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed to be false")
	}

	select {
	case <-controller.server.ReloadCh:
		t.Fatalf("did not expect reload signal")
	default:
	}
}

func TestLoadAndSyncUsersAppliesDeltaFromWarmState(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	nodeConf := &conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    7,
		Key:       "test-token",
		Timeout:   5,
		ConfigDir: configDir,
	}
	client := newTestPanelClient(t, 7, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/server/UniProxy/user_delta":
			if got, want := req.URL.Query().Get("since"), "10"; got != want {
				t.Fatalf("unexpected delta since query: got %q want %q", got, want)
			}
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"full":     false,
				"revision": 11,
				"deleted": []map[string]any{
					{"id": 2, "uuid": "user-delete", "speed_limit": 0, "device_limit": 0},
				},
				"upsert": []map[string]any{
					{"id": 3, "uuid": "user-update", "speed_limit": 20, "device_limit": 2},
					{"id": 4, "uuid": "user-add", "speed_limit": 30, "device_limit": 3},
				},
			}), nil
		default:
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}
	}))
	controller := NewController(client, nodeConf, nil, conf.RealtimeConfig{})

	if err := saveUserSyncState(controller.userSyncStatePath, &userSyncStateFile{
		Revision: 10,
		Users: []panel.UserInfo{
			{Id: 1, Uuid: "user-keep", SpeedLimit: 10, DeviceLimit: 1},
			{Id: 2, Uuid: "user-delete", SpeedLimit: 10, DeviceLimit: 1},
			{Id: 3, Uuid: "user-update", SpeedLimit: 10, DeviceLimit: 1},
		},
	}); err != nil {
		t.Fatalf("save warm user sync state failed: %v", err)
	}

	users, err := controller.loadAndSyncUsers(context.Background())
	if err != nil {
		t.Fatalf("load and sync users failed: %v", err)
	}

	if got, want := controller.userRevision, int64(11); got != want {
		t.Fatalf("unexpected user revision: got %d want %d", got, want)
	}
	assertUserUUIDs(t, users, []string{"user-add", "user-keep", "user-update"})

	state, err := loadUserSyncState(controller.userSyncStatePath)
	if err != nil {
		t.Fatalf("reload user sync state failed: %v", err)
	}
	if got, want := state.Revision, int64(11); got != want {
		t.Fatalf("unexpected persisted revision: got %d want %d", got, want)
	}
	assertUserUUIDs(t, state.Users, []string{"user-add", "user-keep", "user-update"})
}

func TestLoadAndSyncUsersFallsBackToFullListWhenDeltaUnsupported(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	nodeConf := &conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    8,
		Key:       "test-token",
		Timeout:   5,
		ConfigDir: configDir,
	}
	client := newTestPanelClient(t, 8, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/server/UniProxy/user_delta":
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		case "/api/v1/server/UniProxy/user":
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"users": []map[string]any{
					{"id": 1, "uuid": "user-a", "speed_limit": 10, "device_limit": 1},
					{"id": 2, "uuid": "user-b", "speed_limit": 20, "device_limit": 2},
				},
			}), nil
		default:
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}
	}))
	controller := NewController(client, nodeConf, nil, conf.RealtimeConfig{})

	users, err := controller.loadAndSyncUsers(context.Background())
	if err != nil {
		t.Fatalf("load and sync users failed: %v", err)
	}

	if controller.userDeltaSupported {
		t.Fatalf("expected user delta support to be disabled after 404 fallback")
	}
	assertUserUUIDs(t, users, []string{"user-a", "user-b"})
}

func TestExecuteNodeUserSyncAppliesDeltaSideEffects(t *testing.T) {
	t.Parallel()

	nodeConf := &conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    9,
		Key:       "test-token",
		Timeout:   5,
		ConfigDir: t.TempDir(),
	}
	client := newTestPanelClient(t, 9, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/server/UniProxy/user_delta":
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"full":     false,
				"revision": 11,
				"deleted": []map[string]any{
					{"id": 2, "uuid": "user-delete", "speed_limit": 10, "device_limit": 1},
				},
				"upsert": []map[string]any{
					{"id": 3, "uuid": "user-update", "speed_limit": 20, "device_limit": 2},
					{"id": 4, "uuid": "user-add", "speed_limit": 30, "device_limit": 3},
				},
			}), nil
		case "/api/v1/server/UniProxy/alivelist":
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"alive": map[string]int{
					"1": 1,
					"3": 2,
					"4": 1,
				},
			}), nil
		default:
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}
	}))

	limiter.Init()
	initialUsers := []panel.UserInfo{
		{Id: 1, Uuid: "user-keep", SpeedLimit: 10, DeviceLimit: 1},
		{Id: 2, Uuid: "user-delete", SpeedLimit: 10, DeviceLimit: 1},
		{Id: 3, Uuid: "user-update", SpeedLimit: 10, DeviceLimit: 1},
	}
	l := limiter.AddLimiter("hysteria2", "test-tag", initialUsers, map[int]int{1: 1, 2: 1, 3: 1})
	t.Cleanup(func() { limiter.DeleteLimiter("test-tag") })

	var (
		gotUpdated []panel.UserInfo
		gotDeleted []panel.UserInfo
		gotAdded   []panel.UserInfo
	)

	controller := NewController(client, nodeConf, &panel.NodeInfo{
		Type: "hysteria2",
		Common: &panel.CommonNode{
			BaseConfig: &panel.BaseConfig{},
		},
	}, conf.RealtimeConfig{})
	controller.tag = "test-tag"
	controller.limiter = l
	controller.userList = append([]panel.UserInfo(nil), initialUsers...)
	controller.userRevision = 10
	controller.updateUserIDsFn = func(tag string, updated []panel.UserInfo) error {
		gotUpdated = append([]panel.UserInfo(nil), updated...)
		return nil
	}
	controller.delUsersFn = func(ctx context.Context, deleted []panel.UserInfo, tag string) error {
		gotDeleted = append([]panel.UserInfo(nil), deleted...)
		return nil
	}
	controller.addUsersFn = func(ctx context.Context, params *v2core.AddUsersParams) (int, error) {
		gotAdded = append([]panel.UserInfo(nil), params.Users...)
		return len(params.Users), nil
	}

	summary, err := controller.executeNodeUserSync(context.Background())
	if err != nil {
		t.Fatalf("execute node user sync failed: %v", err)
	}

	if got, want := summary, (userSyncSummary{Deleted: 1, Added: 1, Updated: 1}); got != want {
		t.Fatalf("unexpected user sync summary: got %+v want %+v", got, want)
	}
	if got, want := controller.userRevision, int64(11); got != want {
		t.Fatalf("unexpected user revision: got %d want %d", got, want)
	}
	assertUserUUIDs(t, controller.userList, []string{"user-add", "user-keep", "user-update"})
	assertUserUUIDs(t, gotUpdated, []string{"user-update"})
	assertUserUUIDs(t, gotDeleted, []string{"user-delete"})
	assertUserUUIDs(t, gotAdded, []string{"user-add"})

	if got := controller.limiter.UUIDtoUID["user-add"]; got != 4 {
		t.Fatalf("expected limiter to include added user id 4, got %d", got)
	}
	if got := controller.limiter.UUIDtoUID["user-update"]; got != 3 {
		t.Fatalf("expected limiter to keep updated user id 3, got %d", got)
	}
	if _, ok := controller.limiter.UUIDtoUID["user-delete"]; ok {
		t.Fatalf("expected deleted user to be removed from limiter")
	}
	value, ok := controller.limiter.UserLimitInfo.Load("test-tag|user-update")
	if !ok {
		t.Fatalf("expected limiter user info for updated user")
	}
	info := value.(*limiter.UserLimitInfo)
	if got, want := info.SpeedLimit, 20; got != want {
		t.Fatalf("unexpected updated speed limit: got %d want %d", got, want)
	}
	if got, want := info.DeviceLimit, 2; got != want {
		t.Fatalf("unexpected updated device limit: got %d want %d", got, want)
	}
}

func TestExecuteNodeUserSyncFallsBackToFullList(t *testing.T) {
	t.Parallel()

	nodeConf := &conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    10,
		Key:       "test-token",
		Timeout:   5,
		ConfigDir: t.TempDir(),
	}
	client := newTestPanelClient(t, 10, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/server/UniProxy/user_delta":
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		case "/api/v1/server/UniProxy/user":
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"users": []map[string]any{
					{"id": 1, "uuid": "user-keep", "speed_limit": 10, "device_limit": 1},
					{"id": 3, "uuid": "user-update", "speed_limit": 50, "device_limit": 5},
					{"id": 4, "uuid": "user-add", "speed_limit": 40, "device_limit": 4},
				},
			}), nil
		case "/api/v1/server/UniProxy/alivelist":
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"alive": map[string]int{
					"1": 1,
					"3": 1,
					"4": 1,
				},
			}), nil
		default:
			return jsonResponse(t, req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}
	}))

	limiter.Init()
	initialUsers := []panel.UserInfo{
		{Id: 1, Uuid: "user-keep", SpeedLimit: 10, DeviceLimit: 1},
		{Id: 2, Uuid: "user-delete", SpeedLimit: 10, DeviceLimit: 1},
		{Id: 3, Uuid: "user-update", SpeedLimit: 10, DeviceLimit: 1},
	}
	l := limiter.AddLimiter("hysteria2", "fallback-tag", initialUsers, map[int]int{1: 1, 2: 1, 3: 1})
	t.Cleanup(func() { limiter.DeleteLimiter("fallback-tag") })

	var (
		gotUpdated []panel.UserInfo
		gotDeleted []panel.UserInfo
		gotAdded   []panel.UserInfo
	)

	controller := NewController(client, nodeConf, &panel.NodeInfo{
		Type: "hysteria2",
		Common: &panel.CommonNode{
			BaseConfig: &panel.BaseConfig{},
		},
	}, conf.RealtimeConfig{})
	controller.tag = "fallback-tag"
	controller.limiter = l
	controller.userList = append([]panel.UserInfo(nil), initialUsers...)
	controller.userDeltaSupported = true
	controller.updateUserIDsFn = func(tag string, updated []panel.UserInfo) error {
		gotUpdated = append([]panel.UserInfo(nil), updated...)
		return nil
	}
	controller.delUsersFn = func(ctx context.Context, deleted []panel.UserInfo, tag string) error {
		gotDeleted = append([]panel.UserInfo(nil), deleted...)
		return nil
	}
	controller.addUsersFn = func(ctx context.Context, params *v2core.AddUsersParams) (int, error) {
		gotAdded = append([]panel.UserInfo(nil), params.Users...)
		return len(params.Users), nil
	}

	summary, err := controller.executeNodeUserSync(context.Background())
	if err != nil {
		t.Fatalf("execute node user sync failed: %v", err)
	}

	if controller.userDeltaSupported {
		t.Fatalf("expected user delta support to be disabled after fallback")
	}
	if got, want := summary, (userSyncSummary{Deleted: 1, Added: 1, Updated: 1}); got != want {
		t.Fatalf("unexpected user sync summary: got %+v want %+v", got, want)
	}
	assertUserUUIDs(t, controller.userList, []string{"user-add", "user-keep", "user-update"})
	assertUserUUIDs(t, gotUpdated, []string{"user-update"})
	assertUserUUIDs(t, gotDeleted, []string{"user-delete"})
	assertUserUUIDs(t, gotAdded, []string{"user-add"})
}

func TestUserSyncStatePathUsesConfigDir(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "node-1")
	got := userSyncStatePath(configDir, "https://panel.example.com", 9)
	if filepath.Dir(got) != configDir {
		t.Fatalf("unexpected state dir: got %q want %q", filepath.Dir(got), configDir)
	}
	if filepath.Ext(got) != ".json" {
		t.Fatalf("unexpected state path extension: %q", filepath.Ext(got))
	}
}

func newTestPanelClient(t *testing.T, nodeID int, transport http.RoundTripper) *panel.Client {
	t.Helper()

	client, err := panel.New(&conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    nodeID,
		Key:       "test-token",
		Timeout:   5,
		ConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new panel client failed: %v", err)
	}
	client.SetTransport(transport)
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(t *testing.T, req *http.Request, status int, payload any) *http.Response {
	t.Helper()

	var body io.ReadCloser
	if payload == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	} else {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal response failed: %v", err)
		}
		body = io.NopCloser(bytes.NewReader(data))
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    body,
		Request: req,
	}
}

func assertUserUUIDs(t *testing.T, users []panel.UserInfo, want []string) {
	t.Helper()

	got := make([]string, 0, len(users))
	for _, user := range users {
		got = append(got, user.Uuid)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("unexpected user count: got %d want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("unexpected user uuids: got %v want %v", got, want)
		}
	}
}
