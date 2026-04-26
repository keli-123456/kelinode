package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

var machineProfileHTTPClient = func(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

type MachinePanelNode struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	UpdatedAt any    `json:"updated_at"`
}

type machineNodesResponse struct {
	Nodes []MachinePanelNode `json:"nodes"`
	Data  *struct {
		Nodes []MachinePanelNode `json:"nodes"`
	} `json:"data"`
}

func ResolveMachineNodeConfigs(ctx context.Context, cfg *conf.Conf) error {
	if cfg == nil || !cfg.MachineConfig.Enabled || len(cfg.MachineConfig.Profiles) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	nodes := make([]conf.NodeConfig, 0, len(cfg.NodeConfigs))
	seen := make(map[string]struct{})
	for _, existing := range cfg.NodeConfigs {
		key := machineProfileNodeKey(existing.APIHost, existing.MachineID, existing.NodeID)
		if key != "" {
			seen[key] = struct{}{}
		}
		nodes = append(nodes, existing)
	}

	var failures []string
	for _, profile := range cfg.MachineConfig.Profiles {
		panelNodes, err := fetchMachineProfileNodes(ctx, profile)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", machineProfileLabel(profile), err))
			if cfg.MachineConfig.ContinueOnError {
				log.WithFields(log.Fields{
					"profile":    machineProfileLabel(profile),
					"machine_id": profile.MachineID,
					"err":        err,
				}).Warn("Machine profile skipped")
				continue
			}
			return err
		}

		for _, panelNode := range panelNodes {
			if panelNode.ID <= 0 {
				continue
			}
			key := machineProfileNodeKey(profile.APIHost, profile.MachineID, panelNode.ID)
			if _, exists := seen[key]; exists {
				err := fmt.Errorf("duplicate machine profile node: %s", key)
				if cfg.MachineConfig.ContinueOnError {
					failures = append(failures, err.Error())
					continue
				}
				return err
			}
			seen[key] = struct{}{}
			nodes = append(nodes, conf.NodeConfig{
				APIHost:   profile.APIHost,
				NodeID:    panelNode.ID,
				Key:       profile.Key,
				MachineID: profile.MachineID,
				Timeout:   profile.Timeout,
				ConfigDir: machineProfileNodeConfigDir(profile, panelNode.ID),
			})
		}
	}

	if len(nodes) == 0 {
		if len(failures) > 0 {
			return fmt.Errorf("no machine nodes resolved: %s", strings.Join(failures, "; "))
		}
		return fmt.Errorf("no machine nodes resolved")
	}

	cfg.NodeConfigs = nodes
	return nil
}

func fetchMachineProfileNodes(ctx context.Context, profile conf.MachineProfileConfig) ([]MachinePanelNode, error) {
	apiHost := strings.TrimRight(strings.TrimSpace(profile.APIHost), "/")
	token := strings.TrimSpace(profile.Key)
	if apiHost == "" || token == "" || profile.MachineID <= 0 {
		return nil, fmt.Errorf("machine profile requires api host, token and machine id")
	}

	body, err := json.Marshal(map[string]any{
		"machine_id": profile.MachineID,
		"token":      token,
	})
	if err != nil {
		return nil, err
	}

	timeout := 30 * time.Second
	if profile.Timeout > 0 {
		timeout = time.Duration(profile.Timeout) * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiHost+panel.PathV2MachineNodes, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := machineProfileHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("machine nodes request failed: status=%d body=%s", resp.StatusCode, string(data))
	}

	var payload machineNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode machine nodes response failed: %w", err)
	}
	if payload.Data != nil {
		payload.Nodes = payload.Data.Nodes
	}

	return payload.Nodes, nil
}

func machineProfileNodeConfigDir(profile conf.MachineProfileConfig, nodeID int) string {
	root := strings.TrimSpace(profile.ConfigDir)
	if root == "" {
		root = filepath.Join(conf.DefaultNodeConfigDir, sanitizeMachineProfileName(machineProfileLabel(profile)))
	}
	return conf.NormalizeConfigDir(filepath.Join(root, fmt.Sprintf("node-%d", nodeID)))
}

func machineProfileLabel(profile conf.MachineProfileConfig) string {
	if name := strings.TrimSpace(profile.Name); name != "" {
		return name
	}
	if profile.MachineID > 0 {
		return "machine-" + strconv.Itoa(profile.MachineID)
	}
	return strings.TrimRight(strings.TrimSpace(profile.APIHost), "/")
}

var nonMachineProfileNameChar = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeMachineProfileName(name string) string {
	name = strings.Trim(nonMachineProfileNameChar.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if name == "" {
		return "machine"
	}
	return name
}

func machineProfileNodeKey(apiHost string, machineID int, nodeID int) string {
	apiHost = strings.TrimRight(strings.TrimSpace(apiHost), "/")
	if apiHost == "" || nodeID <= 0 {
		return ""
	}
	return apiHost + "#" + strconv.Itoa(machineID) + "#" + strconv.Itoa(nodeID)
}
