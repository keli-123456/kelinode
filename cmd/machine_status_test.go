package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keli-123456/kelinode/agent/subproxy"
	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/node"
)

func TestReportMachineStatusIncludesVersion(t *testing.T) {
	previousVersion := version
	version = "v9.8.7"
	defer func() { version = previousVersion }()

	var gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Status map[string]any `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotVersion, _ = payload.Status["version"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": true})
	}))
	defer server.Close()

	_, err := reportMachineStatus(context.Background(), conf.MachineProfileConfig{
		APIHost:   server.URL,
		Key:       "machine-token",
		MachineID: 1,
		Timeout:   1,
	}, nil, nil, map[string]any{"cpu": 1})
	if err != nil {
		t.Fatalf("reportMachineStatus returned error: %v", err)
	}
	if gotVersion != "v9.8.7" {
		t.Fatalf("unexpected reported version: %q", gotVersion)
	}
}

func TestCurrentKelinodeVersionFallsBackToInstalledMarker(t *testing.T) {
	previousVersion := version
	previousPaths := installedVersionPaths
	defer func() {
		version = previousVersion
		installedVersionPaths = previousPaths
	}()

	version = "TempVersion"
	path := filepath.Join(t.TempDir(), ".installed_version")
	if err := os.WriteFile(path, []byte("v1.2.3\n"), 0644); err != nil {
		t.Fatalf("write version marker: %v", err)
	}
	installedVersionPaths = []string{path}

	if got := currentKelinodeVersion(); got != "v1.2.3" {
		t.Fatalf("unexpected current version: %q", got)
	}
}

func TestReportMachineStatusParsesReloadHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != panel.PathV2MachineStatus {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":   true,
			"reload": true,
		})
	}))
	defer server.Close()

	result, err := reportMachineStatus(context.Background(), conf.MachineProfileConfig{
		APIHost:   server.URL,
		Key:       "machine-token",
		MachineID: 1,
		Timeout:   1,
	}, func() subproxy.Status {
		return subproxy.Status{NeedCertificate: true}
	}, nil, map[string]any{"cpu": 1})
	if err != nil {
		t.Fatalf("reportMachineStatus returned error: %v", err)
	}
	if !result.Reload {
		t.Fatalf("expected reload hint")
	}
}

func TestReportMachineStatusParsesUpgradeCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": true,
			"upgrade": map[string]any{
				"id":             "upgrade-1",
				"target_version": "v1.2.3",
			},
		})
	}))
	defer server.Close()

	result, err := reportMachineStatus(context.Background(), conf.MachineProfileConfig{
		APIHost:   server.URL,
		Key:       "machine-token",
		MachineID: 1,
		Timeout:   1,
	}, nil, nil, map[string]any{"cpu": 1})
	if err != nil {
		t.Fatalf("reportMachineStatus returned error: %v", err)
	}
	if result.Upgrade == nil {
		t.Fatalf("expected upgrade command")
	}
	if got := result.Upgrade.ID; got != "upgrade-1" {
		t.Fatalf("unexpected upgrade id: %q", got)
	}
	if got := result.Upgrade.TargetVersion; got != "v1.2.3" {
		t.Fatalf("unexpected target version: %q", got)
	}
}

func TestRunMachineStatusReporterQueuesReloadHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":   true,
			"reload": true,
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reloadCh := make(chan struct{}, 1)
	runMachineStatusReporter(ctx, []conf.MachineProfileConfig{{
		APIHost:   server.URL,
		Key:       "machine-token",
		MachineID: 1,
		Timeout:   1,
	}}, func() subproxy.Status {
		return subproxy.Status{NeedCertificate: true}
	}, func() {
		queueReload(reloadCh)
		cancel()
	}, nil)

	select {
	case <-reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected queued reload")
	}
}

func TestBuildMachineNodeFailurePayloadFiltersCurrentProfile(t *testing.T) {
	errTestMachineStatus := errors.New("test failure")
	failures := []node.NodeFailure{
		{
			Config: conf.NodeConfig{APIHost: "https://panel.example.com", MachineID: 1, NodeID: 51},
			Err:    errTestMachineStatus,
		},
		{
			Config: conf.NodeConfig{APIHost: "https://other.example.com", MachineID: 1, NodeID: 52},
			Err:    errTestMachineStatus,
		},
	}

	payload := buildMachineNodeFailurePayload(conf.MachineProfileConfig{
		APIHost:   "https://panel.example.com/",
		MachineID: 1,
	}, failures)

	if len(payload) != 1 {
		t.Fatalf("unexpected failure payload: %+v", payload)
	}
	if got := payload[0]["node_id"]; got != 51 {
		t.Fatalf("unexpected node id: %+v", got)
	}
	if got := payload[0]["error"]; got != "test failure" {
		t.Fatalf("unexpected error: %+v", got)
	}
}
