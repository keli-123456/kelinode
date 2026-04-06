package limiter

import (
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
)

func TestCheckLimitTracksHysteria2OnlineIPFromUDPSource(t *testing.T) {
	t.Parallel()

	Init()
	tag := "hy2-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          1,
		Uuid:        "user-1",
		DeviceLimit: 1,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	_, reject := l.CheckLimit(format.UserTag(tag, "user-1"), "1.2.3.4", false)
	if reject {
		t.Fatal("expected hysteria2 UDP source to be accepted")
	}

	online, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*online); got != 1 {
		t.Fatalf("unexpected online device count: got %d want 1", got)
	}
	if got := (*online)[0].UID; got != 1 {
		t.Fatalf("unexpected online uid: got %d want 1", got)
	}
	if got := (*online)[0].IP; got != "1.2.3.4" {
		t.Fatalf("unexpected online ip: got %q want %q", got, "1.2.3.4")
	}
}

func TestCheckLimitTracksTuicOnlineIPFromUDPSource(t *testing.T) {
	t.Parallel()

	Init()
	tag := "tuic-tag"
	l := AddLimiter("tuic", tag, []panel.UserInfo{{
		Id:          2,
		Uuid:        "user-2",
		DeviceLimit: 1,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	_, reject := l.CheckLimit(format.UserTag(tag, "user-2"), "5.6.7.8", false)
	if reject {
		t.Fatal("expected tuic UDP source to be accepted")
	}

	online, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*online); got != 1 {
		t.Fatalf("unexpected online device count: got %d want 1", got)
	}
	if got := (*online)[0].IP; got != "5.6.7.8" {
		t.Fatalf("unexpected online ip: got %q want %q", got, "5.6.7.8")
	}
}

func TestGetOnlineDeviceDoesNotClearBeforeCommit(t *testing.T) {
	t.Parallel()

	Init()
	tag := "snapshot-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          3,
		Uuid:        "user-3",
		DeviceLimit: 3,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	_, reject := l.CheckLimit(format.UserTag(tag, "user-3"), "1.1.1.1", false)
	if reject {
		t.Fatal("expected first connection to be accepted")
	}

	first, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*first); got != 1 {
		t.Fatalf("unexpected first snapshot size: got %d want 1", got)
	}

	second, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device second snapshot failed: %v", err)
	}
	if got := len(*second); got != 1 {
		t.Fatalf("expected online device to remain pending before commit, got %d", got)
	}
}

func TestCommitOnlineDeviceReportKeepsNewConnectionsAfterSnapshot(t *testing.T) {
	t.Parallel()

	Init()
	tag := "commit-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          4,
		Uuid:        "user-4",
		DeviceLimit: 4,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-4")
	if _, reject := l.CheckLimit(key, "2.2.2.2", false); reject {
		t.Fatal("expected first connection to be accepted")
	}

	snapshot, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*snapshot); got != 1 {
		t.Fatalf("unexpected snapshot size: got %d want 1", got)
	}

	if _, reject := l.CheckLimit(key, "3.3.3.3", false); reject {
		t.Fatal("expected second connection to be accepted")
	}

	l.CommitOnlineDeviceReport(*snapshot)

	remaining, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get remaining online devices failed: %v", err)
	}
	if got := len(*remaining); got != 1 {
		t.Fatalf("unexpected remaining online device count: got %d want 1", got)
	}
	if got := (*remaining)[0].IP; got != "3.3.3.3" {
		t.Fatalf("unexpected remaining online ip: got %q want %q", got, "3.3.3.3")
	}
}

func TestCheckLimitRejectsBurstOfLocalNewIPsBeforeAliveSync(t *testing.T) {
	t.Parallel()

	Init()
	tag := "burst-limit-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          5,
		Uuid:        "user-5",
		DeviceLimit: 2,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-5")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected first ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); reject {
		t.Fatal("expected second ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "3.3.3.3", true); !reject {
		t.Fatal("expected third ip to be rejected before alive sync")
	}

	online, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*online); got != 2 {
		t.Fatalf("unexpected online device count after rejection: got %d want 2", got)
	}
}

func TestCheckLimitDoesNotDoubleCountPreviouslyReportedSameIP(t *testing.T) {
	t.Parallel()

	Init()
	tag := "same-ip-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          6,
		Uuid:        "user-6",
		DeviceLimit: 1,
	}}, map[int]int{
		6: 1,
	})
	l.OldUserOnline.Store("1.1.1.1", 6)
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-6")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected previously reported same ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); !reject {
		t.Fatal("expected new ip beyond device limit to be rejected")
	}
}
