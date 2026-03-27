package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/node"
	log "github.com/sirupsen/logrus"
)

type healthState struct {
	mu sync.RWMutex

	startedAt       time.Time
	configPath      string
	ready           bool
	lastReloadAt    time.Time
	healthPort      int
	nodeCount       int
	realtimeEnabled bool
	runtime         appliedRuntimeSettings
}

type healthResponse struct {
	Status          string                 `json:"status"`
	Ready           bool                   `json:"ready"`
	Version         string                 `json:"version"`
	ConfigPath      string                 `json:"config_path,omitempty"`
	StartedAt       string                 `json:"started_at"`
	UptimeSeconds   int64                  `json:"uptime_seconds"`
	LastReloadAt    string                 `json:"last_reload_at,omitempty"`
	NodeCount       int                    `json:"node_count"`
	RealtimeEnabled bool                   `json:"realtime_enabled"`
	HealthPort      int                    `json:"health_port"`
	Goroutines      int                    `json:"goroutines"`
	Runtime         appliedRuntimeSettings `json:"runtime"`
}

func newHealthState(configPath string) *healthState {
	state := &healthState{
		startedAt:  time.Now(),
		configPath: configPath,
	}
	state.publishSnapshot("starting")
	return state
}

func (s *healthState) UpdateConfig(cfg *conf.Conf, runtime appliedRuntimeSettings) {
	if s == nil || cfg == nil {
		return
	}
	s.mu.Lock()
	if s.healthPort == 0 {
		s.healthPort = cfg.HealthPort
	}
	s.nodeCount = len(cfg.NodeConfigs)
	s.realtimeEnabled = cfg.RealtimeConfig.Enabled || cfg.RealtimeConfig.URL != ""
	s.runtime = runtime
	s.mu.Unlock()
	s.publishSnapshot("")
}

func (s *healthState) MarkReady(ready bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ready = ready
	if ready {
		s.lastReloadAt = time.Now()
	}
	s.mu.Unlock()
	s.publishSnapshot("")
}

func startHealthServer(port int, state *healthState) {
	if port == 0 || state == nil {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		writeHealthResponse(w, http.StatusOK, state.snapshot("alive"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		payload := state.snapshot("")
		statusCode := http.StatusOK
		if !payload.Ready {
			payload.Status = "starting"
			statusCode = http.StatusServiceUnavailable
		}
		writeHealthResponse(w, statusCode, payload)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		handleHealthRequest(w, r, state)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		handleHealthRequest(w, r, state)
	})

	go func() {
		log.Infof("Starting health server on 127.0.0.1:%d", port)
		if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux); err != nil {
			log.WithField("err", err).Error("health server failed")
		}
	}()
}

func handleHealthRequest(w http.ResponseWriter, _ *http.Request, state *healthState) {
	payload := state.snapshot("")
	statusCode := http.StatusOK
	if !payload.Ready {
		payload.Status = "starting"
		statusCode = http.StatusServiceUnavailable
	}
	writeHealthResponse(w, statusCode, payload)
}

func (s *healthState) snapshot(status string) healthResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if status == "" {
		status = "ok"
		if !s.ready {
			status = "starting"
		}
	}

	response := healthResponse{
		Status:          status,
		Ready:           s.ready,
		Version:         version,
		ConfigPath:      s.configPath,
		StartedAt:       s.startedAt.Format(time.RFC3339),
		UptimeSeconds:   int64(time.Since(s.startedAt).Seconds()),
		NodeCount:       s.nodeCount,
		RealtimeEnabled: s.realtimeEnabled,
		HealthPort:      s.healthPort,
		Goroutines:      runtime.NumGoroutine(),
		Runtime:         s.runtime,
	}
	if !s.lastReloadAt.IsZero() {
		response.LastReloadAt = s.lastReloadAt.Format(time.RFC3339)
	}
	return response
}

func (s *healthState) publishSnapshot(status string) {
	if s == nil {
		return
	}

	payload := s.snapshot(status)
	node.SetRealtimeHealthSnapshot(node.RealtimeHealthSnapshot{
		Status:          payload.Status,
		Ready:           payload.Ready,
		Version:         payload.Version,
		ConfigPath:      payload.ConfigPath,
		StartedAt:       payload.StartedAt,
		UptimeSeconds:   payload.UptimeSeconds,
		LastReloadAt:    payload.LastReloadAt,
		NodeCount:       payload.NodeCount,
		RealtimeEnabled: payload.RealtimeEnabled,
		HealthPort:      payload.HealthPort,
		Goroutines:      payload.Goroutines,
		Runtime: node.RealtimeRuntimeSnapshot{
			GoMemLimit:      payload.Runtime.GoMemLimit,
			GoMemLimitBytes: payload.Runtime.GoMemLimitBytes,
			GOGC:            payload.Runtime.GOGC,
		},
	})
}

func writeHealthResponse(w http.ResponseWriter, statusCode int, payload healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.WithField("err", err).Warn("write health response failed")
	}
}
