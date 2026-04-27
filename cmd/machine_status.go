package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keli-123456/kelinode/agent/subproxy"
	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

type machineStatusReporterState struct {
	mu          sync.Mutex
	cancel      context.CancelFunc
	fingerprint string
}

func newMachineStatusReporterState() *machineStatusReporterState {
	return &machineStatusReporterState{}
}

func (s *machineStatusReporterState) Apply(machine conf.MachineConfig, agentStatus func() subproxy.Status) {
	if s == nil {
		return
	}
	profiles := normalizeStatusProfiles(machine.Profiles)
	if !machine.Enabled || len(profiles) == 0 {
		s.Close()
		return
	}

	nextFingerprint := machineStatusFingerprint(profiles)
	s.mu.Lock()
	if s.cancel != nil && s.fingerprint == nextFingerprint {
		s.mu.Unlock()
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.fingerprint = nextFingerprint
	s.mu.Unlock()

	go runMachineStatusReporter(ctx, profiles, agentStatus)
}

func (s *machineStatusReporterState) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.fingerprint = ""
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func runMachineStatusReporter(ctx context.Context, profiles []conf.MachineProfileConfig, agentStatus func() subproxy.Status) {
	systemSampler := newMachineSystemSampler()
	report := func() {
		systemStatus := systemSampler.Snapshot()
		for _, profile := range profiles {
			if err := reportMachineStatus(ctx, profile, agentStatus, systemStatus); err != nil {
				log.WithFields(log.Fields{
					"profile":    profile.Name,
					"machine_id": profile.MachineID,
					"err":        err,
				}).Debug("Machine status report failed")
			}
		}
	}
	report()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report()
		}
	}
}

func reportMachineStatus(ctx context.Context, profile conf.MachineProfileConfig, agentStatus func() subproxy.Status, systemStatus map[string]any) error {
	apiHost := strings.TrimRight(strings.TrimSpace(profile.APIHost), "/")
	token := strings.TrimSpace(profile.Key)
	if apiHost == "" || token == "" || profile.MachineID <= 0 {
		return nil
	}
	status := buildMachineStatusPayload(systemStatus)
	status["version"] = version
	if agentStatus != nil {
		status["agent"] = map[string]any{
			"subscription_proxy": agentStatus(),
		}
	}
	body, err := json.Marshal(map[string]any{
		"machine_id": profile.MachineID,
		"token":      token,
		"status":     status,
	})
	if err != nil {
		return err
	}

	timeout := 30 * time.Second
	if profile.Timeout > 0 {
		timeout = time.Duration(profile.Timeout) * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiHost+panel.PathV2MachineStatus, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, string(data))
	}
	return nil
}

func buildMachineStatusPayload(systemStatus map[string]any) map[string]any {
	status := map[string]any{}
	for key, value := range systemStatus {
		status[key] = value
	}
	return status
}

func normalizeStatusProfiles(profiles []conf.MachineProfileConfig) []conf.MachineProfileConfig {
	out := make([]conf.MachineProfileConfig, 0, len(profiles))
	for _, profile := range profiles {
		profile.APIHost = strings.TrimRight(strings.TrimSpace(profile.APIHost), "/")
		profile.Key = strings.TrimSpace(profile.Key)
		if profile.APIHost == "" || profile.Key == "" || profile.MachineID <= 0 {
			continue
		}
		out = append(out, profile)
	}
	return out
}

func machineStatusFingerprint(profiles []conf.MachineProfileConfig) string {
	parts := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		parts = append(parts, profile.APIHost+"#"+strconv.Itoa(profile.MachineID)+"#"+profile.Key)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
