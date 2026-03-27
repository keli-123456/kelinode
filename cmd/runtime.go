package cmd

import (
	"fmt"
	"math"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

type appliedRuntimeSettings struct {
	GoMemLimit      string `json:"gomemlimit"`
	GoMemLimitBytes int64  `json:"gomemlimit_bytes"`
	GOGC            int    `json:"gogc"`
}

type runtimeTuningState struct {
	memLimitConfigured  bool
	gcPercentConfigured bool
}

func applyRuntimeSettings(cfg conf.RuntimeConfig, state *runtimeTuningState) appliedRuntimeSettings {
	result := appliedRuntimeSettings{
		GoMemLimit: strings.TrimSpace(cfg.GoMemLimit),
		GOGC:       cfg.GOGC,
	}

	if state == nil {
		state = &runtimeTuningState{}
	}

	if result.GoMemLimit != "" && result.GoMemLimit != "0" {
		bytes, err := parseMemoryLimit(result.GoMemLimit)
		if err != nil {
			log.WithFields(log.Fields{
				"value": result.GoMemLimit,
				"err":   err,
			}).Warn("Invalid Go memory limit, skipping GOMEMLIMIT override")
		} else {
			debug.SetMemoryLimit(bytes)
			state.memLimitConfigured = true
			result.GoMemLimitBytes = bytes
			log.WithFields(log.Fields{
				"gomemlimit":       result.GoMemLimit,
				"gomemlimit_bytes": bytes,
			}).Info("Applied Go memory limit")
		}
	} else if state.memLimitConfigured {
		debug.SetMemoryLimit(math.MaxInt64)
		state.memLimitConfigured = false
		log.Info("Cleared configured Go memory limit override")
	}

	if result.GOGC != 0 {
		debug.SetGCPercent(result.GOGC)
		state.gcPercentConfigured = true
		log.WithField("gogc", result.GOGC).Info("Applied Go GC target")
	} else if state.gcPercentConfigured {
		debug.SetGCPercent(100)
		state.gcPercentConfigured = false
		log.Info("Reset Go GC target to default")
	}

	return result
}

func parseMemoryLimit(input string) (int64, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return 0, fmt.Errorf("empty memory limit")
	}

	upper := strings.ToUpper(raw)
	suffixes := []struct {
		suffix string
		factor int64
	}{
		{"KIB", 1024},
		{"MIB", 1024 * 1024},
		{"GIB", 1024 * 1024 * 1024},
		{"TIB", 1024 * 1024 * 1024 * 1024},
		{"KB", 1000},
		{"MB", 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"B", 1},
	}

	factor := int64(1)
	numberPart := raw
	for _, candidate := range suffixes {
		if strings.HasSuffix(upper, candidate.suffix) {
			factor = candidate.factor
			numberPart = strings.TrimSpace(raw[:len(raw)-len(candidate.suffix)])
			break
		}
	}

	if numberPart == "" {
		return 0, fmt.Errorf("missing numeric value")
	}

	value, err := strconv.ParseInt(numberPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse numeric value: %w", err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("memory limit must be positive")
	}
	if value > math.MaxInt64/factor {
		return 0, fmt.Errorf("memory limit overflow")
	}
	return value * factor, nil
}
