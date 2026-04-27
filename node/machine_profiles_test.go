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
			"agent": map[string]any{
				"subscription_proxy": map[string]any{
					"enabled":            true,
					"site_id":            "panel-a",
					"upstream_base_url":  "https://panel.example.com",
					"subscribe_path":     "answer/land",
					"https_listen":       "0.0.0.0:8443",
					"cert_file":          "/tmp/cert.pem",
					"key_file":           "/tmp/key.pem",
					"max_response_bytes": 1048576,
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
	proxy := cfg.AgentConfig.SubscriptionProxy
	if !proxy.Enabled || proxy.HTTPSListen != "0.0.0.0:8443" || len(proxy.Profiles) != 1 {
		t.Fatalf("unexpected subscription proxy config: %+v", proxy)
	}
	if got := proxy.Profiles[0]; got.SiteID != "panel-a" || got.UpstreamBaseURL != "https://panel.example.com" || got.SubscribePath != "answer/land" {
		t.Fatalf("unexpected subscription proxy profile: %+v", got)
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

func TestResolveMachineNodeConfigsMergesSubscriptionProxyProfiles(t *testing.T) {
	restore := fakeMachineProfileClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonMachineResponse(http.StatusOK, map[string]any{
			"nodes": []map[string]any{
				{"id": 31, "type": "vless"},
			},
			"agent": map[string]any{
				"subscription_proxy": map[string]any{
					"enabled":            true,
					"site_id":            "site-one",
					"upstream_base_url":  "https://site-one.example.com",
					"subscribe_path":     "s",
					"https_listen":       "0.0.0.0:443",
					"http_listen":        "0.0.0.0:80",
					"cert_file":          "/etc/v2node/subproxy/cert.pem",
					"key_file":           "/etc/v2node/subproxy/key.pem",
					"max_response_bytes": 10485760,
				},
			},
		}), nil
	})
	defer restore()

	cfg := &conf.Conf{
		MachineConfig: conf.MachineConfig{
			Enabled: true,
			Profiles: []conf.MachineProfileConfig{
				{APIHost: "https://site-one.example.com", Key: "machine-token", MachineID: 3},
			},
		},
	}

	if err := ResolveMachineNodeConfigs(context.Background(), cfg); err != nil {
		t.Fatalf("resolve machine nodes failed: %v", err)
	}

	proxy := cfg.AgentConfig.SubscriptionProxy
	if !proxy.Enabled {
		t.Fatalf("expected subscription proxy to be enabled")
	}
	if proxy.HTTPSListen != "0.0.0.0:443" || proxy.CertFile != "/etc/v2node/subproxy/cert.pem" {
		t.Fatalf("unexpected subscription proxy listener config: %+v", proxy)
	}
	if len(proxy.Profiles) != 1 {
		t.Fatalf("unexpected subscription proxy profiles: %+v", proxy.Profiles)
	}
	if got := proxy.Profiles[0]; got.SiteID != "site-one" || got.UpstreamBaseURL != "https://site-one.example.com" || got.SubscribePath != "s" {
		t.Fatalf("unexpected subscription proxy profile: %+v", got)
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
