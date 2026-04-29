package node

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	log "github.com/sirupsen/logrus"
)

const hysteriaPortForwardChain = "V2NODE-HY2"

type portForwardMatcher struct {
	args       []string
	singlePort int
}

type portForwardRule struct {
	matcher    portForwardMatcher
	targetPort int
}

type portForwardCommandRunner func(ctx context.Context, name string, args ...string) error

var runPortForwardCommand portForwardCommandRunner = defaultRunPortForwardCommand

func reconcileHysteriaPortForward(infos []*panel.NodeInfo) {
	rules, errs := buildHysteriaPortForwardRules(infos)
	for _, err := range errs {
		log.WithField("err", err).Warn("Skipped HY2 port forwarding rule")
	}

	if os.Geteuid() != 0 {
		if len(rules) > 0 {
			log.Warn("HY2 port forwarding is enabled but v2node is not running as root")
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tool := range []string{"iptables", "ip6tables"} {
		if err := reconcilePortForwardTool(ctx, tool, rules); err != nil {
			entry := log.WithFields(log.Fields{
				"tool":  tool,
				"chain": hysteriaPortForwardChain,
				"err":   err,
			})
			if tool == "ip6tables" {
				entry.Debug("Failed to reconcile HY2 IPv6 port forwarding")
				continue
			}
			entry.Warn("Failed to reconcile HY2 port forwarding")
		}
	}
}

func buildHysteriaPortForwardRules(infos []*panel.NodeInfo) ([]portForwardRule, []error) {
	rules := make([]portForwardRule, 0)
	errs := make([]error, 0)
	seen := make(map[string]struct{})

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

		matchers, err := parsePortForwardMatchersExcept(externalPort, targetPort)
		if err != nil {
			errs = append(errs, fmt.Errorf("node %d has invalid port %q: %w", info.Id, externalPort, err))
			continue
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
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if cleaned == "" {
		return nil, fmt.Errorf("empty port")
	}

	tokens := strings.Split(cleaned, ",")
	matchers := make([]portForwardMatcher, 0, len(tokens))
	singles := make([]string, 0, 15)

	addSingle := func(port int) {
		if port == excludedPort {
			return
		}
		singles = append(singles, strconv.Itoa(port))
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
		if excludedPort > 0 && start <= excludedPort && excludedPort <= end {
			if start <= excludedPort-1 {
				matchers = append(matchers, portForwardMatcher{
					args: []string{"--dport", fmt.Sprintf("%d:%d", start, excludedPort-1)},
				})
			}
			if excludedPort+1 <= end {
				matchers = append(matchers, portForwardMatcher{
					args: []string{"--dport", fmt.Sprintf("%d:%d", excludedPort+1, end)},
				})
			}
			return
		}
		matchers = append(matchers, portForwardMatcher{
			args: []string{"--dport", fmt.Sprintf("%d:%d", start, end)},
		})
	}

	for _, token := range tokens {
		if token == "" {
			return nil, fmt.Errorf("empty token")
		}
		if strings.ContainsAny(token, "-:") {
			start, end, err := parsePortRange(token)
			if err != nil {
				return nil, err
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
			return nil, err
		}
		addSingle(port)
	}
	flushSingles()

	return matchers, nil
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

	deletePortForwardJump(ctx, tool)
	if len(rules) == 0 {
		_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-F", hysteriaPortForwardChain)
		_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-X", hysteriaPortForwardChain)
		return nil
	}

	_ = runPortForwardCommand(ctx, tool, "-t", "nat", "-N", hysteriaPortForwardChain)
	if err := runPortForwardCommand(ctx, tool, "-t", "nat", "-F", hysteriaPortForwardChain); err != nil {
		return err
	}
	if err := runPortForwardCommand(ctx, tool, "-t", "nat", "-A", "PREROUTING", "-p", "udp", "-j", hysteriaPortForwardChain); err != nil {
		return err
	}

	for _, rule := range rules {
		args := []string{"-t", "nat", "-A", hysteriaPortForwardChain, "-p", "udp"}
		args = append(args, rule.matcher.args...)
		args = append(args, "-j", "REDIRECT", "--to-ports", strconv.Itoa(rule.targetPort))
		if err := runPortForwardCommand(ctx, tool, args...); err != nil {
			return err
		}
	}
	return nil
}

func deletePortForwardJump(ctx context.Context, tool string) {
	for {
		if err := runPortForwardCommand(ctx, tool, "-t", "nat", "-D", "PREROUTING", "-p", "udp", "-j", hysteriaPortForwardChain); err != nil {
			return
		}
	}
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
