package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

func TestResolveMachineNodeConfigsLoadsProfileNodes(t *testing.T) {
	restore := fakeMachineProfileClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != panel.PathV2MachineNodes {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if got := int(payload["machine_id"].(float64)); got != 3 {
			t.Fatalf("unexpected machine_id: %d", got)
		}
		if got := payload["token"].(string); got != "machine-token" {
			t.Fatalf("unexpected token: %s", got)
		}
		return jsonMachineResponse(http.StatusOK, map[string]any{
			"nodes": []map[string]any{
				{"id": 10, "type": "vless", "name": "node-a"},
				{"id": 11, "type": "trojan", "name": "node-b"},
			},
		}), nil
	})
	defer restore()

	cfg := &conf.Conf{
		MachineConfig: conf.MachineConfig{
			Enabled:         true,
			ContinueOnError: true,
			Profiles: []conf.MachineProfileConfig{
				{
					Name:      "site-a",
					APIHost:   "https://panel.example.com",
					Key:       "machine-token",
					MachineID: 3,
					Timeout:   1,
				},
			},
		},
	}

	if err := ResolveMachineNodeConfigs(context.Background(), cfg); err != nil {
		t.Fatalf("resolve machine nodes failed: %v", err)
	}

	if len(cfg.NodeConfigs) != 2 {
		t.Fatalf("unexpected node count: %d", len(cfg.NodeConfigs))
	}
	first := cfg.NodeConfigs[0]
	if first.APIHost != "https://panel.example.com" || first.Key != "machine-token" || first.MachineID != 3 || first.NodeID != 10 {
		t.Fatalf("unexpected first node config: %+v", first)
	}
	wantDir := filepath.Join(conf.DefaultNodeConfigDir, "site-a", "node-10")
	if first.ConfigDir != wantDir {
		t.Fatalf("unexpected config dir: got %s want %s", first.ConfigDir, wantDir)
	}
}

func TestResolveMachineNodeConfigsContinuesAfterProfileError(t *testing.T) {
	restore := fakeMachineProfileClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "fail.example.com" {
			return jsonMachineResponse(http.StatusUnauthorized, map[string]any{"message": "bad"}), nil
		}
		return jsonMachineResponse(http.StatusOK, map[string]any{
			"data": map[string]any{
				"nodes": []map[string]any{
					{"id": 21, "type": "vless"},
				},
			},
		}), nil
	})
	defer restore()

	cfg := &conf.Conf{
		MachineConfig: conf.MachineConfig{
			Enabled:         true,
			ContinueOnError: true,
			Profiles: []conf.MachineProfileConfig{
				{APIHost: "https://fail.example.com", Key: "bad", MachineID: 1},
				{Name: "ok", APIHost: "https://ok.example.com", Key: "ok", MachineID: 2, ConfigDir: "/srv/ok"},
			},
		},
	}

	if err := ResolveMachineNodeConfigs(context.Background(), cfg); err != nil {
		t.Fatalf("resolve machine nodes failed: %v", err)
	}

	if len(cfg.NodeConfigs) != 1 || cfg.NodeConfigs[0].NodeID != 21 {
		t.Fatalf("unexpected node configs: %+v", cfg.NodeConfigs)
	}
	if got, want := cfg.NodeConfigs[0].ConfigDir, "/srv/ok/node-21"; got != want {
		t.Fatalf("unexpected config dir: got %s want %s", got, want)
	}
}

type machineProfileRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn machineProfileRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func fakeMachineProfileClient(t *testing.T, fn machineProfileRoundTripFunc) func() {
	t.Helper()
	old := machineProfileHTTPClient
	machineProfileHTTPClient = func(time.Duration) *http.Client {
		return &http.Client{Transport: fn}
	}
	return func() {
		machineProfileHTTPClient = old
	}
}

func jsonMachineResponse(status int, payload any) *http.Response {
	data, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}
