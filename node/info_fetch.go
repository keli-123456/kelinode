package node

import (
	"context"
	"fmt"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

// FetchNodeInfos loads panel node configs without starting controllers or core.
func FetchNodeInfos(ctx context.Context, configs []conf.NodeConfig, opts MachineOptions) ([]*panel.NodeInfo, []NodeFailure, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	factory := defaultControlPlaneFactory()
	infos := make([]*panel.NodeInfo, 0, len(configs))
	failures := make([]NodeFailure, 0)

	for i := range configs {
		cfg := configs[i]
		controlPlane, err := factory.New(&configs[i])
		if err != nil {
			if opts.ContinueOnError {
				failures = append(failures, NodeFailure{Config: cfg, Err: err})
				log.WithFields(log.Fields{
					"api_host": cfg.APIHost,
					"node_id":  cfg.NodeID,
					"err":      err,
				}).Warn("Skipped node info fetch")
				continue
			}
			return nil, failures, fmt.Errorf("create control plane [%s-%d] error: %w", cfg.APIHost, cfg.NodeID, err)
		}

		info, err := controlPlane.GetNodeInfo(ctx)
		if err != nil {
			if opts.ContinueOnError {
				failures = append(failures, NodeFailure{Config: cfg, Err: err})
				log.WithFields(log.Fields{
					"api_host": cfg.APIHost,
					"node_id":  cfg.NodeID,
					"err":      err,
				}).Warn("Skipped node info fetch")
				continue
			}
			return nil, failures, fmt.Errorf("get node info [%s-%d] error: %w", cfg.APIHost, cfg.NodeID, err)
		}
		if info == nil {
			err := fmt.Errorf("received empty node info")
			if opts.ContinueOnError {
				failures = append(failures, NodeFailure{Config: cfg, Err: err})
				continue
			}
			return nil, failures, fmt.Errorf("get node info [%s-%d] error: %w", cfg.APIHost, cfg.NodeID, err)
		}
		infos = append(infos, info)
	}

	if len(configs) > 0 && len(infos) == 0 && !opts.ContinueOnError {
		return nil, failures, fmt.Errorf("no node infos resolved")
	}
	return infos, failures, nil
}
