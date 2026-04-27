package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/keli-123456/kelinode/agent/subproxy"
	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

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
	}, map[string]any{"cpu": 1})
	if err != nil {
		t.Fatalf("reportMachineStatus returned error: %v", err)
	}
	if !result.Reload {
		t.Fatalf("expected reload hint")
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
	})

	select {
	case <-reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected queued reload")
	}
}
