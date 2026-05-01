package node

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	log "github.com/sirupsen/logrus"
)

const hysteriaPortForwardChain = "V2NODE-HY2"
const hysteriaPortForwardComment = "V2NODE-HY2"

type portForwardMatcher struct {
	args       []string
	singlePort int
}

type portForwardRule struct {
	matcher    portForwardMatcher
	targetPort int
}

type portForwardRange struct {
	start int
	end   int
}

type allocatedPortForwardRange struct {
	nodeID     int
	targetPort int
	rng        portForwardRange
}

type portForwardCommandRunner func(ctx context.Context, name string, args ...string) error
type portForwardCommandOutputRunner func(ctx context.Context, name string, args ...string) (string, error)

var (
	runPortForwardCommand       portForwardCommandRunner       = defaultRunPortForwardCommand
	runPortForwardCommandOutput portForwardCommandOutputRunner = defaultRunPortForwardCommandOutput
)

func reconcileHysteriaPortForward(infos []*panel.NodeInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := RepairHysteriaPortForward(ctx, infos)
	for _, err := range status.Errors {
		log.WithField("err", err).Warn("HY2 port forwarding reconcile warning")
	}
	for _, toolStatus := range status.Tools {
		if toolStatus.Error != "" {
			entry := log.WithFields(log.Fields{
				"tool":  toolStatus.Tool,
				"chain": hysteriaPortForwardChain,
				"err":   toolStatus.Error,
			})
			if toolStatus.Tool == "ip6tables" {
				entry.Debug("Failed to reconcile HY2 IPv6 port forwarding")
				continue
			}
			entry.Warn("Failed to reconcile HY2 port forwarding")
		}
	}
}

var cleanupHysteriaPortForwardRuntime = cleanupHysteriaPortForward

func cleanupHysteriaPortForward() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := CleanupHysteriaPortForward(ctx)
	for _, err := range status.Errors {
		log.WithField("err", err).Warn("HY2 port forwarding cleanup warning")
	}
	for _, toolStatus := range status.Tools {
		if toolStatus.Error == "" {
			continue
		}
		entry := log.WithFields(log.Fields{
			"tool":  toolStatus.Tool,
			"chain": hysteriaPortForwardChain,
			"err":   toolStatus.Error,
		})
		if toolStatus.Tool == "ip6tables" {
			entry.Debug("Failed to clean up HY2 IPv6 port forwarding")
			continue
		}
		entry.Warn("Failed to clean up HY2 port forwarding")
	}
}

