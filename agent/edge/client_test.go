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
