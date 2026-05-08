package node

import (
	"context"
	"strings"

	panel "github.com/keli-123456/kelinode/api/v2board"
	log "github.com/sirupsen/logrus"
)

type EdgeTrafficRecord struct {
	User          string
	UploadBytes   uint64
	DownloadBytes uint64
}

type EdgeTrafficSnapshot struct {
	Users []EdgeTrafficRecord
}

type EdgeTrafficBridge interface {
	DrainTraffic(ctx context.Context) (EdgeTrafficSnapshot, error)
	RecordTraffic(ctx context.Context, record EdgeTrafficRecord) error
}

type drainedEdgeTraffic struct {
	report         []panel.UserTraffic
	reportRecords  []EdgeTrafficRecord
	restoreRecords []EdgeTrafficRecord
}

type edgeTrafficAccumulator struct {
	upload   uint64
	download uint64
}

func (c *Controller) drainEdgeUserTraffic(ctx context.Context, mintraffic int) (*drainedEdgeTraffic, error) {
	if c == nil || c.edgeTrafficBridge == nil {
		return nil, nil
	}
	snapshot, err := c.edgeTrafficBridge.DrainTraffic(ctx)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Users) == 0 {
		return nil, nil
	}

	uuidToUID := make(map[string]int, len(c.userList))
	for _, user := range c.userList {
		if user.Uuid == "" || user.Id <= 0 {
			continue
		}
		uuidToUID[user.Uuid] = user.Id
	}

	minBytes := trafficMinBytes(mintraffic)
	merged := make(map[int]edgeTrafficAccumulator)
	drained := &drainedEdgeTraffic{}
	for _, record := range snapshot.Users {
		if record.User == "" || (record.UploadBytes == 0 && record.DownloadBytes == 0) {
			continue
		}
		uuid, ok := c.edgeTrafficUUID(record.User)
		if !ok {
			drained.restoreRecords = append(drained.restoreRecords, record)
			continue
		}
		uid, ok := uuidToUID[uuid]
		if !ok {
			drained.restoreRecords = append(drained.restoreRecords, record)
			continue
		}
		if edgeTrafficBelowMin(record, minBytes) {
			drained.restoreRecords = append(drained.restoreRecords, record)
			continue
		}

		acc := merged[uid]
		acc.upload = saturatingAddUint64(acc.upload, record.UploadBytes)
		acc.download = saturatingAddUint64(acc.download, record.DownloadBytes)
		merged[uid] = acc
		drained.reportRecords = append(drained.reportRecords, record)
	}

	for uid, traffic := range merged {
		drained.report = append(drained.report, panel.UserTraffic{
			UID:      uid,
			Upload:   uint64ToInt64Saturated(traffic.upload),
			Download: uint64ToInt64Saturated(traffic.download),
		})
	}
	return drained, nil
}

func (c *Controller) edgeTrafficUUID(user string) (string, bool) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", false
	}
	tag, uuid, tagged := strings.Cut(user, "|")
	if !tagged {
		return "", false
	}
	uuid = strings.TrimSpace(uuid)
	if tag != c.tag || uuid == "" {
		return "", false
	}
	return uuid, true
}

func (c *Controller) restoreEdgeTraffic(ctx context.Context, records []EdgeTrafficRecord, reason string) {
	if c == nil || c.edgeTrafficBridge == nil || len(records) == 0 {
		return
	}

	var firstErr error
	restored := 0
	for _, record := range records {
		if record.User == "" || (record.UploadBytes == 0 && record.DownloadBytes == 0) {
			continue
		}
		if err := c.edgeTrafficBridge.RecordTraffic(ctx, record); err != nil {
			firstErr = err
			break
		}
		restored++
	}
	if firstErr != nil {
		log.WithFields(log.Fields{
			"tag":      c.tag,
			"restored": restored,
			"total":    len(records),
			"reason":   reason,
			"err":      firstErr,
		}).Warn("Restore edge traffic failed")
	}
}

func mergeUserTraffic(primary []panel.UserTraffic, extra []panel.UserTraffic) []panel.UserTraffic {
	if len(extra) == 0 {
		return primary
	}
	if len(primary) == 0 {
		return append([]panel.UserTraffic(nil), extra...)
	}

	positions := make(map[int]int, len(primary)+len(extra))
	merged := append([]panel.UserTraffic(nil), primary...)
	for i := range merged {
		positions[merged[i].UID] = i
	}
	for _, traffic := range extra {
		if idx, ok := positions[traffic.UID]; ok {
			merged[idx].Upload = saturatingAddInt64(merged[idx].Upload, traffic.Upload)
			merged[idx].Download = saturatingAddInt64(merged[idx].Download, traffic.Download)
			continue
		}
		positions[traffic.UID] = len(merged)
		merged = append(merged, traffic)
	}
	return merged
}

func trafficMinBytes(mintraffic int) uint64 {
	if mintraffic <= 0 {
		return 0
	}
	return uint64(mintraffic) * 1000
}

func edgeTrafficBelowMin(record EdgeTrafficRecord, minBytes uint64) bool {
	if minBytes == 0 {
		return false
	}
	return saturatingAddUint64(record.UploadBytes, record.DownloadBytes) <= minBytes
}

func saturatingAddUint64(left uint64, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func uint64ToInt64Saturated(value uint64) int64 {
	const maxInt64 = uint64(1<<63 - 1)
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}

func saturatingAddInt64(left int64, right int64) int64 {
	const maxInt64 = int64(1<<63 - 1)
	if right > 0 && left > maxInt64-right {
		return maxInt64
	}
	return left + right
}
