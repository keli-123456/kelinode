package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/keli-123456/kelinode/conf"
)

func TestGetUserAlivePreservesCachedSnapshotOnFailure(t *testing.T) {
	t.Parallel()

	var calls int
	client := newTestPanelClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return jsonResponse(t, req, http.StatusOK, map[string]any{
				"alive": map[string]int{
					"1": 2,
				},
				"alive_ips": map[string][]string{
					"1": {"1.1.1.1", "2.2.2.2"},
				},
				"mode": 1,
			}), nil
		default:
			return jsonResponse(t, req, http.StatusInternalServerError, map[string]any{
				"message": "panel error",
			}), nil
		}
	}))

	alive, err := client.GetUserAlive(context.Background())
	if err != nil {
		t.Fatalf("first get user alive failed: %v", err)
	}
	if got := alive[1]; got != 2 {
		t.Fatalf("unexpected alive count: got %d want 2", got)
	}

	alive, err = client.GetUserAlive(context.Background())
	if err == nil {
		t.Fatal("expected second get user alive to fail")
	}
	if alive != nil {
		t.Fatalf("expected nil alive map on failure, got %#v", alive)
	}

	cached := client.CachedAliveMap()
	if got := cached[1]; got != 2 {
		t.Fatalf("expected cached alive snapshot to be preserved, got %d want 2", got)
	}

	snapshot := client.CachedAliveSnapshot()
	if got := snapshot.Mode; got != 1 {
		t.Fatalf("unexpected cached mode: got %d want 1", got)
	}
	if got := len(snapshot.AliveIPs[1]); got != 2 {
		t.Fatalf("unexpected cached alive ips count: got %d want 2", got)
	}
}

func TestGetUserDeltaTreatsForbiddenAsUnsupported(t *testing.T) {
	t.Parallel()

	client := newTestPanelClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != PathV1UniProxyUserDelta {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return jsonResponse(t, req, http.StatusForbidden, map[string]any{
			"message": "forbidden by edge rule",
		}), nil
	}))

	_, err := client.GetUserDelta(context.Background(), 0)
	if !errors.Is(err, ErrUserDeltaNotSupported) {
		t.Fatalf("expected ErrUserDeltaNotSupported, got %v", err)
	}
}

func newTestPanelClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()

	client, err := New(&conf.NodeConfig{
		APIHost:   "https://panel.example.com",
		NodeID:    1,
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
