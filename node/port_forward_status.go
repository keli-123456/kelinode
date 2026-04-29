package node

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
)

type HysteriaPortForwardRuleSpec struct {
	Protocol   string `json:"protocol"`
	Match      string `json:"match"`
	TargetPort int    `json:"target_port"`
	Spec       string `json:"spec"`
}

type HysteriaPortForwardToolStatus struct {
	Tool       string   `json:"tool"`
	Available  bool     `json:"available"`
	Current    []string `json:"current"`
	Expected   []string `json:"expected"`
	Missing    []string `json:"missing"`
	Extra      []string `json:"extra"`
	StaleChain bool     `json:"stale_chain"`
	Error      string   `json:"error,omitempty"`
}

type HysteriaPortForwardStatus struct {
	Enabled       bool                            `json:"enabled"`
	RunningAsRoot bool                            `json:"running_as_root"`
	UpdatedAt     string                          `json:"updated_at,omitempty"`
	ExpectedRules []HysteriaPortForwardRuleSpec   `json:"expected_rules"`
	Tools         []HysteriaPortForwardToolStatus `json:"tools"`
	Errors        []string                        `json:"errors,omitempty"`
}

var hysteriaPortForwardStatusState struct {
	mu       sync.RWMutex
	snapshot HysteriaPortForwardStatus
}

func InspectHysteriaPortForward(ctx context.Context, infos []*panel.NodeInfo) HysteriaPortForwardStatus {
	if ctx == nil {
		ctx = context.Background()
	}

	rules, errs := buildHysteriaPortForwardRules(infos)
	status := newHysteriaPortForwardStatus(rules, errs)
	for _, tool := range []string{"iptables", "ip6tables"} {
		status.Tools = append(status.Tools, inspectPortForwardTool(ctx, tool, rules))
	}
	setHysteriaPortForwardStatus(status)
	return status
}

func RepairHysteriaPortForward(ctx context.Context, infos []*panel.NodeInfo) HysteriaPortForwardStatus {
	if ctx == nil {
		ctx = context.Background()
	}

	rules, errs := buildHysteriaPortForwardRules(infos)
	status := newHysteriaPortForwardStatus(rules, errs)
	if !status.RunningAsRoot {
		for _, tool := range []string{"iptables", "ip6tables"} {
			status.Tools = append(status.Tools, inspectPortForwardTool(ctx, tool, rules))
		}
		if len(rules) > 0 || HysteriaPortForwardNeedsRepair(status) {
			status.Errors = append(status.Errors, "HY2 port forwarding repair requires root")
		}
		setHysteriaPortForwardStatus(status)
		return status
	}

	for _, tool := range []string{"iptables", "ip6tables"} {
		toolStatus := inspectPortForwardTool(ctx, tool, rules)
		if toolStatus.Available {
			if err := reconcilePortForwardTool(ctx, tool, rules); err != nil {
				toolStatus.Error = err.Error()
				status.Errors = append(status.Errors, tool+": "+err.Error())
			} else {
				toolStatus = inspectPortForwardTool(ctx, tool, rules)
			}
		}
		status.Tools = append(status.Tools, toolStatus)
	}

	setHysteriaPortForwardStatus(status)
	return status
}

func CleanupHysteriaPortForward(ctx context.Context) HysteriaPortForwardStatus {
	if ctx == nil {
		ctx = context.Background()
	}

	status := newHysteriaPortForwardStatus(nil, nil)
	status.Enabled = false
	if !status.RunningAsRoot {
		status.Errors = append(status.Errors, "HY2 port forwarding cleanup requires root")
		for _, tool := range []string{"iptables", "ip6tables"} {
			status.Tools = append(status.Tools, inspectPortForwardTool(ctx, tool, nil))
		}
		setHysteriaPortForwardStatus(status)
		return status
	}

	for _, tool := range []string{"iptables", "ip6tables"} {
		toolStatus := inspectPortForwardTool(ctx, tool, nil)
		if toolStatus.Available {
			deletePortForwardRules(ctx, tool)
			_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-F", hysteriaPortForwardChain)
			_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-X", hysteriaPortForwardChain)
			toolStatus = inspectPortForwardTool(ctx, tool, nil)
		}
		status.Tools = append(status.Tools, toolStatus)
	}

	setHysteriaPortForwardStatus(status)
	return status
}

func GetHysteriaPortForwardStatusSnapshot() HysteriaPortForwardStatus {
	hysteriaPortForwardStatusState.mu.RLock()
	defer hysteriaPortForwardStatusState.mu.RUnlock()

	snapshot := hysteriaPortForwardStatusState.snapshot
	snapshot.ExpectedRules = append([]HysteriaPortForwardRuleSpec(nil), snapshot.ExpectedRules...)
	snapshot.Errors = append([]string(nil), snapshot.Errors...)
	snapshot.Tools = append([]HysteriaPortForwardToolStatus(nil), snapshot.Tools...)
	for i := range snapshot.Tools {
		snapshot.Tools[i].Current = append([]string(nil), snapshot.Tools[i].Current...)
		snapshot.Tools[i].Expected = append([]string(nil), snapshot.Tools[i].Expected...)
		snapshot.Tools[i].Missing = append([]string(nil), snapshot.Tools[i].Missing...)
		snapshot.Tools[i].Extra = append([]string(nil), snapshot.Tools[i].Extra...)
	}
	return snapshot
}

