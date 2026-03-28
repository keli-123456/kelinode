package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

func TestRealtimeClientConnectAndServeRoundTrip(t *testing.T) {
	originalDial := dialRealtimeWS
	t.Cleanup(func() { dialRealtimeWS = originalDial })

	SetRealtimeHealthSnapshot(RealtimeHealthSnapshot{
		Status: "ok",
		Ready:  true,
	})

	conn := newFakeRealtimeConn()
	gotURLCh := make(chan string, 1)
	dialRealtimeWS = func(ctx context.Context, rawURL string) (realtimeWSConn, error) {
		gotURLCh <- rawURL
		return conn, nil
	}

	invalidateCh := make(chan realtimeMessage, 1)
	client := NewRealtimeClient(context.Background(), RealtimeOptions{
		URL:            "wss://panel.example.com/ws/node?foo=bar",
		Token:          "node-token",
		NodeID:         32,
		NodeType:       "v2node",
		PingInterval:   0,
		ReconnectDelay: time.Second,
		LogTag:         "[panel]-hysteria2:32",
	}, func(message realtimeMessage) {
		invalidateCh <- message
	})

	client.Send(realtimeMessage{
		Type:    "receipt",
		Topic:   "config",
		EventID: "evt-1",
		Status:  "applied",
	})

	conn.pushJSON(t, realtimeMessage{
		Type:     "hello_ack",
		ServerID: 8,
		NodeID:   "32",
		NodeType: "v2node",
	})
	conn.pushJSON(t, realtimeMessage{Type: "ping"})
	conn.pushJSON(t, realtimeMessage{
		Type:     "invalidate",
		Topic:    "users",
		Reason:   "user.delta",
		Revision: 12,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe()
	}()

	select {
	case gotURL := <-gotURLCh:
		parsed, err := url.Parse(gotURL)
		if err != nil {
			t.Fatalf("parse dial url failed: %v", err)
		}
		query := parsed.Query()
		if got, want := query.Get("foo"), "bar"; got != want {
			t.Fatalf("unexpected preserved query: got %q want %q", got, want)
		}
		if got, want := query.Get("token"), "node-token"; got != want {
			t.Fatalf("unexpected token query: got %q want %q", got, want)
		}
		if got, want := query.Get("node_id"), "32"; got != want {
			t.Fatalf("unexpected node_id query: got %q want %q", got, want)
		}
		if got, want := query.Get("node_type"), "v2node"; got != want {
			t.Fatalf("unexpected node_type query: got %q want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected dial url to be captured")
	}

	select {
	case message := <-invalidateCh:
		if got, want := message.Topic, "users"; got != want {
			t.Fatalf("unexpected invalidate topic: got %q want %q", got, want)
		}
		if got, want := message.Reason, "user.delta"; got != want {
			t.Fatalf("unexpected invalidate reason: got %q want %q", got, want)
		}
		if got, want := message.Revision, int64(12); got != want {
			t.Fatalf("unexpected invalidate revision: got %d want %d", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected invalidate callback to be invoked")
	}

	waitForRealtimeWrite(t, conn, func(message realtimeMessage) bool {
		return message.Type == "receipt" &&
			message.Topic == "config" &&
			message.EventID == "evt-1" &&
			message.Status == "applied"
	}, "queued outbound receipt")

	conn.pushError(io.EOF)

	err := <-errCh
	if !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected connectAndServe error: %v", err)
	}

	writes := conn.snapshotWrites()
	assertHasRealtimeWrite(t, writes, func(message realtimeMessage) bool {
		return message.Type == "ping" &&
			message.Token == "node-token" &&
			message.NodeID == "32" &&
			message.NodeType == "v2node" &&
			message.Health != nil &&
			message.Health.Status == "ok"
	}, "initial auth ping")
	assertHasRealtimeWrite(t, writes, func(message realtimeMessage) bool {
		return message.Type == "receipt" &&
			message.Topic == "config" &&
			message.EventID == "evt-1" &&
			message.Status == "applied"
	}, "queued outbound receipt")
	assertHasRealtimeWrite(t, writes, func(message realtimeMessage) bool {
		return message.Type == "pong"
	}, "pong response")
}

func TestRealtimeClientBuildDialURL(t *testing.T) {
	client := &RealtimeClient{
		opts: RealtimeOptions{
			URL:      "wss://panel.example.com/ws/node?foo=1",
			Token:    "token",
			NodeID:   9,
			NodeType: "v2node",
		},
	}

	got, err := client.buildDialURL()
	if err != nil {
		t.Fatalf("build dial url failed: %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse dial url failed: %v", err)
	}
	query := parsed.Query()
	if got, want := query.Get("foo"), "1"; got != want {
		t.Fatalf("unexpected preserved query: got %q want %q", got, want)
	}
	if got, want := query.Get("token"), "token"; got != want {
		t.Fatalf("unexpected token query: got %q want %q", got, want)
	}
	if got, want := query.Get("node_id"), "9"; got != want {
		t.Fatalf("unexpected node_id query: got %q want %q", got, want)
	}
	if got, want := query.Get("node_type"), "v2node"; got != want {
		t.Fatalf("unexpected node_type query: got %q want %q", got, want)
	}
}

func TestResolveRealtimeOptionsPriority(t *testing.T) {
	controller := &Controller{
		conf: &conf.NodeConfig{
			APIHost: "https://panel.example.com",
			NodeID:  7,
			Key:     "node-token",
		},
		tag: "[panel]-hysteria2:7",
		info: &panel.NodeInfo{
			Common: &panel.CommonNode{
				BaseConfig: &panel.BaseConfig{
					Realtime: &panel.RealtimeBaseConfig{
						Enabled:      true,
						URL:          "wss://panel.example.com/ws/from-panel",
						PingInterval: 15,
					},
				},
			},
		},
		realtimeConfig: conf.RealtimeConfig{
			Enabled:           true,
			URL:               "wss://panel.example.com/ws/local",
			PingInterval:      8,
			ReconnectInterval: 11,
		},
	}

	options := controller.resolveRealtimeOptions()
	if options == nil {
		t.Fatalf("expected realtime options")
	}
	if got, want := options.URL, "wss://panel.example.com/ws/local"; got != want {
		t.Fatalf("unexpected realtime url: got %q want %q", got, want)
	}
	if got, want := options.PingInterval, 8*time.Second; got != want {
		t.Fatalf("unexpected ping interval: got %s want %s", got, want)
	}
	if got, want := options.ReconnectDelay, 11*time.Second; got != want {
		t.Fatalf("unexpected reconnect delay: got %s want %s", got, want)
	}
}

func TestResolveRealtimeOptionsDerivedFromAPIHost(t *testing.T) {
	controller := &Controller{
		conf: &conf.NodeConfig{
			APIHost: "https://panel.example.com/base",
			NodeID:  7,
			Key:     "node-token",
		},
		tag: "[panel]-hysteria2:7",
		realtimeConfig: conf.RealtimeConfig{
			Enabled: true,
		},
	}

	options := controller.resolveRealtimeOptions()
	if options == nil {
		t.Fatalf("expected realtime options")
	}
	if got, want := options.URL, "wss://panel.example.com/ws/node"; got != want {
		t.Fatalf("unexpected derived realtime url: got %q want %q", got, want)
	}
	if got, want := options.PingInterval, 30*time.Second; got != want {
		t.Fatalf("unexpected default ping interval: got %s want %s", got, want)
	}
}

func TestResolveRealtimeOptionsDisabled(t *testing.T) {
	controller := &Controller{
		conf: &conf.NodeConfig{
			APIHost: "https://panel.example.com",
			NodeID:  7,
			Key:     "node-token",
		},
		tag:            "[panel]-hysteria2:7",
		realtimeConfig: conf.RealtimeConfig{},
	}

	if got := controller.resolveRealtimeOptions(); got != nil {
		t.Fatalf("expected realtime options to be nil when disabled, got %+v", got)
	}
}

type fakeRealtimeConn struct {
	readCh  chan fakeRealtimeRead
	closeCh chan struct{}

	mu     sync.Mutex
	writes []realtimeMessage
}

type fakeRealtimeRead struct {
	payload []byte
	err     error
}

func newFakeRealtimeConn() *fakeRealtimeConn {
	return &fakeRealtimeConn{
		readCh:  make(chan fakeRealtimeRead, 8),
		closeCh: make(chan struct{}),
	}
}

func (c *fakeRealtimeConn) SetWriteDeadline(time.Time) error { return nil }

func (c *fakeRealtimeConn) WriteJSON(v interface{}) error {
	message, ok := v.(realtimeMessage)
	if !ok {
		return errors.New("unexpected write payload type")
	}
	c.mu.Lock()
	c.writes = append(c.writes, message)
	c.mu.Unlock()
	return nil
}

func (c *fakeRealtimeConn) ReadMessage() (int, []byte, error) {
	select {
	case item := <-c.readCh:
		if item.err != nil {
			return 0, nil, item.err
		}
		return websocket.TextMessage, item.payload, nil
	case <-c.closeCh:
		return 0, nil, io.EOF
	}
}

func (c *fakeRealtimeConn) Close() error {
	select {
	case <-c.closeCh:
	default:
		close(c.closeCh)
	}
	return nil
}

func (c *fakeRealtimeConn) pushJSON(t *testing.T, message realtimeMessage) {
	t.Helper()
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal realtime message failed: %v", err)
	}
	c.readCh <- fakeRealtimeRead{payload: payload}
}

func (c *fakeRealtimeConn) pushError(err error) {
	c.readCh <- fakeRealtimeRead{err: err}
}

func (c *fakeRealtimeConn) snapshotWrites() []realtimeMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]realtimeMessage, len(c.writes))
	copy(out, c.writes)
	return out
}

func assertHasRealtimeWrite(t *testing.T, writes []realtimeMessage, match func(realtimeMessage) bool, label string) {
	t.Helper()
	for _, message := range writes {
		if match(message) {
			return
		}
	}
	t.Fatalf("expected realtime write not found: %s; got %+v", label, writes)
}

func waitForRealtimeWrite(t *testing.T, conn *fakeRealtimeConn, match func(realtimeMessage) bool, label string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hasRealtimeWrite(conn.snapshotWrites(), match) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for realtime write: %s; got %+v", label, conn.snapshotWrites())
}

func hasRealtimeWrite(writes []realtimeMessage, match func(realtimeMessage) bool) bool {
	for _, message := range writes {
		if match(message) {
			return true
		}
	}
	return false
}
