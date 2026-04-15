package limiter

import (
	"testing"
	"time"

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
	l.OldUserOnline.Store("1.1.1.1", oldOnlineEntry{
		UID:        6,
		ReportedAt: time.Now().UnixNano(),
	})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-6")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected previously reported same ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); !reject {
		t.Fatal("expected new ip beyond device limit to be rejected")
	}
}

func TestCheckLimitRejectsNewIPAfterRecentLocalReportBeforeAlivePull(t *testing.T) {
	t.Parallel()

	Init()
	tag := "recent-local-report-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          8,
		Uuid:        "user-8",
		DeviceLimit: 3,
	}}, map[int]int{})
	l.SetAliveSnapshot(&panel.AliveMap{
		Alive: map[int]int{},
		Mode:  0,
	})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-8")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected first ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); reject {
		t.Fatal("expected second ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "3.3.3.3", true); reject {
		t.Fatal("expected third ip to be accepted")
	}

	reported, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*reported); got != 3 {
		t.Fatalf("unexpected reported device count: got %d want 3", got)
	}

	l.CommitOnlineDeviceReport(*reported)

	if _, reject := l.CheckLimit(key, "4.4.4.4", true); !reject {
		t.Fatal("expected fourth ip to be rejected until next alive pull catches up")
	}
}

func TestCheckLimitAllowsSameGlobalIPInLooseMode(t *testing.T) {
	t.Parallel()

	Init()
	tag := "global-ip-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          7,
		Uuid:        "user-7",
		DeviceLimit: 1,
	}}, map[int]int{
		7: 1,
	})
	l.SetAliveSnapshot(&panel.AliveMap{
		Alive: map[int]int{
			7: 1,
		},
		AliveIPs: map[int][]string{
			7: {"1.1.1.1"},
		},
		Mode: 1,
	})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-7")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected same global ip to be accepted in loose mode")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); !reject {
		t.Fatal("expected new global ip beyond device limit to be rejected")
	}
}

func TestCheckLimitAllowsHysteria2RebindWhenKnownAliveAtLimit(t *testing.T) {
	t.Parallel()

	Init()
	tag := "hy2-rebind-known-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          9,
		Uuid:        "user-9",
		DeviceLimit: 1,
	}}, map[int]int{
		9: 1,
	})
	l.SetAliveSnapshot(&panel.AliveMap{
		Alive: map[int]int{
			9: 1,
		},
		Mode: 0,
	})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-9")
	if _, reject := l.CheckLimit(key, "2.2.2.2", false); reject {
		t.Fatal("expected hysteria2 rebind ip to be accepted when known alive already at limit")
	}
}

func TestCheckLimitAllowsHysteria2FrequentRebindByReplacingPending(t *testing.T) {
	t.Parallel()

	Init()
	tag := "hy2-rebind-replace-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          10,
		Uuid:        "user-10",
		DeviceLimit: 1,
	}}, map[int]int{
		10: 1,
	})
	l.SetAliveSnapshot(&panel.AliveMap{
		Alive: map[int]int{
			10: 1,
		},
		Mode: 0,
	})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-10")
	if _, reject := l.CheckLimit(key, "2.2.2.2", false); reject {
		t.Fatal("expected first transient rebind ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "3.3.3.3", false); reject {
		t.Fatal("expected second transient rebind ip to be accepted by replacing pending unknown ip")
	}

	online, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*online); got != 1 {
		t.Fatalf("unexpected pending online device count: got %d want 1", got)
	}
	if got := (*online)[0].IP; got != "3.3.3.3" {
		t.Fatalf("unexpected pending online ip after replacement: got %q want %q", got, "3.3.3.3")
	}
}

func TestCheckLimitRejectsSecondHysteria2IPWithoutKnownAlive(t *testing.T) {
	t.Parallel()

	Init()
	tag := "hy2-no-known-alive-tag"
	l := AddLimiter("hysteria2", tag, []panel.UserInfo{{
		Id:          11,
		Uuid:        "user-11",
		DeviceLimit: 1,
	}}, map[int]int{})
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-11")
	if _, reject := l.CheckLimit(key, "1.1.1.1", false); reject {
		t.Fatal("expected first ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", false); !reject {
		t.Fatal("expected second ip to be rejected without known alive snapshot")
	}
}

func TestCheckLimitUsesDefaultDeviceLimitWhenUserLimitIsZero(t *testing.T) {
	t.Parallel()

	Init()
	tag := "fallback-device-limit-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          12,
		Uuid:        "user-12",
		DeviceLimit: 0,
	}}, map[int]int{})
	l.SetDefaultDeviceLimit(1)
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-12")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected first ip to be accepted with default device limit")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); !reject {
		t.Fatal("expected second ip to be rejected by default device limit")
	}
}

func TestCheckLimitIgnoresNegativeDefaultDeviceLimit(t *testing.T) {
	t.Parallel()

	Init()
	tag := "negative-default-device-limit-tag"
	l := AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          13,
		Uuid:        "user-13",
		DeviceLimit: 0,
	}}, map[int]int{})
	l.SetDefaultDeviceLimit(-3)
	t.Cleanup(func() { DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-13")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected first ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); reject {
		t.Fatal("expected second ip to be accepted when fallback limit is disabled")
	}
}

func TestSetDefaultDeviceLimitClampsToInt32Max(t *testing.T) {
	t.Parallel()

	l := &Limiter{}
	l.SetDefaultDeviceLimit(1 << 40)

	if got := l.getDefaultDeviceLimit(); got != maxDefaultDeviceLimit {
		t.Fatalf("unexpected clamped fallback limit: got %d want %d", got, maxDefaultDeviceLimit)
	}
}
