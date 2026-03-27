package node

import (
	"sync"
	"time"
)

type RealtimeRuntimeSnapshot struct {
	GoMemLimit      string `json:"gomemlimit,omitempty"`
	GoMemLimitBytes int64  `json:"gomemlimit_bytes,omitempty"`
	GOGC            int    `json:"gogc,omitempty"`
}

type RealtimeHealthSnapshot struct {
	Status          string                  `json:"status,omitempty"`
	Ready           bool                    `json:"ready"`
	Version         string                  `json:"version,omitempty"`
	ConfigPath      string                  `json:"config_path,omitempty"`
	StartedAt       string                  `json:"started_at,omitempty"`
	UptimeSeconds   int64                   `json:"uptime_seconds,omitempty"`
	LastReloadAt    string                  `json:"last_reload_at,omitempty"`
	NodeCount       int                     `json:"node_count,omitempty"`
	RealtimeEnabled bool                    `json:"realtime_enabled"`
	HealthPort      int                     `json:"health_port,omitempty"`
	Goroutines      int                     `json:"goroutines,omitempty"`
	Runtime         RealtimeRuntimeSnapshot `json:"runtime"`
	UpdatedAt       string                  `json:"updated_at,omitempty"`
}

var realtimeHealthState struct {
	mu       sync.RWMutex
	snapshot RealtimeHealthSnapshot
}

func SetRealtimeHealthSnapshot(snapshot RealtimeHealthSnapshot) {
	realtimeHealthState.mu.Lock()
	defer realtimeHealthState.mu.Unlock()

	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	realtimeHealthState.snapshot = snapshot
}

func GetRealtimeHealthSnapshot() *RealtimeHealthSnapshot {
	realtimeHealthState.mu.RLock()
	defer realtimeHealthState.mu.RUnlock()

	if realtimeHealthState.snapshot.UpdatedAt == "" {
		return nil
	}

	snapshot := realtimeHealthState.snapshot
	return &snapshot
}
