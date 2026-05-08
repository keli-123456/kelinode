package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientHealth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok","version":"0.1.0","uptime_seconds":3}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("health failed: %v", err)
	}
	if health.Status != "ok" || health.Version != "0.1.0" || health.UptimeSeconds != 3 {
		t.Fatalf("unexpected health: %+v", health)
	}
}

func TestClientRecordTraffic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		if r.Form.Get("user") != "node:user" || r.Form.Get("upload") != "10" || r.Form.Get("download") != "20" {
			t.Fatalf("unexpected form: %+v", r.Form)
		}
		_, _ = w.Write([]byte(`{"recorded":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	err := client.RecordTraffic(context.Background(), TrafficRecord{
		User:          "node:user",
		UploadBytes:   10,
		DownloadBytes: 20,
	})
	if err != nil {
		t.Fatalf("record traffic failed: %v", err)
	}
}

func TestClientDrainTraffic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic/drain" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"upload_bytes":10,"download_bytes":20,"users":[{"user":"node:user","upload_bytes":10,"download_bytes":20}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	snapshot, err := client.DrainTraffic(context.Background())
	if err != nil {
		t.Fatalf("drain traffic failed: %v", err)
	}
	if snapshot.UploadBytes != 10 || snapshot.DownloadBytes != 20 || len(snapshot.Users) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Users[0].User != "node:user" || snapshot.Users[0].UploadBytes != 10 || snapshot.Users[0].DownloadBytes != 20 {
		t.Fatalf("unexpected user traffic: %+v", snapshot.Users[0])
	}
}

func TestClientUpsertSidecar(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sidecars/upsert" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		if r.Form.Get("name") != "mieru-mita" ||
			r.Form.Get("protocol") != "mieru" ||
			r.Form.Get("enabled") != "true" {
			t.Fatalf("unexpected identity form: %+v", r.Form)
		}
		if r.Form.Get("binary") != "mita" || r.Form.Get("args") != "run\n--config\nruntime/mieru.json" {
			t.Fatalf("unexpected process form: %+v", r.Form)
		}
		if r.Form.Get("env") != "MITA_CONFIG_JSON_FILE=runtime/mieru.json" {
			t.Fatalf("unexpected env form: %+v", r.Form)
		}
		if r.Form.Get("file_path") != "runtime/mieru.json" || r.Form.Get("file_contents") != "{}" {
			t.Fatalf("unexpected generated file form: %+v", r.Form)
		}
		_, _ = w.Write([]byte(`{"started":["mieru-mita"],"stopped":[],"failed":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	report, err := client.UpsertSidecar(context.Background(), SidecarSpec{
		Name:     "mieru-mita",
		Protocol: "mieru",
		Enabled:  true,
		Binary:   "mita",
		Args:     []string{"run", "--config", "runtime/mieru.json"},
		Env: map[string]string{
			"MITA_CONFIG_JSON_FILE": "runtime/mieru.json",
		},
		GeneratedFiles: []GeneratedFile{{Path: "runtime/mieru.json", Contents: "{}"}},
	})
	if err != nil {
		t.Fatalf("upsert sidecar failed: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0] != "mieru-mita" || len(report.Failed) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestClientReturnsStatusErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "edge offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatalf("expected status error")
	}
}
