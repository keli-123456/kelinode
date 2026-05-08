package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/keli-123456/kelinode/agent/edge"
	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/node"
	log "github.com/sirupsen/logrus"
)

type edgeControlState struct {
	mu     sync.RWMutex
	client *edge.Client
}

func newEdgeControlState() *edgeControlState {
	return &edgeControlState{}
}

func (s *edgeControlState) Apply(cfg conf.EdgeConfig) {
	if !cfg.Enabled {
		s.mu.Lock()
		s.client = nil
		s.mu.Unlock()
		return
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	client := edge.NewClient(cfg.URL, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	health, err := client.Health(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"url": cfg.URL,
			"err": err,
		}).Warn("Keli edge health check failed")
		s.mu.Lock()
		s.client = client
		s.mu.Unlock()
		return
	}

	log.WithFields(log.Fields{
		"url":     cfg.URL,
		"version": health.Version,
		"status":  health.Status,
	}).Info("Keli edge connected")
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
}

func (s *edgeControlState) Reload(ctx context.Context) {
	client := s.currentClient()
	if client == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := client.Reload(ctx); err != nil {
		log.WithField("err", err).Warn("Keli edge reload request failed")
	}
}

func (s *edgeControlState) DrainTraffic(ctx context.Context) (node.EdgeTrafficSnapshot, error) {
	client := s.currentClient()
	if client == nil {
		return node.EdgeTrafficSnapshot{}, nil
	}
	snapshot, err := client.DrainTraffic(ctx)
	if err != nil {
		return node.EdgeTrafficSnapshot{}, err
	}
	users := make([]node.EdgeTrafficRecord, 0, len(snapshot.Users))
	for _, user := range snapshot.Users {
		users = append(users, node.EdgeTrafficRecord{
			User:          user.User,
			UploadBytes:   user.UploadBytes,
			DownloadBytes: user.DownloadBytes,
		})
	}
	return node.EdgeTrafficSnapshot{Users: users}, nil
}

func (s *edgeControlState) RecordTraffic(ctx context.Context, record node.EdgeTrafficRecord) error {
	client := s.currentClient()
	if client == nil {
		return fmt.Errorf("keli-edge client is disabled")
	}
	return client.RecordTraffic(ctx, edge.TrafficRecord{
		User:          record.User,
		UploadBytes:   record.UploadBytes,
		DownloadBytes: record.DownloadBytes,
	})
}

func (s *edgeControlState) UpsertSidecar(ctx context.Context, spec node.EdgeSidecarSpec) (node.EdgeSidecarApplyReport, error) {
	client := s.currentClient()
	if client == nil {
		return node.EdgeSidecarApplyReport{}, fmt.Errorf("keli-edge client is disabled")
	}
	report, err := client.UpsertSidecar(ctx, edge.SidecarSpec{
		Name:           spec.Name,
		Protocol:       spec.Protocol,
		Enabled:        spec.Enabled,
		Binary:         spec.Binary,
		Args:           spec.Args,
		Env:            spec.Env,
		GeneratedFiles: edgeGeneratedFiles(spec.GeneratedFiles),
	})
	if err != nil {
		return node.EdgeSidecarApplyReport{}, err
	}
	failures := make([]node.EdgeSidecarFailure, 0, len(report.Failed))
	for _, failure := range report.Failed {
		failures = append(failures, node.EdgeSidecarFailure{
			Name:  failure.Name,
			Error: failure.Error,
		})
	}
	return node.EdgeSidecarApplyReport{
		Started: report.Started,
		Stopped: report.Stopped,
		Failed:  failures,
	}, nil
}

func edgeGeneratedFiles(files []node.EdgeGeneratedFile) []edge.GeneratedFile {
	if len(files) == 0 {
		return nil
	}
	result := make([]edge.GeneratedFile, 0, len(files))
	for _, file := range files {
		result = append(result, edge.GeneratedFile{
			Path:     file.Path,
			Contents: file.Contents,
		})
	}
	return result
}

func (s *edgeControlState) currentClient() *edge.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}
