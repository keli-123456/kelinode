package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type machineUpgradeStatus struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	TargetVersion string `json:"target_version"`
	Error         string `json:"error,omitempty"`
	StartedAt     int64  `json:"started_at,omitempty"`
	FinishedAt    int64  `json:"finished_at,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

var machineUpgradeState struct {
	sync.Mutex
	status *machineUpgradeStatus
}

var kelinodeVersionPattern = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z._-]{0,63}$`)
var unitNamePattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func currentMachineUpgradeStatus() *machineUpgradeStatus {
	machineUpgradeState.Lock()
	defer machineUpgradeState.Unlock()
	if machineUpgradeState.status == nil {
		return nil
	}
	copy := *machineUpgradeState.status
	return &copy
}

func requestMachineUpgrade(command machineUpgradeCommand) {
	command.ID = strings.TrimSpace(command.ID)
	command.TargetVersion = strings.TrimSpace(command.TargetVersion)
	if command.ID == "" || !kelinodeVersionPattern.MatchString(command.TargetVersion) {
		return
	}

	now := time.Now().Unix()
	machineUpgradeState.Lock()
	if machineUpgradeState.status != nil && machineUpgradeState.status.ID == command.ID {
		status := machineUpgradeState.status.Status
		if status == "running" || status == "succeeded" {
			machineUpgradeState.Unlock()
			return
		}
	}

	next := &machineUpgradeStatus{
		ID:            command.ID,
		Status:        "running",
		TargetVersion: command.TargetVersion,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if versionsEqual(currentKelinodeVersion(), command.TargetVersion) {
		next.Status = "succeeded"
		next.FinishedAt = now
		machineUpgradeState.status = next
		machineUpgradeState.Unlock()
		return
	}
	machineUpgradeState.status = next
	machineUpgradeState.Unlock()

	go runMachineUpgrade(command)
}

func runMachineUpgrade(command machineUpgradeCommand) {
	if err := launchMachineUpgrade(command.TargetVersion); err != nil {
		markMachineUpgradeFailed(command.ID, err)
	}
}

func markMachineUpgradeFailed(id string, err error) {
	now := time.Now().Unix()
	machineUpgradeState.Lock()
	defer machineUpgradeState.Unlock()
	if machineUpgradeState.status == nil || machineUpgradeState.status.ID != id {
		return
	}
	machineUpgradeState.status.Status = "failed"
	machineUpgradeState.status.Error = truncateUpgradeError(err.Error())
	machineUpgradeState.status.FinishedAt = now
	machineUpgradeState.status.UpdatedAt = now
}

func launchMachineUpgrade(targetVersion string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	if !kelinodeVersionPattern.MatchString(targetVersion) {
		return errors.New("invalid target version")
	}

	scriptURL := "https://raw.githubusercontent.com/keli-123456/kelinode/main/script/install.sh"
	script := fmt.Sprintf(
		"curl -fsSL %s -o /tmp/v2node-install.sh && bash /tmp/v2node-install.sh %s",
		shellQuote(scriptURL),
		shellQuote(targetVersion),
	)
	if _, err := exec.LookPath("systemd-run"); err == nil {
		unit := "v2node-self-update-" + sanitizeSystemdUnitPart(targetVersion)
		output, err := exec.Command(
			"systemd-run",
			"--unit", unit,
			"--description=v2node self update",
			"/bin/sh",
			"-c",
			script,
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("start systemd update failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}

	detached := "sleep 1; " + script
	output, err := exec.Command(
		"/bin/sh",
		"-c",
		fmt.Sprintf("nohup /bin/sh -c %s >/tmp/v2node-self-update.log 2>&1 &", shellQuote(detached)),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start detached update failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func sanitizeSystemdUnitPart(value string) string {
	value = unitNamePattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if value == "" {
		return "latest"
	}
	return value
}

func versionsEqual(current string, target string) bool {
	return strings.TrimLeft(strings.TrimSpace(current), "vV") == strings.TrimLeft(strings.TrimSpace(target), "vV")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func truncateUpgradeError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 1000 {
		return value
	}
	return value[:1000]
}
