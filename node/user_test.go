package node

import (
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
	"github.com/keli-123456/kelinode/limiter"
)

func TestCommitReportedOnlineDevicesKeepsUnreportedDevicesPending(t *testing.T) {
	t.Parallel()

	limiter.Init()
	tag := "partial-online-report-tag"
	l := limiter.AddLimiter("vless", tag, []panel.UserInfo{{
		Id:          1,
		Uuid:        "user-1",
		DeviceLimit: 3,
	}}, map[int]int{})
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })

	key := format.UserTag(tag, "user-1")
	if _, reject := l.CheckLimit(key, "1.1.1.1", true); reject {
		t.Fatal("expected first ip to be accepted")
	}
	if _, reject := l.CheckLimit(key, "2.2.2.2", true); reject {
		t.Fatal("expected second ip to be accepted")
	}

	onlineDevice, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get online device failed: %v", err)
	}
	if got := len(*onlineDevice); got != 2 {
		t.Fatalf("unexpected online device count: got %d want 2", got)
	}

	c := &Controller{
		tag:     tag,
		limiter: l,
	}
	c.commitReportedOnlineDevices(onlineDevice, []panel.OnlineUser{(*onlineDevice)[0]})

	remaining, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("get remaining online device failed: %v", err)
	}
	if got := len(*remaining); got != 1 {
		t.Fatalf("unexpected remaining online device count: got %d want 1", got)
	}
	if got := (*remaining)[0].IP; got == (*onlineDevice)[0].IP {
		t.Fatalf("reported ip remained pending: %q", got)
	}
}
