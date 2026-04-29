package cmd

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/node"
)

type machineRealtimeState struct {
	mu          sync.Mutex
	manager     *node.MachineRealtimeManager
	fingerprint string
}

func newMachineRealtimeState() *machineRealtimeState {
	return &machineRealtimeState{}
}

func (s *machineRealtimeState) Apply(machine conf.MachineConfig, realtime conf.RealtimeConfig, requestReload func()) {
	if s == nil {
		return
	}
	profiles := normalizeMachineRealtimeProfiles(machine.Profiles)
	if !machine.Enabled || len(profiles) == 0 {
		s.Close()
		return
	}

	nextFingerprint := machineRealtimeFingerprint(profiles, realtime)
	s.mu.Lock()
	if s.manager != nil && s.fingerprint == nextFingerprint {
		s.mu.Unlock()
		return
	}
	old := s.manager
	manager := node.NewMachineRealtimeManager(context.Background(), profiles, realtime, requestReload)
	s.manager = manager
	s.fingerprint = nextFingerprint
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}
	manager.Start()
}

func (s *machineRealtimeState) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	manager := s.manager
	s.manager = nil
	s.fingerprint = ""
	s.mu.Unlock()
	if manager != nil {
		manager.Close()
	}
}

func normalizeMachineRealtimeProfiles(profiles []conf.MachineProfileConfig) []conf.MachineProfileConfig {
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

func machineRealtimeFingerprint(profiles []conf.MachineProfileConfig, realtime conf.RealtimeConfig) string {
	parts := make([]string, 0, len(profiles)+1)
	parts = append(parts, strings.Join([]string{
		boolFingerprint(realtime.Enabled),
		strings.TrimSpace(realtime.URL),
		strconv.Itoa(realtime.PingInterval),
		strconv.Itoa(realtime.ReconnectInterval),
	}, "#"))
	for _, profile := range profiles {
		parts = append(parts, strings.Join([]string{
			profile.APIHost,
			strconv.Itoa(profile.MachineID),
			profile.Key,
			boolFingerprint(profile.Realtime.Enabled),
			strings.TrimSpace(profile.Realtime.URL),
			strconv.Itoa(profile.Realtime.PingInterval),
			strconv.Itoa(profile.Realtime.ReconnectInterval),
		}, "#"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func boolFingerprint(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
