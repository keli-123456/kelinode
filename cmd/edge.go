package cmd

import (
	"context"
	"time"

	"github.com/keli-123456/kelinode/agent/edge"
	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

type edgeControlState struct {
	client *edge.Client
}

func newEdgeControlState() *edgeControlState {
	return &edgeControlState{}
}

func (s *edgeControlState) Apply(cfg conf.EdgeConfig) {
	if !cfg.Enabled {
		s.client = nil
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
		s.client = client
		return
	}

	log.WithFields(log.Fields{
		"url":     cfg.URL,
		"version": health.Version,
		"status":  health.Status,
	}).Info("Keli edge connected")
	s.client = client
}

func (s *edgeControlState) Reload(ctx context.Context) {
	if s.client == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.client.Reload(ctx); err != nil {
		log.WithField("err", err).Warn("Keli edge reload request failed")
	}
}
