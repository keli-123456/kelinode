package node

import (
	"context"
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

func TestDrainEdgeUserTrafficReportsMatchingTaggedUsersAndRestoresOthers(t *testing.T) {
	t.Parallel()

	tag := "edge-report-tag"
	edge := &fakeEdgeTrafficBridge{
		snapshot: EdgeTrafficSnapshot{Users: []EdgeTrafficRecord{
			{User: format.UserTag(tag, "user-1"), UploadBytes: 1000, DownloadBytes: 50},
			{User: format.UserTag("other-tag", "user-1"), UploadBytes: 9, DownloadBytes: 9},
			{User: format.UserTag(tag, "missing-user"), UploadBytes: 10, DownloadBytes: 20},
			{User: format.UserTag(tag, "user-2"), UploadBytes: 100, DownloadBytes: 100},
		}},
	}
	controller := &Controller{
		tag: tag,
		userList: []panel.UserInfo{
			{Id: 1, Uuid: "user-1"},
			{Id: 2, Uuid: "user-2"},
		},
		edgeTrafficBridge: edge,
	}

	drained, err := controller.drainEdgeUserTraffic(context.Background(), 1)
	if err != nil {
		t.Fatalf("drain edge traffic failed: %v", err)
	}
	if drained == nil {
		t.Fatalf("expected drained edge traffic")
	}
	if got := len(drained.report); got != 1 {
		t.Fatalf("unexpected report count: %d", got)
	}
	if got := drained.report[0]; got.UID != 1 || got.Upload != 1000 || got.Download != 50 {
		t.Fatalf("unexpected report traffic: %+v", got)
	}
	if got := len(drained.restoreRecords); got != 3 {
		t.Fatalf("unexpected restore count: %d", got)
	}

	controller.restoreEdgeTraffic(context.Background(), drained.restoreRecords, "test")
	if got := len(edge.recorded); got != 3 {
		t.Fatalf("unexpected restored edge record count: %d", got)
	}
}

func TestMergeUserTrafficAddsEdgeTrafficToExistingUID(t *testing.T) {
	t.Parallel()

	got := mergeUserTraffic(
		[]panel.UserTraffic{{UID: 1, Upload: 10, Download: 20}},
		[]panel.UserTraffic{{UID: 1, Upload: 3, Download: 4}, {UID: 2, Upload: 5, Download: 6}},
	)
	if len(got) != 2 {
		t.Fatalf("unexpected merged count: %d", len(got))
	}
	if got[0].UID != 1 || got[0].Upload != 13 || got[0].Download != 24 {
		t.Fatalf("unexpected merged first traffic: %+v", got[0])
	}
	if got[1].UID != 2 || got[1].Upload != 5 || got[1].Download != 6 {
		t.Fatalf("unexpected merged second traffic: %+v", got[1])
	}
}

type fakeEdgeTrafficBridge struct {
	snapshot EdgeTrafficSnapshot
	recorded []EdgeTrafficRecord
}

func (f *fakeEdgeTrafficBridge) DrainTraffic(context.Context) (EdgeTrafficSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeEdgeTrafficBridge) RecordTraffic(_ context.Context, record EdgeTrafficRecord) error {
	f.recorded = append(f.recorded, record)
	return nil
}