func buildHysteriaPortForwardRules(infos []*panel.NodeInfo) ([]portForwardRule, []error) {
	rules := make([]portForwardRule, 0)
	errs := make([]error, 0)
	seen := make(map[string]struct{})
	allocated := make([]allocatedPortForwardRange, 0)
	targetPorts := collectHysteriaTargetPorts(infos)

	for _, info := range infos {
		if info == nil || info.Common == nil || !isHysteriaNode(info.Type) {
			continue
		}
		targetPort := info.Common.ServerPort
		if targetPort <= 0 || targetPort > 65535 {
			errs = append(errs, fmt.Errorf("node %d has invalid server_port %d", info.Id, targetPort))
			continue
		}

		externalPort := strings.TrimSpace(info.Common.Port.String())
		if externalPort == "" {
			externalPort = strings.TrimSpace(info.Common.Ports.String())
		}
		if externalPort == "" {
			continue
		}

		matchers, ranges, err := parsePortForwardMatchersAndRangesExcept(externalPort, targetPort)
		if err != nil {
			errs = append(errs, fmt.Errorf("node %d has invalid port %q: %w", info.Id, externalPort, err))
			continue
		}
		if conflictPort, ok := findPortForwardTargetPortConflict(ranges, targetPort, targetPorts); ok {
			errs = append(errs, fmt.Errorf(
				"node %d port %q overlaps server_port %d from another HY2 node",
				info.Id,
				externalPort,
				conflictPort,
			))
			continue
		}
		if conflict := findPortForwardRangeConflict(ranges, targetPort, allocated); conflict != nil {
			errs = append(errs, fmt.Errorf(
				"node %d port %q overlaps node %d port %d-%d with different target server_port",
				info.Id,
				externalPort,
				conflict.nodeID,
				conflict.rng.start,
				conflict.rng.end,
			))
			continue
		}
		for _, rng := range ranges {
			allocated = append(allocated, allocatedPortForwardRange{
				nodeID:     info.Id,
				targetPort: targetPort,
				rng:        rng,
			})
		}
		for _, matcher := range matchers {
			key := strconv.Itoa(targetPort) + "|" + strings.Join(matcher.args, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rules = append(rules, portForwardRule{
				matcher:    matcher,
				targetPort: targetPort,
			})
		}
	}

	return rules, errs
}

func collectHysteriaTargetPorts(infos []*panel.NodeInfo) map[int]struct{} {
	targets := make(map[int]struct{})
	for _, info := range infos {
		if info == nil || info.Common == nil || !isHysteriaNode(info.Type) {
			continue
		}
		targetPort := info.Common.ServerPort
		if targetPort <= 0 || targetPort > 65535 {
			continue
		}
		targets[targetPort] = struct{}{}
	}
	return targets
}

func findPortForwardTargetPortConflict(ranges []portForwardRange, targetPort int, targetPorts map[int]struct{}) (int, bool) {
	for port := range targetPorts {
		if port == targetPort {
			continue
		}
		for _, candidate := range ranges {
			if candidate.contains(port) {
				return port, true
			}
		}
	}
	return 0, false
}

func findPortForwardRangeConflict(ranges []portForwardRange, targetPort int, allocated []allocatedPortForwardRange) *allocatedPortForwardRange {
	for _, candidate := range ranges {
		for i := range allocated {
			if allocated[i].targetPort == targetPort {
				continue
			}
			if candidate.overlaps(allocated[i].rng) {
				return &allocated[i]
			}
		}
	}
	return nil
}

func (r portForwardRange) overlaps(other portForwardRange) bool {
	return r.start <= other.end && other.start <= r.end
}

func (r portForwardRange) contains(port int) bool {
	return r.start <= port && port <= r.end
}

func isHysteriaNode(nodeType string) bool {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "hysteria", "hysteria2":
		return true
	default:
		return false
	}
}

func parsePortForwardMatchers(raw string) ([]portForwardMatcher, error) {
	return parsePortForwardMatchersExcept(raw, 0)
}

func parsePortForwardMatchersExcept(raw string, excludedPort int) ([]portForwardMatcher, error) {
	matchers, _, err := parsePortForwardMatchersAndRangesExcept(raw, excludedPort)
	return matchers, err
}

func parsePortForwardMatchersAndRangesExcept(raw string, excludedPort int) ([]portForwardMatcher, []portForwardRange, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if cleaned == "" {
		return nil, nil, fmt.Errorf("empty port")
	}

	tokens := strings.Split(cleaned, ",")
	matchers := make([]portForwardMatcher, 0, len(tokens))
	ranges := make([]portForwardRange, 0, len(tokens))
	singles := make([]string, 0, 15)

	addSingle := func(port int) {
		if port == excludedPort {
			return
		}
		singles = append(singles, strconv.Itoa(port))
		ranges = append(ranges, portForwardRange{start: port, end: port})
	}

	flushSingles := func() {
		for len(singles) > 0 {
			chunkSize := len(singles)
			if chunkSize > 15 {
				chunkSize = 15
			}
			chunk := append([]string(nil), singles[:chunkSize]...)
			if len(chunk) == 1 {
				port, _ := strconv.Atoi(chunk[0])
				matchers = append(matchers, portForwardMatcher{
					args:       []string{"--dport", chunk[0]},
					singlePort: port,
				})
			} else {
				matchers = append(matchers, portForwardMatcher{
					args: []string{"-m", "multiport", "--dports", strings.Join(chunk, ",")},
				})
			}
			singles = singles[chunkSize:]
		}
	}

	addRange := func(start, end int) {
		appendRange := func(rangeStart, rangeEnd int) {
			ranges = append(ranges, portForwardRange{start: rangeStart, end: rangeEnd})
			matchers = append(matchers, portForwardMatcher{
				args: []string{"--dport", fmt.Sprintf("%d:%d", rangeStart, rangeEnd)},
			})
		}
		if excludedPort > 0 && start <= excludedPort && excludedPort <= end {
			if start <= excludedPort-1 {
				appendRange(start, excludedPort-1)
			}
			if excludedPort+1 <= end {
				appendRange(excludedPort+1, end)
			}
			return
		}
		appendRange(start, end)
	}

	for _, token := range tokens {
		if token == "" {
			return nil, nil, fmt.Errorf("empty token")
		}
		if strings.ContainsAny(token, "-:") {
			start, end, err := parsePortRange(token)
			if err != nil {
				return nil, nil, err
			}
			if start == end {
				addSingle(start)
				continue
			}
			flushSingles()
			addRange(start, end)
			continue
		}
		port, err := parsePortNumber(token)
		if err != nil {
			return nil, nil, err
		}
		addSingle(port)
	}
	flushSingles()

	return matchers, ranges, nil
}

