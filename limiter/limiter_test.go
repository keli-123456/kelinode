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