func SetHysteriaPortForwardDisabled() {
	setHysteriaPortForwardStatus(HysteriaPortForwardStatus{
		Enabled:       false,
		RunningAsRoot: os.Geteuid() == 0,
		ExpectedRules: []HysteriaPortForwardRuleSpec{},
		Tools:         []HysteriaPortForwardToolStatus{},
	})
}

func setHysteriaPortForwardStatus(status HysteriaPortForwardStatus) {
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	hysteriaPortForwardStatusState.mu.Lock()
	hysteriaPortForwardStatusState.snapshot = status
	hysteriaPortForwardStatusState.mu.Unlock()
}

func newHysteriaPortForwardStatus(rules []portForwardRule, errs []error) HysteriaPortForwardStatus {
	status := HysteriaPortForwardStatus{
		Enabled:       true,
		RunningAsRoot: os.Geteuid() == 0,
		ExpectedRules: describePortForwardRules(rules),
		Tools:         []HysteriaPortForwardToolStatus{},
	}
	for _, err := range errs {
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		}
	}
	return status
}

func describePortForwardRules(rules []portForwardRule) []HysteriaPortForwardRuleSpec {
	specs := make([]HysteriaPortForwardRuleSpec, 0, len(rules))
	for _, rule := range rules {
		specs = append(specs, HysteriaPortForwardRuleSpec{
			Protocol:   "udp",
			Match:      strings.Join(rule.matcher.args, " "),
			TargetPort: rule.targetPort,
			Spec:       strings.Join(expectedPortForwardSpecFields(rule), " "),
		})
	}
	return specs
}

func inspectPortForwardTool(ctx context.Context, tool string, rules []portForwardRule) HysteriaPortForwardToolStatus {
	status := HysteriaPortForwardToolStatus{
		Tool:     tool,
		Expected: expectedPortForwardSpecs(rules),
		Current:  []string{},
		Missing:  []string{},
		Extra:    []string{},
	}
	if _, err := exec.LookPath(tool); err != nil {
		status.Available = false
		return status
	}
	status.Available = true

	current, err := listPortForwardSpecs(ctx, tool)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Current = current

	expectedKeys := make(map[string]string, len(rules))
	for _, rule := range rules {
		fields := expectedPortForwardSpecFields(rule)
		expectedKeys[portForwardFieldsKey(fields)] = strings.Join(fields, " ")
	}

	currentKeys := make(map[string]string, len(current))
	for _, spec := range current {
		fields := parseIptablesSpec(spec)
		key := portForwardFieldsKey(fields)
		currentKeys[key] = spec
		if _, ok := expectedKeys[key]; !ok {
			status.Extra = append(status.Extra, spec)
		}
	}
	for key, spec := range expectedKeys {
		if _, ok := currentKeys[key]; !ok {
			status.Missing = append(status.Missing, spec)
		}
	}

	status.StaleChain = portForwardChainExists(ctx, tool)
	return status
}

func listPortForwardSpecs(ctx context.Context, tool string) ([]string, error) {
	output, err := runPortForwardCommandOutput(ctx, tool, "-t", "nat", "-S", "PREROUTING")
	if err != nil {
		return nil, err
	}

	specs := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		fields := parseIptablesSpec(strings.TrimSpace(line))
		if !isPortForwardRuleSpec(fields) {
			continue
		}
		specs = append(specs, strings.Join(normalizePortForwardSpecFields(fields), " "))
	}
	return specs, nil
}

func portForwardChainExists(ctx context.Context, tool string) bool {
	_, err := runPortForwardCommandOutput(ctx, tool, "-t", "nat", "-S", hysteriaPortForwardChain)
	return err == nil
}

func expectedPortForwardSpecs(rules []portForwardRule) []string {
	specs := make([]string, 0, len(rules))
	for _, rule := range rules {
		specs = append(specs, strings.Join(expectedPortForwardSpecFields(rule), " "))
	}
	return specs
}

func expectedPortForwardSpecFields(rule portForwardRule) []string {
	fields := []string{"-A", "PREROUTING", "-p", "udp"}
	fields = append(fields, rule.matcher.args...)
	fields = append(fields, "-m", "comment", "--comment", hysteriaPortForwardComment)
	fields = append(fields, "-j", "REDIRECT", "--to-ports", strconv.Itoa(rule.targetPort))
	return normalizePortForwardSpecFields(fields)
}

func normalizePortForwardSpecFields(fields []string) []string {
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "-m" && i+1 < len(fields) && fields[i+1] == "udp" {
			i++
			continue
		}
		value := fields[i]
		if i > 0 && fields[i-1] == "--comment" {
			value = strings.Trim(value, `"'`)
		}
		out = append(out, value)
	}
	return out
}

func portForwardFieldsKey(fields []string) string {
	return strings.Join(normalizePortForwardSpecFields(fields), "\x00")
}

func HysteriaPortForwardNeedsRepair(status HysteriaPortForwardStatus) bool {
	for _, tool := range status.Tools {
		if !tool.Available || tool.Error != "" {
			continue
		}
		if len(tool.Missing) > 0 || len(tool.Extra) > 0 || tool.StaleChain {
			return true
		}
	}
	return false
}