func parsePortRange(token string) (int, int, error) {
	parts := strings.FieldsFunc(token, func(r rune) bool {
		return r == '-' || r == ':'
	})
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range %q", token)
	}
	start, err := parsePortNumber(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parsePortNumber(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid reversed range %q", token)
	}
	return start, end, nil
}

func parsePortNumber(token string) (int, error) {
	if token == "" {
		return 0, fmt.Errorf("empty port")
	}
	port, err := strconv.Atoi(token)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", token)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port out of range %q", token)
	}
	return port, nil
}

func reconcilePortForwardTool(ctx context.Context, tool string, rules []portForwardRule) error {
	if _, err := exec.LookPath(tool); err != nil {
		return nil
	}

	deletePortForwardRules(ctx, tool)
	_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-F", hysteriaPortForwardChain)
	_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-X", hysteriaPortForwardChain)

	for _, rule := range rules {
		args := []string{"-t", "nat", "-A", "PREROUTING", "-p", "udp"}
		args = append(args, rule.matcher.args...)
		args = append(args, "-m", "comment", "--comment", hysteriaPortForwardComment)
		args = append(args, "-j", "REDIRECT", "--to-ports", strconv.Itoa(rule.targetPort))
		if err := runPortForwardCommand(ctx, tool, args...); err != nil {
			return err
		}
	}
	return nil
}

func deletePortForwardRules(ctx context.Context, tool string) {
	output, err := runPortForwardCommandOutput(ctx, tool, "-t", "nat", "-S", "PREROUTING")
	if err == nil {
		for _, line := range strings.Split(output, "\n") {
			fields := parseIptablesSpec(strings.TrimSpace(line))
			if !isPortForwardRuleSpec(fields) {
				continue
			}
			fields[0] = "-D"
			args := append([]string{"-t", "nat"}, fields...)
			_ = runPortForwardCommand(ctx, tool, args...)
		}
		return
	}

	for {
		if err := runPortForwardCommand(ctx, tool, "-t", "nat", "-D", "PREROUTING", "-p", "udp", "-j", hysteriaPortForwardChain); err != nil {
			return
		}
	}
}

func isPortForwardRuleSpec(fields []string) bool {
	if len(fields) < 4 || fields[0] != "-A" || fields[1] != "PREROUTING" {
		return false
	}
	for i := 2; i < len(fields)-1; i++ {
		if fields[i] == "-j" && fields[i+1] == hysteriaPortForwardChain {
			return true
		}
		if fields[i] == "--comment" && strings.Trim(fields[i+1], `"'`) == hysteriaPortForwardComment {
			return true
		}
	}
	return false
}

func parseIptablesSpec(line string) []string {
	if line == "" {
		return nil
	}
	fields := make([]string, 0)
	var builder strings.Builder
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			builder.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t':
			if builder.Len() > 0 {
				fields = append(fields, builder.String())
				builder.Reset()
			}
		default:
			builder.WriteRune(r)
		}
	}
	if builder.Len() > 0 {
		fields = append(fields, builder.String())
	}
	return fields
}

func defaultRunPortForwardCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func defaultRunPortForwardCommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return "", err
	}
	return "", fmt.Errorf("%w: %s", err, message)
}
