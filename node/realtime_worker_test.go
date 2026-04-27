package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/keli-123456/kelinode/core"
)

func TestRunRealtimeConfigWorkerChanged(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	controller.executeConfigCheckFn = func(context.Context) (bool, error) {
		return true, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeConfigWorker(ctx)

	controller.enqueueRealtimeConfigCheck(realtimeMessage{
		EventID: "evt-config-changed",
		Topic:   "config",
		Reason:  "admin.server.saved",
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "config", "evt-config-changed", []string{"received", "applying", "applied"})
	if got, want := receipts[2].Message, "reload queued"; got != want {
		t.Fatalf("unexpected applied message: got %q want %q", got, want)
	}
}

func TestRunRealtimeConfigWorkerNoChange(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	controller.executeConfigCheckFn = func(context.Context) (bool, error) {
		return false, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeConfigWorker(ctx)

	controller.enqueueRealtimeConfigCheck(realtimeMessage{
		EventID: "evt-config-same",
		Topic:   "config",
		Reason:  "admin.server.saved",
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "config", "evt-config-same", []string{"received", "applying", "applied"})
	if got, want := receipts[2].Message, "no change"; got != want {
		t.Fatalf("unexpected applied message: got %q want %q", got, want)
	}
}

func TestRunRealtimeConfigWorkerForcesReloadForSubscriptionProxyCertChange(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	reloadCh := make(chan struct{}, 1)
	controller.server = &core.V2Core{ReloadCh: reloadCh}
	controller.executeConfigCheckFn = func(context.Context) (bool, error) {
		t.Fatalf("subscription proxy cert state change should force reload without node config check")
		return false, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeConfigWorker(ctx)

	controller.enqueueRealtimeConfigCheck(realtimeMessage{
		EventID: "evt-subproxy-cert",
		Topic:   "config",
		Reason:  realtimeReasonSubscriptionProxyCertStateChanged,
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "config", "evt-subproxy-cert", []string{"received", "applying", "applied"})
	if got, want := receipts[2].Message, "reload queued"; got != want {
		t.Fatalf("unexpected applied message: got %q want %q", got, want)
	}
	select {
	case <-reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected reload signal")
	}
}

func TestRunRealtimeConfigWorkerForcesReloadForServerMachineBindingChange(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	reloadCh := make(chan struct{}, 1)
	controller.server = &core.V2Core{ReloadCh: reloadCh}
	controller.executeConfigCheckFn = func(context.Context) (bool, error) {
		t.Fatalf("server machine binding change should force reload without node config check")
		return false, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeConfigWorker(ctx)

	controller.enqueueRealtimeConfigCheck(realtimeMessage{
		EventID: "evt-machine-bound",
		Topic:   "config",
		Reason:  realtimeReasonServerMachineBound,
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "config", "evt-machine-bound", []string{"received", "applying", "applied"})
	if got, want := receipts[2].Message, "reload queued"; got != want {
		t.Fatalf("unexpected applied message: got %q want %q", got, want)
	}
	select {
	case <-reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected reload signal")
	}
}

func TestRunRealtimeConfigWorkerFailed(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	controller.executeConfigCheckFn = func(context.Context) (bool, error) {
		return false, errors.New("config fetch failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeConfigWorker(ctx)

	controller.enqueueRealtimeConfigCheck(realtimeMessage{
		EventID: "evt-config-failed",
		Topic:   "config",
		Reason:  "admin.server.saved",
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "config", "evt-config-failed", []string{"received", "applying", "failed"})
	if got, want := receipts[2].Message, "config fetch failed"; got != want {
		t.Fatalf("unexpected failed message: got %q want %q", got, want)
	}
}

func TestRunRealtimeUserWorkerApplied(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	controller.executeUserSyncFn = func(context.Context) (userSyncSummary, error) {
		return userSyncSummary{Deleted: 1, Added: 2, Updated: 3}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeUserWorker(ctx)

	controller.enqueueRealtimeUserSync(realtimeMessage{
		EventID: "evt-users-applied",
		Topic:   "users",
		Reason:  "user.delta",
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "users", "evt-users-applied", []string{"received", "applying", "applied"})
	if got, want := receipts[2].Message, "deleted=1 added=2 updated=3"; got != want {
		t.Fatalf("unexpected applied message: got %q want %q", got, want)
	}
}

func TestRunRealtimeUserWorkerFailed(t *testing.T) {
	t.Parallel()

	controller := newTestRealtimeController()
	controller.executeUserSyncFn = func(context.Context) (userSyncSummary, error) {
		return userSyncSummary{}, errors.New("delta sync failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.runRealtimeUserWorker(ctx)

	controller.enqueueRealtimeUserSync(realtimeMessage{
		EventID: "evt-users-failed",
		Topic:   "users",
		Reason:  "user.delta",
	})

	receipts := collectRealtimeMessages(t, controller.realtimeClient.sendCh, 3)
	assertReceiptSequence(t, receipts, "users", "evt-users-failed", []string{"received", "applying", "failed"})
	if got, want := receipts[2].Message, "delta sync failed"; got != want {
		t.Fatalf("unexpected failed message: got %q want %q", got, want)
	}
}

func TestTruncateRealtimeReceiptMessage(t *testing.T) {
	t.Parallel()

	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}

	got := truncateRealtimeReceiptMessage(long)
	if len(got) != 240 {
		t.Fatalf("unexpected truncated length: got %d want 240", len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Fatalf("expected ellipsis suffix, got %q", got[len(got)-3:])
	}
}

func TestDeriveRealtimeURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		apiHost string
		want    string
	}{
		{name: "https", apiHost: "https://panel.example.com", want: "wss://panel.example.com/ws/node"},
		{name: "http", apiHost: "http://panel.example.com/base", want: "ws://panel.example.com/ws/node"},
		{name: "keep wss", apiHost: "wss://panel.example.com/custom", want: "wss://panel.example.com/ws/node"},
		{name: "invalid", apiHost: "://bad", want: ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveRealtimeURL(tt.apiHost); got != tt.want {
				t.Fatalf("unexpected realtime url: got %q want %q", got, tt.want)
			}
		})
	}
}

func newTestRealtimeController() *Controller {
	return &Controller{
		realtimeClient:   &RealtimeClient{sendCh: make(chan realtimeMessage, 16)},
		realtimeConfigCh: make(chan realtimeMessage, 4),
		realtimeUserCh:   make(chan realtimeMessage, 4),
	}
}

func collectRealtimeMessages(t *testing.T, ch <-chan realtimeMessage, count int) []realtimeMessage {
	t.Helper()

	messages := make([]realtimeMessage, 0, count)
	timeout := time.After(2 * time.Second)
	for len(messages) < count {
		select {
		case message := <-ch:
			messages = append(messages, message)
		case <-timeout:
			t.Fatalf("timed out waiting for realtime messages: got %d want %d", len(messages), count)
		}
	}
	return messages
}

func assertReceiptSequence(t *testing.T, messages []realtimeMessage, topic string, eventID string, statuses []string) {
	t.Helper()

	if len(messages) != len(statuses) {
		t.Fatalf("unexpected message count: got %d want %d", len(messages), len(statuses))
	}
	for i, message := range messages {
		if got, want := message.Type, "receipt"; got != want {
			t.Fatalf("message %d unexpected type: got %q want %q", i, got, want)
		}
		if got, want := message.Topic, topic; got != want {
			t.Fatalf("message %d unexpected topic: got %q want %q", i, got, want)
		}
		if got, want := message.EventID, eventID; got != want {
			t.Fatalf("message %d unexpected event id: got %q want %q", i, got, want)
		}
		if got, want := message.Status, statuses[i]; got != want {
			t.Fatalf("message %d unexpected status: got %q want %q", i, got, want)
		}
	}
}
