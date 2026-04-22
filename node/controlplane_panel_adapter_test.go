package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

func TestPanelControlPlane_GetRealtimeBootstrapFallbackWhenUnsupported(t *testing.T) {
	t.Parallel()

	cp := newPanelControlPlane(nil, &conf.NodeConfig{
		APIHost: "https://panel.example.com",
		NodeID:  7,
		Key:     "token",
	})
	cp.httpClient = &http.Client{
		Transport: cpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v2/server/handshake" {
				return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
			}
			return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}),
	}

	got, err := cp.GetRealtimeBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetRealtimeBootstrap returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil bootstrap on unsupported endpoint, got %+v", got)
	}
}

func TestPanelControlPlane_GetRealtimeBootstrapFromHandshake(t *testing.T) {
	t.Parallel()

	cp := newPanelControlPlane(nil, &conf.NodeConfig{
		APIHost: "https://panel.example.com",
		NodeID:  7,
		Key:     "token",
	})
	cp.httpClient = &http.Client{
		Transport: cpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v2/server/handshake" {
				return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
			}
			if got, want := req.URL.Query().Get("token"), "token"; got != want {
				t.Fatalf("unexpected token query: got %q want %q", got, want)
			}
			if got, want := req.URL.Query().Get("node_id"), "7"; got != want {
				t.Fatalf("unexpected node_id query: got %q want %q", got, want)
			}
			return jsonHTTPResponse(req, http.StatusOK, map[string]any{
				"websocket": map[string]any{
					"enabled": true,
					"ws_url":  "wss://panel.example/ws",
				},
			}), nil
		}),
	}

	got, err := cp.GetRealtimeBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetRealtimeBootstrap returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil bootstrap")
	}
	if got.URL != "wss://panel.example/ws" || !got.Enabled {
		t.Fatalf("unexpected bootstrap: %+v", got)
	}
}

func TestPanelControlPlane_ReportSnapshotFallbackWhenUnsupported(t *testing.T) {
	t.Parallel()

	cp := newPanelControlPlane(nil, &conf.NodeConfig{
		APIHost: "https://panel.example.com",
		NodeID:  9,
		Key:     "token",
	})
	cp.httpClient = &http.Client{
		Transport: cpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v2/server/report" {
				return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
			}
			return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
		}),
	}

	err := cp.ReportSnapshot(
		context.Background(),
		[]panel.UserTraffic{{UID: 1, Upload: 10, Download: 20}},
		map[int][]string{1: []string{"1.1.1.1"}},
	)
	if err == nil {
		t.Fatal("expected unsupported error")
	}
	if err != ErrControlPlaneUnifiedReportUnsupported {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanelControlPlane_ReportSnapshotPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	cp := newPanelControlPlane(nil, &conf.NodeConfig{
		APIHost: "https://panel.example.com",
		NodeID:  9,
		Key:     "token",
	})
	cp.httpClient = &http.Client{
		Transport: cpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v2/server/report" {
				return jsonHTTPResponse(req, http.StatusNotFound, map[string]any{"message": "not found"}), nil
			}
			if got, want := req.URL.Query().Get("token"), "token"; got != want {
				t.Fatalf("unexpected token query: got %q want %q", got, want)
			}
			if got, want := req.URL.Query().Get("node_id"), "9"; got != want {
				t.Fatalf("unexpected node_id query: got %q want %q", got, want)
			}
			if err := json.NewDecoder(req.Body).Decode(&captured); err != nil {
				t.Fatalf("decode payload failed: %v", err)
			}
			return jsonHTTPResponse(req, http.StatusOK, map[string]any{"data": true}), nil
		}),
	}

	err := cp.ReportSnapshot(
		context.Background(),
		[]panel.UserTraffic{{UID: 2, Upload: 11, Download: 22}},
		map[int][]string{2: []string{"8.8.8.8", "8.8.4.4"}},
	)
	if err != nil {
		t.Fatalf("ReportSnapshot returned error: %v", err)
	}

	trafficRaw, ok := captured["traffic"].(map[string]any)
	if !ok {
		t.Fatalf("missing traffic field: %#v", captured)
	}
	if _, ok := trafficRaw["2"]; !ok {
		t.Fatalf("missing traffic uid in payload: %#v", trafficRaw)
	}
	aliveRaw, ok := captured["alive"].(map[string]any)
	if !ok {
		t.Fatalf("missing alive field: %#v", captured)
	}
	if _, ok := aliveRaw["2"]; !ok {
		t.Fatalf("missing alive uid in payload: %#v", aliveRaw)
	}
	onlineRaw, ok := captured["online"].(map[string]any)
	if !ok {
		t.Fatalf("missing online field: %#v", captured)
	}
	if got, ok := onlineRaw["2"].(float64); !ok || got != 2 {
		t.Fatalf("unexpected online count payload: %#v", onlineRaw["2"])
	}
}

type cpRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn cpRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonHTTPResponse(req *http.Request, status int, payload any) *http.Response {
	var body io.ReadCloser
	if payload == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	} else {
		data, _ := json.Marshal(payload)
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
